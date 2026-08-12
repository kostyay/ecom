package bikediscount

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/kostyay/ecom/provider"
)

func TestBrandsListsFixtureWithCanonicalIdentity(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{
		Body: readCategoryFixture(t, "brands.html"),
	}}}
	page, err := (implementation{}).Brands(t.Context(), provider.BrandListRequest{
		Request: bikeDiscountRequest(service),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(page.Warnings) != 0 {
		t.Fatalf("brand page = %#v", page)
	}
	shimano := page.Items[0]
	if shimano.ID != "shimano" || shimano.Name != "Shimano" || shimano.URL != bikeDiscountBaseURL+"/en/shimano" {
		t.Errorf("Shimano brand = %#v", shimano)
	}
	if page.Page.Number != 1 || page.Page.Size != 48 || page.Page.HasNext == nil || *page.Page.HasNext {
		t.Errorf("brand page info = %#v", page.Page)
	}
	if len(service.requests) != 1 || service.requests[0].URL != bikeDiscountBaseURL+"/en/brands" || len(service.requests[0].Query) != 0 {
		t.Errorf("brand resource request = %#v", service.requests)
	}
}

func TestBrandsReturnsCompleteIndexForLocalQueryFallback(t *testing.T) {
	document := []byte(`<section><h2>A</h2><a href="/en/absolute-black">absoluteBLACK</a></section>
		<section><h2>S</h2><a href="/en/shimano">Shimano</a><a href="/en/sram">SRAM</a></section>`)
	service := &fakeResourceService{responses: []provider.ResourceResponse{{Body: document}}}
	page, err := (implementation{}).Brands(t.Context(), provider.BrandListRequest{Request: bikeDiscountRequest(service)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("complete brand items = %#v", page.Items)
	}
	query := "SHIM"
	matches := make([]provider.Brand, 0)
	for _, brand := range page.Items {
		if strings.Contains(strings.ToLower(brand.Name), strings.ToLower(query)) {
			matches = append(matches, brand)
		}
	}
	if len(matches) != 1 || matches[0].ID != "shimano" {
		t.Errorf("local query matches = %#v", matches)
	}
}

func TestExtractBrandsWarnsForMalformedAndDuplicateNodes(t *testing.T) {
	document := []byte(`<section><h2>S</h2>
		<a href="/en/shimano">Shimano</a>
		<a>Missing URL</a>
		<a href="https://example.com/en/foreign">Foreign</a>
		<a href="/en/shimano">Duplicate Shimano</a>
		<a href="/en/sram">SRAM</a>
	</section><section><h2>Not a group</h2><a href="/en/ignored">Ignored</a></section>`)
	result, err := extractBrands(document, bikeDiscountBaseURL+"/en/brands")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.items) != 2 || result.items[0].ID != "shimano" || result.items[1].ID != "sram" {
		t.Fatalf("parsed brands = %#v", result.items)
	}
	if len(result.warnings) != 1 {
		t.Fatalf("brand warnings = %#v", result.warnings)
	}
	warning := result.warnings[0]
	if warning.Code != provider.WarningCodePartialParsing || warning.FoundCount == nil || *warning.FoundCount != 5 ||
		warning.ParsedCount == nil || *warning.ParsedCount != 2 || warning.Cause() == nil {
		t.Errorf("partial brand warning = %#v", warning)
	}
}

func TestBrandsFailsWhenNoUsefulBrandExists(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{Body: []byte(`<section><h2>S</h2><a>Broken</a></section>`)}}}
	_, err := (implementation{}).Brands(t.Context(), provider.BrandListRequest{Request: bikeDiscountRequest(service)})
	if !errors.Is(err, provider.ErrorCodeHTTPFailure) {
		t.Fatalf("Brands() error = %v", err)
	}
}

func TestBrandItemsUsesListingQueryAndProductParser(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{
		Body: readCategoryFixture(t, "listing_page_1.html"),
	}}}
	page, err := (implementation{}).BrandItems(t.Context(), provider.BrandItemsRequest{
		Request: bikeDiscountRequest(service), BrandID: " Shimano ",
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
	if len(page.Items) != 1 || page.Items[0].Name != "Yamaha 500 Wh 36V/13.6Ah Frame Battery" {
		t.Fatalf("brand products = %#v", page.Items)
	}
	if page.Page.Number != 2 || page.Page.Size != 48 || page.Page.HasNext == nil || !*page.Page.HasNext {
		t.Errorf("brand product page = %#v", page.Page)
	}
	wantQuery := []provider.RequestValue{
		{Name: "p", Values: []string{"2"}}, {Name: "n", Values: []string{"48"}},
		{Name: "order", Values: []string{"standard"}},
		{Name: "manufacturer", Values: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		{Name: "properties", Values: []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb|cccccccccccccccccccccccccccccccc"}},
	}
	if len(service.requests) != 1 || service.requests[0].URL != bikeDiscountBaseURL+"/en/shimano" || !reflect.DeepEqual(service.requests[0].Query, wantQuery) {
		t.Errorf("brand item resource request = %#v", service.requests)
	}
}

func TestBrandItemsReturnsPartialProductWarning(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{Body: readCategoryFixture(t, "listing_partial.html")}}}
	page, err := (implementation{}).BrandItems(t.Context(), provider.BrandItemsRequest{
		Request: bikeDiscountRequest(service), BrandID: "shimano",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(page.Warnings) != 1 || page.Warnings[0].Code != provider.WarningCodePartialParsing {
		t.Fatalf("partial brand item page = %#v", page)
	}
}

func TestBrandOperationsRejectInvalidIdentifiersAndPages(t *testing.T) {
	implementation := implementation{}
	for _, request := range []provider.BrandListRequest{
		{Request: bikeDiscountRequest(&fakeResourceService{}), Page: provider.PageRequest{Number: 2, Size: 48}},
		{Request: bikeDiscountRequest(&fakeResourceService{}), Page: provider.PageRequest{Number: 1, Size: 24}},
	} {
		_, err := implementation.Brands(context.Background(), request)
		if !errors.Is(err, provider.ErrorCodeInvalidFilter) {
			t.Errorf("Brands(%#v) error = %v", request.Page, err)
		}
	}
	for _, id := range []string{"", "/en/shimano", "shimano/deore", "shimano?x=1", "Shima no"} {
		_, err := implementation.BrandItems(context.Background(), provider.BrandItemsRequest{
			Request: bikeDiscountRequest(&fakeResourceService{}), BrandID: id,
		})
		if !errors.Is(err, provider.ErrorCodeInvalidFilter) {
			t.Errorf("BrandItems(%q) error = %v", id, err)
		}
	}
}

func TestCanonicalBrandIdentityRejectsNonCanonicalLinks(t *testing.T) {
	for _, reference := range []string{
		"http://www.bike-discount.de/en/shimano", "https://example.com/en/shimano",
		"/de/shimano", "/en/shimano/deore", "/en/shimano?x=1", "/en/shimano#parts",
	} {
		id, brandURL := canonicalBrandIdentity(reference, bikeDiscountBaseURL+"/en/brands")
		if id != "" || brandURL != "" {
			t.Errorf("canonicalBrandIdentity(%q) = %q, %q", reference, id, brandURL)
		}
	}
}
