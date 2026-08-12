package cli

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	coreapp "github.com/kostyay/ecom/internal/app"
	"github.com/kostyay/ecom/provider"
)

type brandDealFixtureProvider struct {
	help                provider.Help
	brandsResult        provider.BrandPage
	brandSearchResult   provider.BrandPage
	brandItemsResult    provider.ProductPage
	dealsResult         provider.DealPage
	brandsRequests      []provider.BrandListRequest
	brandSearchRequests []provider.BrandSearchRequest
	brandItemsRequests  []provider.BrandItemsRequest
	dealsRequests       []provider.DealsRequest
}

func (fixture *brandDealFixtureProvider) Help(context.Context, provider.HelpRequest) (provider.HelpResult, error) {
	return provider.HelpResult{Help: fixture.help}, nil
}

func (fixture *brandDealFixtureProvider) Brands(_ context.Context, request provider.BrandListRequest) (provider.BrandPage, error) {
	fixture.brandsRequests = append(fixture.brandsRequests, request)
	return fixture.brandsResult, nil
}

func (fixture *brandDealFixtureProvider) SearchBrands(_ context.Context, request provider.BrandSearchRequest) (provider.BrandPage, error) {
	fixture.brandSearchRequests = append(fixture.brandSearchRequests, request)
	return fixture.brandSearchResult, nil
}

func (fixture *brandDealFixtureProvider) BrandItems(_ context.Context, request provider.BrandItemsRequest) (provider.ProductPage, error) {
	fixture.brandItemsRequests = append(fixture.brandItemsRequests, request)
	return fixture.brandItemsResult, nil
}

func (fixture *brandDealFixtureProvider) Deals(_ context.Context, request provider.DealsRequest) (provider.DealPage, error) {
	fixture.dealsRequests = append(fixture.dealsRequests, request)
	return fixture.dealsResult, nil
}

func TestBrandsListsAndUsesProviderTextSearch(t *testing.T) {
	fixture, factory := brandDealFixture(t, true, true)

	listed := runBrandDeals(t, factory, "brands", "--page", "1", "--page-size", "24")
	if listed.status != 0 || listed.stderr != "" || !strings.Contains(listed.stdout, `"id":"acme"`) {
		t.Fatalf("brand list = status %d, stdout %q, stderr %q", listed.status, listed.stdout, listed.stderr)
	}
	if len(fixture.brandsRequests) != 1 || fixture.brandsRequests[0].Page != (provider.PageRequest{Number: 1, Size: 24}) {
		t.Errorf("brand list requests = %#v", fixture.brandsRequests)
	}

	searched := runBrandDeals(t, factory, "brands", "mountain", "--page", "2", "--page-size", "24", "--refresh", "--stale-if-error", "--interactive")
	if searched.status != 0 || searched.stderr != "" || !strings.Contains(searched.stdout, `"search_method":"provider"`) {
		t.Fatalf("brand search = status %d, stdout %q, stderr %q", searched.status, searched.stdout, searched.stderr)
	}
	if len(fixture.brandSearchRequests) != 1 {
		t.Fatalf("brand search requests = %d", len(fixture.brandSearchRequests))
	}
	request := fixture.brandSearchRequests[0]
	if request.Query != "mountain" || request.Page != (provider.PageRequest{Number: 2, Size: 24}) || !request.Cache.Refresh || !request.Cache.StaleIfError || !request.Interactive || request.Resources == nil {
		t.Errorf("brand search request = %#v", request)
	}
}

