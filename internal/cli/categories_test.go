package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	coreapp "github.com/kostyay/ecom/internal/app"
	"github.com/kostyay/ecom/provider"
)

type categoryFixtureProvider struct {
	help                   provider.Help
	categoriesResult       provider.CategoryPage
	categorySearchResult   provider.CategoryPage
	categoryItemsResult    provider.ProductPage
	categoriesRequests     []provider.CategoryListRequest
	categorySearchRequests []provider.CategorySearchRequest
	categoryItemsRequests  []provider.CategoryItemsRequest
}

func (fixture *categoryFixtureProvider) Help(context.Context, provider.HelpRequest) (provider.HelpResult, error) {
	return provider.HelpResult{Help: fixture.help}, nil
}

func (fixture *categoryFixtureProvider) Categories(_ context.Context, request provider.CategoryListRequest) (provider.CategoryPage, error) {
	fixture.categoriesRequests = append(fixture.categoriesRequests, request)
	return fixture.categoriesResult, nil
}

func (fixture *categoryFixtureProvider) SearchCategories(_ context.Context, request provider.CategorySearchRequest) (provider.CategoryPage, error) {
	fixture.categorySearchRequests = append(fixture.categorySearchRequests, request)
	return fixture.categorySearchResult, nil
}

func (fixture *categoryFixtureProvider) CategoryItems(_ context.Context, request provider.CategoryItemsRequest) (provider.ProductPage, error) {
	fixture.categoryItemsRequests = append(fixture.categoryItemsRequests, request)
	return fixture.categoryItemsResult, nil
}

