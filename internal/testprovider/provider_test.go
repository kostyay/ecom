package testprovider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kostyay/ecom/internal/testprovider"
	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/provider/conformance"
)

var market = provider.Market{Country: "DE", Language: "en", Currency: "EUR"}

func TestProviderConformance(t *testing.T) {
	conformance.Run(t, conformance.Suite{
		Registration: testprovider.Registration(),
		Cases: []conformance.OperationCase{
			{
				Name: "filtered second search page", Capability: provider.CapabilitySearch,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.Search(ctx, provider.SearchRequest{
						Market: market, Query: "helmet",
						Filters: []provider.Filter{{Key: "in-stock", Value: "true"}},
						Sort:    &provider.Sort{Value: "price-asc"}, Page: provider.PageRequest{Number: 2, Size: 1},
					})
				},
				Check: func(value any) error {
					if value.(provider.ProductPage).Items[0].ID != "road-helmet" {
						return errors.New("search did not return the expected second item")
					}
					return nil
				},
			},
			{
				Name: "partial warning", Capability: provider.CapabilitySearch, WantPartialWarning: true,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.Search(ctx, provider.SearchRequest{Market: market, Query: "partial-warning"})
				},
			},
			{
				Name: "invalid filter error", Capability: provider.CapabilitySearch, WantErrorCode: provider.ErrorCodeInvalidFilter,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.Search(ctx, provider.SearchRequest{Query: "helmet", Filters: []provider.Filter{{Key: "unknown", Value: "true"}}})
				},
			},
			{
				Name: "recursive categories", Capability: provider.CapabilityCategories,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.Categories(ctx, provider.CategoryListRequest{Recursive: true, Page: provider.PageRequest{Size: 2}})
				},
			},
			{
				Name: "native category search", Capability: provider.CapabilityCategorySearch,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.SearchCategories(ctx, provider.CategorySearchRequest{Query: "helmet"})
				},
			},
			{
				Name: "category products", Capability: provider.CapabilityCategoryItems,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.CategoryItems(ctx, provider.CategoryItemsRequest{Market: market, CategoryID: "helmets"})
				},
			},
			{
				Name: "brand page", Capability: provider.CapabilityBrands,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.Brands(ctx, provider.BrandListRequest{Page: provider.PageRequest{Number: 2, Size: 1}})
				},
			},
			{
				Name: "native brand search", Capability: provider.CapabilityBrandSearch,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.SearchBrands(ctx, provider.BrandSearchRequest{Query: "velo"})
				},
			},
			{
				Name: "brand products", Capability: provider.CapabilityBrandItems,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.BrandItems(ctx, provider.BrandItemsRequest{Market: market, BrandID: "acme", Filters: []provider.Filter{{Key: "color", Value: "red"}}})
				},
			},
			{
				Name: "native deals", Capability: provider.CapabilityDeals,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.Deals(ctx, provider.DealsRequest{Market: market, Filters: []provider.Filter{{Key: "min-discount", Value: "20"}}})
				},
			},
			{
				Name: "context filters", Capability: provider.CapabilityFilters,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.Filters(ctx, provider.FiltersRequest{Capability: provider.CapabilityDeals})
				},
			},
			{
				Name: "item by URL", Capability: provider.CapabilityItem,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.Item(ctx, provider.ItemRequest{Market: market, IDOrURL: "https://fixture.invalid/items/trail-helmet"})
				},
			},
			{
				Name: "exact variant", Capability: provider.CapabilityVariantSelection,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.Item(ctx, provider.ItemRequest{Market: market, IDOrURL: "trail-helmet", Variants: []provider.VariantSelection{{Key: "size", Value: "M"}, {Key: "color", Value: "black"}}})
				},
				Check: func(value any) error {
					item := value.(provider.ItemResult).Item
					if item.SelectedVariant == nil || item.SelectedVariant.ID != "trail-helmet-m" {
						return errors.New("exact variant was not selected")
					}
					return nil
				},
			},
			{
				Name: "unknown variant error", Capability: provider.CapabilityVariantSelection, WantErrorCode: provider.ErrorCodeVariantNotFound,
				Invoke: func(ctx context.Context, selected provider.Provider) (any, error) {
					return selected.Item(ctx, provider.ItemRequest{IDOrURL: "trail-helmet", Variants: []provider.VariantSelection{{Key: "size", Value: "XL"}}})
				},
			},
		},
	})
}

func TestRegistrationUsesExplicitRegistry(t *testing.T) {
	if _, exists := provider.Lookup(testprovider.Name); exists {
		t.Fatal("fixture provider was registered in the production registry")
	}
	registry := provider.NewRegistry()
	if err := registry.Register(testprovider.Registration()); err != nil {
		t.Fatal(err)
	}
	selected, err := registry.Resolve(testprovider.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !selected.Supports(provider.CapabilityVariantSelection) {
		t.Error("fixture provider does not support variant selection")
	}
}

func TestProviderSupportsWarningsAndStructuredErrors(t *testing.T) {
	registry := provider.NewRegistry()
	if err := registry.Register(testprovider.Registration()); err != nil {
		t.Fatal(err)
	}
	selected, _ := registry.Resolve(testprovider.Name)

	page, err := selected.Search(t.Context(), provider.SearchRequest{
		Market: provider.Market{Currency: "USD"}, Query: "partial-warning",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Warnings) != 2 || page.Warnings[0].Code != provider.WarningCodeCurrencyUnavailable || page.Warnings[1].Code != provider.WarningCodePartialParsing {
		t.Fatalf("warnings = %#v", page.Warnings)
	}

	_, err = selected.Item(t.Context(), provider.ItemRequest{IDOrURL: "missing"})
	if code, ok := provider.ErrorCodeOf(err); !ok || code != provider.ErrorCodeInvalidFilter {
		t.Fatalf("missing item error = %v, code = %q", err, code)
	}
}

func TestConfigValidation(t *testing.T) {
	registry := provider.NewRegistry()
	if err := registry.Register(testprovider.Registration()); err != nil {
		t.Fatal(err)
	}
	selected, _ := registry.Resolve(testprovider.Name)
	if err := selected.ValidateConfig(map[string]any{"page_size": 1}); err != nil {
		t.Fatal(err)
	}
	if err := selected.ValidateConfig(map[string]any{"page_size": 48}); !errors.Is(err, provider.ErrorCodeInvalidProviderConfig) {
		t.Fatalf("invalid config error = %v", err)
	}
}
