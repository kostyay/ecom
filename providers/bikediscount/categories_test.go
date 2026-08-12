package bikediscount

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/kostyay/ecom/provider"
)

const bikeRootID = "018c7eacc99370f89d7947c78ee08b5b"

func TestCategoriesListsStableLLMSRoots(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{Body: readCategoryFixture(t, "llms.txt")}}}
	page, err := (implementation{}).Categories(t.Context(), provider.CategoryListRequest{Request: bikeDiscountRequest(service)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 7 || page.Page.Number != 1 || page.Page.Size != 48 || len(page.Warnings) != 0 {
		t.Fatalf("category page = %#v", page)
	}
	bike := page.Items[1]
	if bike.ID != bikeRootID || bike.Name != "Bike" || bike.Path != "Bike" || bike.ParentID != "" || !bike.HasChildren {
		t.Errorf("bike root = %#v", bike)
	}
	if bike.URL != bikeDiscountBaseURL+"/navigation/"+bikeRootID {
		t.Errorf("bike URL = %q", bike.URL)
	}
	if len(service.requests) != 1 || service.requests[0].URL != bikeDiscountBaseURL+"/en/llms.txt" {
		t.Errorf("resource requests = %#v", service.requests)
	}
}

func TestCategoriesListsDirectAndRecursiveChildrenWithPaths(t *testing.T) {
	for _, test := range []struct {
		name      string
		parentID  string
		recursive bool
		wantIDs   []string
	}{
		{name: "root direct", parentID: bikeRootID, wantIDs: []string{"/en/bike/sale", "/en/mountain-bike-parts"}},
		{name: "root recursive", parentID: bikeRootID, recursive: true, wantIDs: []string{"/en/bike/sale", "/en/mountain-bike-parts", "/en/bike/bike-parts/mountain-bike-parts/brakes/disc-brake-sets"}},
		{name: "canonical direct", parentID: "/en/mountain-bike-parts", wantIDs: []string{"/en/bike/bike-parts/mountain-bike-parts/brakes/disc-brake-sets"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeResourceService{responses: []provider.ResourceResponse{
				{Body: readCategoryFixture(t, "llms.txt")},
				{Body: readCategoryFixture(t, "category_tree.html")},
			}}
			page, err := (implementation{}).Categories(t.Context(), provider.CategoryListRequest{
				Request: bikeDiscountRequest(service), ParentID: test.parentID, Recursive: test.recursive,
			})
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, len(page.Items))
			for index, category := range page.Items {
				ids[index] = category.ID
			}
			if !reflect.DeepEqual(ids, test.wantIDs) {
				t.Fatalf("category IDs = %v, want %v", ids, test.wantIDs)
			}
			last := page.Items[len(page.Items)-1]
			if last.ID == "/en/mountain-bike-parts" {
				if last.ParentID != bikeRootID || last.Path != "Bike / Mountain Bike Parts" || !last.HasChildren {
					t.Errorf("mountain category = %#v", last)
				}
			} else if last.Name == "Disc Brake Sets" {
				if last.ParentID != "/en/mountain-bike-parts" || last.Path != "Bike / Mountain Bike Parts / Disc Brake Sets" {
					t.Errorf("disc category = %#v", last)
				}
			}
		})
	}
}

