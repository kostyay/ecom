package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/kostyay/ecom/provider"
)

type minimalFixtureProvider struct{}

func (*minimalFixtureProvider) Help(context.Context, provider.HelpRequest) (provider.HelpResult, error) {
	return provider.HelpResult{Help: provider.Help{Name: "minimal"}}, nil
}

type fullFixtureProvider struct {
	searchRequest provider.SearchRequest
}

func (*fullFixtureProvider) Help(context.Context, provider.HelpRequest) (provider.HelpResult, error) {
	return provider.HelpResult{Help: provider.Help{Name: "full"}}, nil
}

func (p *fullFixtureProvider) Search(_ context.Context, request provider.SearchRequest) (provider.ProductPage, error) {
	p.searchRequest = request
	return provider.ProductPage{Items: []provider.ProductSummary{{ID: "item-1"}}}, nil
}

func (*fullFixtureProvider) Categories(context.Context, provider.CategoryListRequest) (provider.CategoryPage, error) {
	return provider.CategoryPage{}, nil
}

func (*fullFixtureProvider) SearchCategories(context.Context, provider.CategorySearchRequest) (provider.CategoryPage, error) {
	return provider.CategoryPage{}, nil
}

func (*fullFixtureProvider) CategoryItems(context.Context, provider.CategoryItemsRequest) (provider.ProductPage, error) {
	return provider.ProductPage{}, nil
}

func (*fullFixtureProvider) Brands(context.Context, provider.BrandListRequest) (provider.BrandPage, error) {
	return provider.BrandPage{}, nil
}

func (*fullFixtureProvider) SearchBrands(context.Context, provider.BrandSearchRequest) (provider.BrandPage, error) {
	return provider.BrandPage{}, nil
}

func (*fullFixtureProvider) BrandItems(context.Context, provider.BrandItemsRequest) (provider.ProductPage, error) {
	return provider.ProductPage{}, nil
}

func (*fullFixtureProvider) Deals(context.Context, provider.DealsRequest) (provider.DealPage, error) {
	return provider.DealPage{}, nil
}

func (*fullFixtureProvider) Filters(context.Context, provider.FiltersRequest) (provider.FiltersResult, error) {
	return provider.FiltersResult{}, nil
}

func (*fullFixtureProvider) Item(context.Context, provider.ItemRequest) (provider.ItemResult, error) {
	return provider.ItemResult{}, nil
}

var (
	_ provider.HelpProvider           = (*minimalFixtureProvider)(nil)
	_ provider.HelpProvider           = (*fullFixtureProvider)(nil)
	_ provider.SearchProvider         = (*fullFixtureProvider)(nil)
	_ provider.CategoryListProvider   = (*fullFixtureProvider)(nil)
	_ provider.CategorySearchProvider = (*fullFixtureProvider)(nil)
	_ provider.CategoryItemsProvider  = (*fullFixtureProvider)(nil)
	_ provider.BrandListProvider      = (*fullFixtureProvider)(nil)
	_ provider.BrandSearchProvider    = (*fullFixtureProvider)(nil)
	_ provider.BrandItemsProvider     = (*fullFixtureProvider)(nil)
	_ provider.DealsProvider          = (*fullFixtureProvider)(nil)
	_ provider.FiltersProvider        = (*fullFixtureProvider)(nil)
	_ provider.ItemProvider           = (*fullFixtureProvider)(nil)
)

var allCapabilities = []provider.CapabilityName{
	provider.CapabilitySearch,
	provider.CapabilityCategories,
	provider.CapabilityCategorySearch,
	provider.CapabilityCategoryItems,
	provider.CapabilityBrands,
	provider.CapabilityBrandSearch,
	provider.CapabilityBrandItems,
	provider.CapabilityDeals,
	provider.CapabilityFilters,
	provider.CapabilityItem,
	provider.CapabilityVariantSelection,
}