func TestCategoriesListsTopLevelParentAndRecursiveTrees(t *testing.T) {
	fixture, factory := categoryFixture(t, true)

	for _, test := range []struct {
		args      []string
		parent    string
		recursive bool
	}{
		{args: []string{"categories"}},
		{args: []string{"categories", "--parent", "bikes"}, parent: "bikes"},
		{args: []string{"categories", "--recursive"}, recursive: true},
	} {
		result := runCategories(t, factory, test.args...)
		if result.status != 0 || result.stderr != "" || !strings.Contains(result.stdout, `"id":"bikes"`) {
			t.Fatalf("categories result = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
		}
		request := fixture.categoriesRequests[len(fixture.categoriesRequests)-1]
		if request.ParentID != test.parent || request.Recursive != test.recursive {
			t.Errorf("category request = %#v", request)
		}
	}
}

func TestCategoriesUsesProviderTextSearchAndReportsMethod(t *testing.T) {
	fixture, factory := categoryFixture(t, true)
	result := runCategories(t, factory, "categories", "mountain", "--page", "2", "--page-size", "24")
	if result.status != 0 || result.stderr != "" || !strings.Contains(result.stdout, `"search_method":"provider"`) {
		t.Fatalf("category search = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	if len(fixture.categorySearchRequests) != 1 || fixture.categorySearchRequests[0].Query != "mountain" || fixture.categorySearchRequests[0].Page != (provider.PageRequest{Number: 2, Size: 24}) {
		t.Errorf("search request = %#v", fixture.categorySearchRequests)
	}
	if len(fixture.categoriesRequests) != 0 {
		t.Error("provider-native search loaded the local tree")
	}
	table := runCategories(t, factory, "categories", "mountain", "-o", "table")
	if table.status != 0 || !strings.Contains(table.stdout, "Search method:  provider") {
		t.Errorf("table output = %q; stderr = %q", table.stdout, table.stderr)
	}
}

func TestCategoriesUsesCaseInsensitiveLocalTreeSearchAndReportsMethod(t *testing.T) {
	fixture, factory := categoryFixture(t, false)
	fixture.categoriesResult.Items = append(fixture.categoriesResult.Items,
		provider.Category{ID: "helmets", Name: "Road Helmets", Path: "Equipment / Helmets", ParentID: "equipment"},
		provider.Category{ID: "shoes", Name: "Shoes", Path: "Equipment / Shoes", ParentID: "equipment"},
	)
	result := runCategories(t, factory, "categories", "HELMET", "--page", "1", "--page-size", "24")
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("local search = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	var value struct {
		Data struct {
			Items        []provider.Category `json:"items"`
			SearchMethod string              `json:"search_method"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &value); err != nil {
		t.Fatal(err)
	}
	if value.Data.SearchMethod != "local" || len(value.Data.Items) != 1 || value.Data.Items[0].ID != "helmets" {
		t.Errorf("local result = %#v", value.Data)
	}
	if len(fixture.categoriesRequests) != 1 || !fixture.categoriesRequests[0].Recursive {
		t.Errorf("fallback request = %#v", fixture.categoriesRequests)
	}
	if len(fixture.categorySearchRequests) != 0 {
		t.Error("local fallback called unsupported provider search")
	}
}

func TestCategoryItemsPassesFiltersSortAndPage(t *testing.T) {
	fixture, factory := categoryFixture(t, true)
	result := runCategories(t, factory, "category-items", "bikes", "--filter", "sale=true", "--sort", "price-asc", "--page", "2", "--page-size", "24", "--refresh", "--stale-if-error", "--interactive")
	if result.status != 0 || result.stderr != "" || !strings.Contains(result.stdout, `"id":"product-1"`) {
		t.Fatalf("category items = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	if len(fixture.categoryItemsRequests) != 1 {
		t.Fatalf("category item requests = %d", len(fixture.categoryItemsRequests))
	}
	request := fixture.categoryItemsRequests[0]
	wantFilters := []provider.Filter{{Key: "sale", Value: "true"}}
	if request.CategoryID != "bikes" || !reflect.DeepEqual(request.Filters, wantFilters) || request.Sort == nil || request.Sort.Value != "price-asc" || request.Page != (provider.PageRequest{Number: 2, Size: 24}) {
		t.Errorf("category item request = %#v", request)
	}
	if !request.Cache.Refresh || !request.Cache.StaleIfError || !request.Interactive || request.Resources == nil {
		t.Errorf("request policy = %#v", request.Request)
	}
}

func TestCategoriesRejectsConflictsAndInvalidProviderTrees(t *testing.T) {
	fixture, factory := categoryFixture(t, false)
	assertErrorCode(t, runCategories(t, factory, "categories", "bike", "--parent", "root"), provider.ErrorCodeInvalidFilter)
	if len(fixture.categoriesRequests) != 0 {
		t.Error("conflicting search loaded categories")
	}
	fixture.categoriesResult.Items[0].ID = ""
	assertErrorCode(t, runCategories(t, factory, "categories"), provider.ErrorCodeInvalidProviderResult)
}

func categoryFixture(t *testing.T, nativeSearch bool) (*categoryFixtureProvider, *coreapp.Factory) {
	t.Helper()
	total, pages, next := 1, 3, true
	helpCapabilities := []provider.CapabilityHelp{
		{Name: provider.CapabilityCategories, Supported: true},
		{Name: provider.CapabilityCategoryItems, Supported: true},
		{Name: provider.CapabilityCategorySearch, Supported: nativeSearch},
	}
	fixture := &categoryFixtureProvider{
		help: provider.Help{
			Name: "fixture", Capabilities: helpCapabilities,
			Filters:    []provider.FilterDefinition{{Key: "sale", Type: provider.FilterTypeBoolean, AppliesTo: []provider.CapabilityName{provider.CapabilityCategoryItems}}},
			SortModes:  []provider.SortMode{{Value: "price-asc", AppliesTo: []provider.CapabilityName{provider.CapabilityCategoryItems}}},
			Pagination: &provider.PaginationHelp{Mode: provider.PaginationPageNumber, FirstPage: 1, DefaultPageSize: 24, SupportedPageSizes: []int{24, 48}, ReportsTotalItems: true, ReportsTotalPages: true},
		},
		categoriesResult:     provider.CategoryPage{Items: []provider.Category{{ID: "bikes", Name: "Bikes", Path: "Bikes", HasChildren: true}}, Page: provider.PageInfo{Number: 1, Size: 24, TotalItems: &total, TotalPages: &pages, HasNext: &next}},
		categorySearchResult: provider.CategoryPage{Items: []provider.Category{{ID: "mountain", Name: "Mountain Bikes", Path: "Bikes / Mountain", ParentID: "bikes"}}, Page: provider.PageInfo{Number: 2, Size: 24, TotalItems: &total, TotalPages: &pages, HasNext: &next}},
		categoryItemsResult:  provider.ProductPage{Items: []provider.ProductSummary{{ID: "product-1", Name: "Bike", DetailLevel: provider.DetailLevelSummary}}, Page: provider.PageInfo{Number: 2, Size: 24, TotalItems: &total, TotalPages: &pages, HasNext: &next}},
	}
	capabilities := []provider.CapabilityName{provider.CapabilityCategories, provider.CapabilityCategoryItems}
	if nativeSearch {
		capabilities = append(capabilities, provider.CapabilityCategorySearch)
	}
	registry := provider.NewRegistry()
	if err := registry.Register(provider.Registration{Name: "fixture", SDKAPIVersion: provider.APIVersion, Implementation: fixture, Capabilities: capabilities}); err != nil {
		t.Fatal(err)
	}
	return fixture, coreapp.NewFactory(registry.Resolve)
}

func runCategories(t *testing.T, factory *coreapp.Factory, args ...string) commandResult {
	t.Helper()
	args = append(args, "--provider", "fixture")
	return runProviderHelpWithCache(t, factory, filepath.Join(t.TempDir(), "cache.db"), args...)
}

var _ provider.HelpProvider = (*categoryFixtureProvider)(nil)
var _ provider.CategoryListProvider = (*categoryFixtureProvider)(nil)
var _ provider.CategorySearchProvider = (*categoryFixtureProvider)(nil)
var _ provider.CategoryItemsProvider = (*categoryFixtureProvider)(nil)