func TestBrandsUsesCaseInsensitiveLocalTextSearch(t *testing.T) {
	fixture, factory := brandDealFixture(t, false, true)
	fixture.brandsResult.Items = append(fixture.brandsResult.Items, provider.Brand{ID: "other", Name: "Other"})

	result := runBrandDeals(t, factory, "brands", "ACM", "-o", "table")
	if result.status != 0 || result.stderr != "" || !strings.Contains(result.stdout, "Search method:  local") || !strings.Contains(result.stdout, "Acme") || strings.Contains(result.stdout, "Other") {
		t.Fatalf("local brand search = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	if len(fixture.brandsRequests) != 1 || len(fixture.brandSearchRequests) != 0 {
		t.Errorf("search requests = list %#v, native %#v", fixture.brandsRequests, fixture.brandSearchRequests)
	}
}

func TestBrandItemsPassesFiltersSortPageAndPolicy(t *testing.T) {
	fixture, factory := brandDealFixture(t, true, true)
	result := runBrandDeals(t, factory, "brand-items", "acme", "--filter", "available=true", "--sort", "price-asc", "--page", "2", "--page-size", "24", "--refresh", "--stale-if-error", "--interactive")
	if result.status != 0 || result.stderr != "" || !strings.Contains(result.stdout, `"id":"product-1"`) {
		t.Fatalf("brand items = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	if len(fixture.brandItemsRequests) != 1 {
		t.Fatalf("brand item requests = %d", len(fixture.brandItemsRequests))
	}
	request := fixture.brandItemsRequests[0]
	wantFilters := []provider.Filter{{Key: "available", Value: "true"}}
	if request.BrandID != "acme" || !reflect.DeepEqual(request.Filters, wantFilters) || request.Sort == nil || request.Sort.Value != "price-asc" || request.Page != (provider.PageRequest{Number: 2, Size: 24}) {
		t.Errorf("brand item request = %#v", request)
	}
	if !request.Cache.Refresh || !request.Cache.StaleIfError || !request.Interactive || request.Resources == nil {
		t.Errorf("brand item policy = %#v", request.Request)
	}
}

func TestDealsPassesArgumentsAndWritesNativeReductions(t *testing.T) {
	fixture, factory := brandDealFixture(t, true, true)
	result := runBrandDeals(t, factory, "deals", "--filter", "minimum-discount=20", "--sort", "discount-desc", "--page", "2", "--page-size", "24")
	if result.status != 0 || result.stderr != "" || !strings.Contains(result.stdout, `"original_price"`) {
		t.Fatalf("deals = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	if len(fixture.dealsRequests) != 1 {
		t.Fatalf("deal requests = %d", len(fixture.dealsRequests))
	}
	request := fixture.dealsRequests[0]
	wantFilters := []provider.Filter{{Key: "minimum-discount", Value: "20"}}
	if !reflect.DeepEqual(request.Filters, wantFilters) || request.Sort == nil || request.Sort.Value != "discount-desc" || request.Page != (provider.PageRequest{Number: 2, Size: 24}) {
		t.Errorf("deal request = %#v", request)
	}

	table := runBrandDeals(t, factory, "deals", "-o", "table")
	if table.status != 0 || !strings.Contains(table.stdout, "Sale Helmet") || !strings.Contains(table.stdout, "$100.00") {
		t.Errorf("deal table = status %d, stdout %q, stderr %q", table.status, table.stdout, table.stderr)
	}
	jsonPath := runBrandDeals(t, factory, "deals", "-o", `jsonpath={.data.items[*].product.name}`)
	if jsonPath.status != 0 || jsonPath.stdout != "Sale Helmet" || jsonPath.stderr != "" {
		t.Errorf("deal JSONPath = status %d, stdout %q, stderr %q", jsonPath.status, jsonPath.stdout, jsonPath.stderr)
	}
}

func TestBrandAndDealCommandsRejectUnsupportedCapabilitiesAndInvalidDeals(t *testing.T) {
	_, unsupportedFactory := brandDealFixture(t, false, false)
	for _, args := range [][]string{{"brands"}, {"brands", "acme"}, {"brand-items", "acme"}, {"deals"}} {
		assertErrorCode(t, runBrandDeals(t, unsupportedFactory, args...), provider.ErrorCodeCapabilityUnavailable)
	}

	fixture, factory := brandDealFixture(t, true, true)
	fixture.dealsResult.Items[0].Product.OriginalPrice = nil
	assertErrorCode(t, runBrandDeals(t, factory, "deals"), provider.ErrorCodeInvalidProviderResult)
	if len(fixture.dealsRequests) != 1 {
		t.Errorf("deal requests = %d, want 1", len(fixture.dealsRequests))
	}
}

func TestBrandAndDealCommandsValidateProviderHelpArguments(t *testing.T) {
	fixture, factory := brandDealFixture(t, true, true)
	for _, args := range [][]string{
		{"brands", "--page-size", "25"},
		{"brand-items", "acme", "--filter", "minimum-discount=20"},
		{"brand-items", "acme", "--sort", "discount-desc"},
		{"deals", "--filter", "available=true"},
		{"deals", "--sort", "price-asc"},
	} {
		assertErrorCode(t, runBrandDeals(t, factory, args...), provider.ErrorCodeInvalidFilter)
	}
	if len(fixture.brandsRequests)+len(fixture.brandItemsRequests)+len(fixture.dealsRequests) != 0 {
		t.Error("invalid arguments called a provider operation")
	}
}

func brandDealFixture(t *testing.T, nativeSearch, supported bool) (*brandDealFixtureProvider, *coreapp.Factory) {
	t.Helper()
	total, pages, next := 1, 3, false
	capabilityHelp := []provider.CapabilityHelp{
		{Name: provider.CapabilityBrands, Supported: supported},
		{Name: provider.CapabilityBrandSearch, Supported: supported && nativeSearch},
		{Name: provider.CapabilityBrandItems, Supported: supported},
		{Name: provider.CapabilityDeals, Supported: supported},
	}
	current := &provider.Money{Amount: "80.00", Currency: "CAD", Display: "$80.00"}
	original := &provider.Money{Amount: "100.00", Currency: "CAD", Display: "$100.00"}
	fixture := &brandDealFixtureProvider{
		help: provider.Help{
			Name: "fixture", Capabilities: capabilityHelp,
			Filters: []provider.FilterDefinition{
				{Key: "available", Type: provider.FilterTypeBoolean, AppliesTo: []provider.CapabilityName{provider.CapabilityBrandItems}},
				{Key: "minimum-discount", Type: provider.FilterTypeInteger, AppliesTo: []provider.CapabilityName{provider.CapabilityDeals}},
			},
			SortModes: []provider.SortMode{
				{Value: "price-asc", AppliesTo: []provider.CapabilityName{provider.CapabilityBrandItems}},
				{Value: "discount-desc", AppliesTo: []provider.CapabilityName{provider.CapabilityDeals}},
			},
			Pagination: &provider.PaginationHelp{Mode: provider.PaginationPageNumber, FirstPage: 1, DefaultPageSize: 24, SupportedPageSizes: []int{24, 48}, ReportsTotalItems: true, ReportsTotalPages: true},
		},
		brandsResult:      provider.BrandPage{Items: []provider.Brand{{ID: "acme", Name: "Acme", URL: "https://shop.example/brands/acme"}}, Page: provider.PageInfo{Number: 1, Size: 24, TotalItems: &total, TotalPages: &pages, HasNext: &next}},
		brandSearchResult: provider.BrandPage{Items: []provider.Brand{{ID: "mountain", Name: "Mountain Parts"}}, Page: provider.PageInfo{Number: 2, Size: 24, TotalItems: &total, TotalPages: &pages, HasNext: &next}},
		brandItemsResult:  provider.ProductPage{Items: []provider.ProductSummary{{ID: "product-1", Name: "Helmet", DetailLevel: provider.DetailLevelSummary}}, Page: provider.PageInfo{Number: 2, Size: 24, TotalItems: &total, TotalPages: &pages, HasNext: &next}},
		dealsResult: provider.DealPage{Items: []provider.Deal{{Product: provider.ProductSummary{
			ID: "deal-1", Name: "Sale Helmet", Price: current, OriginalPrice: original, DetailLevel: provider.DetailLevelSummary,
		}}}, Page: provider.PageInfo{Number: 2, Size: 24, TotalItems: &total, TotalPages: &pages, HasNext: &next}},
	}

	capabilities := []provider.CapabilityName(nil)
	if supported {
		capabilities = append(capabilities, provider.CapabilityBrands, provider.CapabilityBrandItems, provider.CapabilityDeals)
		if nativeSearch {
			capabilities = append(capabilities, provider.CapabilityBrandSearch)
		}
	}
	registry := provider.NewRegistry()
	if err := registry.Register(provider.Registration{Name: "fixture", SDKAPIVersion: provider.APIVersion, Implementation: fixture, Capabilities: capabilities}); err != nil {
		t.Fatal(err)
	}
	return fixture, coreapp.NewFactory(registry.Resolve)
}

func runBrandDeals(t *testing.T, factory *coreapp.Factory, args ...string) commandResult {
	t.Helper()
	args = append(args, "--provider", "fixture")
	return runProviderHelpWithCache(t, factory, filepath.Join(t.TempDir(), "cache.db"), args...)
}

var _ provider.HelpProvider = (*brandDealFixtureProvider)(nil)
var _ provider.BrandListProvider = (*brandDealFixtureProvider)(nil)
var _ provider.BrandSearchProvider = (*brandDealFixtureProvider)(nil)
var _ provider.BrandItemsProvider = (*brandDealFixtureProvider)(nil)
var _ provider.DealsProvider = (*brandDealFixtureProvider)(nil)