func TestFullFixtureDeclaresAndDispatchesAllCapabilities(t *testing.T) {
	implementation := &fullFixtureProvider{}
	registry := provider.NewRegistry()
	if err := registry.Register(provider.Registration{
		Name:           "full",
		SDKAPIVersion:  provider.APIVersion,
		Implementation: implementation,
		Capabilities:   allCapabilities,
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	registered, ok := registry.Lookup("full")
	if !ok {
		t.Fatal("Lookup() did not find full provider")
	}
	for _, capability := range allCapabilities {
		if !registered.Supports(capability) {
			t.Errorf("Supports(%q) = false, want true", capability)
		}
	}

	request := provider.SearchRequest{
		Request: provider.Request{
			Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"},
			Cache:  provider.CachePolicy{Refresh: true, StaleIfError: true}, Interactive: true,
		},
		Query:   "helmet",
		Filters: []provider.Filter{{Key: "discount", Value: "true"}},
		Sort:    &provider.Sort{Value: "price-asc"},
		Page:    provider.PageRequest{Number: 2, Size: 48},
	}
	result, err := registered.Search(context.Background(), request)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if !reflect.DeepEqual(implementation.searchRequest, request) {
		t.Fatalf("Search() request = %#v, want %#v", implementation.searchRequest, request)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "item-1" {
		t.Fatalf("Search() result = %#v, want item-1", result)
	}

	capabilities := registered.Capabilities()
	capabilities[0] = provider.CapabilityName("changed")
	if !registered.Supports(provider.CapabilitySearch) {
		t.Fatal("Capabilities() exposed mutable provider state")
	}
}

func TestOperationTypesPreserveCommonInputsAndProviderPageData(t *testing.T) {
	totalItems := 101
	totalPages := 3
	hasNext := true
	value := struct {
		Request provider.SearchRequest `json:"request"`
		Result  provider.ProductPage   `json:"result"`
	}{
		Request: provider.SearchRequest{
			Request: provider.Request{
				Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"},
				Cache:  provider.CachePolicy{Refresh: true, StaleIfError: true}, Interactive: true,
			},
			Query:   "trail helmet",
			Filters: []provider.Filter{{Key: "brand", Value: "example"}},
			Sort:    &provider.Sort{Value: "discount-desc"},
			Page:    provider.PageRequest{Number: 2, Size: 48},
		},
		Result: provider.ProductPage{
			Items: []provider.ProductSummary{{ID: "item-1"}},
			Page: provider.PageInfo{
				Number:       2,
				Size:         48,
				TotalItems:   &totalItems,
				TotalPages:   &totalPages,
				HasNext:      &hasNext,
				ProviderData: provider.Data{"example": json.RawMessage(`{"native_page":"p2"}`)},
			},
		},
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var decoded typeofOperationRoundTrip
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	want := typeofOperationRoundTrip(value)
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("round trip mismatch\ngot:  %#v\nwant: %#v", decoded, want)
	}
}

type typeofOperationRoundTrip struct {
	Request provider.SearchRequest `json:"request"`
	Result  provider.ProductPage   `json:"result"`
}

func TestMinimalFixtureReturnsCapabilityUnavailableWithoutAssertions(t *testing.T) {
	registry := provider.NewRegistry()
	if err := registry.Register(provider.Registration{
		Name: "minimal", SDKAPIVersion: provider.APIVersion, Implementation: &minimalFixtureProvider{},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	registered, ok := registry.Lookup("minimal")
	if !ok {
		t.Fatal("Lookup() did not find minimal provider")
	}
	if len(registered.Capabilities()) != 0 || registered.Supports(provider.CapabilitySearch) {
		t.Fatalf("minimal capabilities = %#v, want none", registered.Capabilities())
	}

	ctx := context.Background()
	assertCapabilityUnavailable(t, provider.CapabilitySearch, func() error {
		_, err := registered.Search(ctx, provider.SearchRequest{})
		return err
	})
	assertCapabilityUnavailable(t, provider.CapabilityCategories, func() error {
		_, err := registered.Categories(ctx, provider.CategoryListRequest{})
		return err
	})
	assertCapabilityUnavailable(t, provider.CapabilityCategorySearch, func() error {
		_, err := registered.SearchCategories(ctx, provider.CategorySearchRequest{})
		return err
	})
	assertCapabilityUnavailable(t, provider.CapabilityCategoryItems, func() error {
		_, err := registered.CategoryItems(ctx, provider.CategoryItemsRequest{})
		return err
	})
	assertCapabilityUnavailable(t, provider.CapabilityBrands, func() error {
		_, err := registered.Brands(ctx, provider.BrandListRequest{})
		return err
	})
	assertCapabilityUnavailable(t, provider.CapabilityBrandSearch, func() error {
		_, err := registered.SearchBrands(ctx, provider.BrandSearchRequest{})
		return err
	})
	assertCapabilityUnavailable(t, provider.CapabilityBrandItems, func() error {
		_, err := registered.BrandItems(ctx, provider.BrandItemsRequest{})
		return err
	})
	assertCapabilityUnavailable(t, provider.CapabilityDeals, func() error {
		_, err := registered.Deals(ctx, provider.DealsRequest{})
		return err
	})
	assertCapabilityUnavailable(t, provider.CapabilityFilters, func() error {
		_, err := registered.Filters(ctx, provider.FiltersRequest{})
		return err
	})
	assertCapabilityUnavailable(t, provider.CapabilityItem, func() error {
		_, err := registered.Item(ctx, provider.ItemRequest{})
		return err
	})
}

func TestRegistrationRejectsCapabilitiesWithoutMatchingInterfaces(t *testing.T) {
	tests := []struct {
		name         string
		capabilities []provider.CapabilityName
		want         error
	}{
		{name: "missing operation interface", capabilities: []provider.CapabilityName{provider.CapabilitySearch}, want: provider.ErrInvalidProviderImplementation},
		{name: "duplicate", capabilities: []provider.CapabilityName{provider.CapabilitySearch, provider.CapabilitySearch}, want: provider.ErrInvalidCapability},
		{name: "unknown", capabilities: []provider.CapabilityName{"unknown"}, want: provider.ErrInvalidCapability},
		{name: "variant without item declaration", capabilities: []provider.CapabilityName{provider.CapabilityVariantSelection}, want: provider.ErrInvalidCapability},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			implementation := any(&minimalFixtureProvider{})
			if test.name == "duplicate" || test.name == "variant without item declaration" {
				implementation = &fullFixtureProvider{}
			}
			err := provider.NewRegistry().Register(provider.Registration{
				Name: "fixture", SDKAPIVersion: provider.APIVersion, Implementation: implementation, Capabilities: test.capabilities,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("Register() error = %v, want errors.Is(_, %v)", err, test.want)
			}
		})
	}
}

func TestUndeclaredImplementedOperationIsUnavailable(t *testing.T) {
	registry := provider.NewRegistry()
	if err := registry.Register(provider.Registration{
		Name: "undeclared", SDKAPIVersion: provider.APIVersion, Implementation: &fullFixtureProvider{},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	registered, _ := registry.Lookup("undeclared")
	_, err := registered.Search(context.Background(), provider.SearchRequest{})
	if !errors.Is(err, provider.ErrorCodeCapabilityUnavailable) {
		t.Fatalf("Search() error = %v, want capability_unavailable", err)
	}
}

func TestItemRejectsUndeclaredVariantSelection(t *testing.T) {
	registry := provider.NewRegistry()
	if err := registry.Register(provider.Registration{
		Name:           "item-only",
		SDKAPIVersion:  provider.APIVersion,
		Implementation: &fullFixtureProvider{},
		Capabilities:   []provider.CapabilityName{provider.CapabilityItem},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	registered, _ := registry.Lookup("item-only")
	_, err := registered.Item(context.Background(), provider.ItemRequest{
		Variants: []provider.VariantSelection{{Key: "size", Value: "M"}},
	})
	if !errors.Is(err, provider.ErrorCodeCapabilityUnavailable) {
		t.Fatalf("Item() error = %v, want capability_unavailable", err)
	}
}

func assertCapabilityUnavailable(t *testing.T, capability provider.CapabilityName, operation func() error) {
	t.Helper()
	err := operation()
	if !errors.Is(err, provider.ErrorCodeCapabilityUnavailable) {
		t.Fatalf("%s error = %v, want capability_unavailable", capability, err)
	}
	code, ok := provider.ErrorCodeOf(err)
	if !ok || code != provider.ErrorCodeCapabilityUnavailable {
		t.Fatalf("ErrorCodeOf(%s) = %q, %t", capability, code, ok)
	}
}