func TestCategoriesRecursiveTreeContainsRootsAndUniqueDescendants(t *testing.T) {
	llms := readCategoryFixture(t, "llms.txt")
	responses := []provider.ResourceResponse{{Body: llms}}
	rootPages := []struct{ name, slug string }{
		{"Running", "running"}, {"Bike", "bike"}, {"Streetwear", "streetwear"}, {"Ski", "ski"},
		{"Triathlon", "triathlon"}, {"Outdoor", "outdoor"}, {"Brands", "brands"},
	}
	for _, root := range rootPages {
		document := `<nav aria-label="Catalog"><ul><li><a href="/en/` + root.slug + `">` + root.name + `</a>`
		if root.name == "Bike" {
			document += `<ul><li><a href="/en/mountain-bike-parts">Mountain Bike Parts</a></li></ul>`
		}
		document += `</li></ul></nav>`
		responses = append(responses, provider.ResourceResponse{Body: []byte(document)})
	}
	service := &fakeResourceService{responses: responses}
	page, err := (implementation{}).Categories(t.Context(), provider.CategoryListRequest{
		Request: bikeDiscountRequest(service), Recursive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 8 || len(page.Warnings) != 0 {
		t.Fatalf("recursive page has %d items and %d warnings: %#v", len(page.Items), len(page.Warnings), page)
	}
	seen := make(map[string]bool)
	for _, category := range page.Items {
		if seen[category.ID] {
			t.Fatalf("duplicate category ID %q", category.ID)
		}
		seen[category.ID] = true
	}
	if !seen["/en/mountain-bike-parts"] {
		t.Error("recursive tree omitted the nested category")
	}
}

func TestCategoryTreeSkipsMalformedDuplicateAndCyclicNodes(t *testing.T) {
	document := []byte(`<nav aria-label="Catalog"><ul><li><a href="/en/bike">Bike</a><ul>
		<li><a href="/en/bike/sale">Sale</a><ul><li><a href="/en/bike/sale">Sale cycle</a></li></ul></li>
		<li>Missing link</li><li><a href="https://example.com/category">Foreign</a></li>
	</ul></li></ul></nav>`)
	result, err := extractCategoryTree(document, bikeDiscountBaseURL+"/en/bike", provider.Category{ID: bikeRootID, Name: "Bike", Path: "Bike"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.items) != 1 || result.items[0].ID != "/en/bike/sale" {
		t.Fatalf("parsed categories = %#v", result.items)
	}
	if len(result.warnings) != 1 || result.warnings[0].Code != provider.WarningCodePartialParsing ||
		result.warnings[0].FoundCount == nil || *result.warnings[0].FoundCount != 5 ||
		result.warnings[0].ParsedCount == nil || *result.warnings[0].ParsedCount != 2 {
		t.Errorf("warnings = %#v", result.warnings)
	}
}

func TestCategoryItemsUsesVerifiedQueryAndProductParser(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{
		Body: readCategoryFixture(t, "listing_page_1.html"),
	}}}
	page, err := (implementation{}).CategoryItems(t.Context(), provider.CategoryItemsRequest{
		Request: bikeDiscountRequest(service), CategoryID: "/en/bike/sale",
		Page: provider.PageRequest{Number: 2, Size: 48}, Sort: &provider.Sort{Value: "standard"},
		Filters: []provider.Filter{
			{Key: "manufacturer", Value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			{Key: "properties", Value: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			{Key: "properties", Value: "cccccccccccccccccccccccccccccccc"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Name != "Yamaha 500 Wh 36V/13.6Ah Frame Battery" || page.Items[0].DetailLevel != provider.DetailLevelSummary {
		t.Fatalf("products = %#v", page.Items)
	}
	if page.Page.Number != 2 || page.Page.Size != 48 || page.Page.HasNext == nil || !*page.Page.HasNext {
		t.Errorf("page info = %#v", page.Page)
	}
	wantQuery := []provider.RequestValue{
		{Name: "p", Values: []string{"2"}}, {Name: "n", Values: []string{"48"}},
		{Name: "order", Values: []string{"standard"}},
		{Name: "manufacturer", Values: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		{Name: "properties", Values: []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb|cccccccccccccccccccccccccccccccc"}},
	}
	if len(service.requests) != 1 || service.requests[0].URL != bikeDiscountBaseURL+"/en/bike/sale" || !reflect.DeepEqual(service.requests[0].Query, wantQuery) {
		t.Errorf("resource request = %#v", service.requests)
	}
}

func TestCategoryItemsReturnsPartialProductWarning(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{Body: readCategoryFixture(t, "listing_partial.html")}}}
	page, err := (implementation{}).CategoryItems(t.Context(), provider.CategoryItemsRequest{
		Request: bikeDiscountRequest(service), CategoryID: "/en/bike/sale",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(page.Warnings) != 1 || page.Warnings[0].Code != provider.WarningCodePartialParsing {
		t.Fatalf("partial page = %#v", page)
	}
}

func TestCategoryOperationsRejectUnverifiedValues(t *testing.T) {
	implementation := implementation{}
	requests := []provider.CategoryItemsRequest{
		{CategoryID: "bike/sale"},
		{CategoryID: "/en/bike/sale", Page: provider.PageRequest{Number: 0, Size: 24}},
		{CategoryID: "/en/bike/sale", Sort: &provider.Sort{Value: "price-low"}},
		{CategoryID: "/en/bike/sale", Filters: []provider.Filter{{Key: "available", Value: "1"}}},
		{CategoryID: "/en/bike/sale", Filters: []provider.Filter{{Key: "manufacturer", Value: "not-an-id"}}},
	}
	for index, request := range requests {
		request.Request = bikeDiscountRequest(&fakeResourceService{})
		_, err := implementation.CategoryItems(context.Background(), request)
		if !errors.Is(err, provider.ErrorCodeInvalidFilter) {
			t.Errorf("request %d error = %v", index, err)
		}
	}
}

func TestExtractRootCategoriesReturnsPartialWarning(t *testing.T) {
	result := extractRootCategories([]byte("[Bike](https://www.bike-discount.de/navigation/018c7eacc99370f89d7947c78ee08b5b)\n[Broken](https://example.com)"))
	if len(result.items) != 1 || len(result.warnings) != 1 || result.warnings[0].Cause() == nil {
		t.Fatalf("root extraction = %#v", result)
	}
}

func readCategoryFixture(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile("testdata/catalog/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestCategoryPathValidation(t *testing.T) {
	for _, value := range []string{"/en/bike", "/en/yamaha-500-wh-36v/13.6ah-frame-battery"} {
		if !validCategoryPath(value) {
			t.Errorf("valid path %q was rejected", value)
		}
	}
	for _, value := range []string{"", "/de/bike", "/en/", "/en/bike?x=1", "https://www.bike-discount.de/en/bike", "/en//bike"} {
		if validCategoryPath(value) {
			t.Errorf("invalid path %q was accepted", value)
		}
	}
}

func TestCategoryURLUsesReturnedOpaquePath(t *testing.T) {
	target, err := categoryResourceTarget("/en/mountain-bike-parts")
	if err != nil || target.URL != bikeDiscountBaseURL+"/en/mountain-bike-parts" || target.Path != "" {
		t.Fatalf("category target = %#v, %v", target, err)
	}
	if strings.Contains(target.URL, "Bike / Mountain") {
		t.Error("request URL was constructed from the display path")
	}
}
