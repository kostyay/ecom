package bikediscount

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/provider/conformance"
)

func TestProviderConformanceOffline(t *testing.T) {
	resources := conformance.NewFixtureService(
		conformanceFixture(t, "search_current.html", bikeDiscountBaseURL+"/en/search", []provider.RequestValue{
			{Name: "search", Values: []string{"powertube"}},
			{Name: "p", Values: []string{"1"}},
			{Name: "n", Values: []string{"48"}},
		}),
		conformanceFixture(t, "llms.txt", bikeDiscountBaseURL+"/en/llms.txt", nil),
		conformanceFixture(t, "listing_page_1.html", bikeDiscountBaseURL+"/en/mountain-bike-parts", listingQuery()),
		conformanceFixture(t, "brands.html", bikeDiscountBaseURL+"/en/brands", nil),
		conformanceFixture(t, "listing_page_1.html", bikeDiscountBaseURL+"/en/shimano", listingQuery()),
		conformanceFixture(t, "deals.html", bikeDiscountBaseURL+"/en/bike/sale", listingQuery()),
		conformanceFixture(t, "item.html", bikeDiscountBaseURL+"/en/yamaha-500-wh-36v/13.6ah-frame-battery", nil),
		conformanceFixture(t, "item_variants.html", bikeDiscountBaseURL+"/en/fixture-variant-product", nil),
	)
	request := provider.Request{
		Market:      bikeDiscountMarket(),
		Cache:       provider.CachePolicy{Refresh: true, StaleIfError: true},
		Interactive: true,
		Resources:   resources,
	}

	conformance.Run(t, conformance.Suite{
		Registration: registration(),
		Resources:    resources,
		Cases: []conformance.OperationCase{
			{
				Name: "current product search", Capability: provider.CapabilitySearch,
				Invoke: func(ctx context.Context, implementation provider.Provider) (any, error) {
					return implementation.Search(ctx, provider.SearchRequest{Request: request, Query: "powertube"})
				},
				Check: checkProductPage("Cover Cap PowerTube For Charging Socket"),
			},
			{
				Name: "llms root categories", Capability: provider.CapabilityCategories,
				Invoke: func(ctx context.Context, implementation provider.Provider) (any, error) {
					return implementation.Categories(ctx, provider.CategoryListRequest{Request: request})
				},
				Check: func(value any) error {
					page := value.(provider.CategoryPage)
					if len(page.Items) != 7 || page.Items[1].Name != "Bike" {
						return fmt.Errorf("root categories = %#v", page.Items)
					}
					return nil
				},
			},
			{
				Name: "category product listing", Capability: provider.CapabilityCategoryItems,
				Invoke: func(ctx context.Context, implementation provider.Provider) (any, error) {
					return implementation.CategoryItems(ctx, provider.CategoryItemsRequest{
						Request: request, CategoryID: "/en/mountain-bike-parts",
					})
				},
				Check: checkProductPage("Yamaha 500 Wh 36V/13.6Ah Frame Battery"),
			},
			{
				Name: "alphabetical brand index", Capability: provider.CapabilityBrands,
				Invoke: func(ctx context.Context, implementation provider.Provider) (any, error) {
					return implementation.Brands(ctx, provider.BrandListRequest{Request: request})
				},
				Check: func(value any) error {
					page := value.(provider.BrandPage)
					if len(page.Items) != 1 || page.Items[0].ID != "shimano" {
						return fmt.Errorf("brands = %#v", page.Items)
					}
					return nil
				},
			},
			{
				Name: "brand product listing", Capability: provider.CapabilityBrandItems,
				Invoke: func(ctx context.Context, implementation provider.Provider) (any, error) {
					return implementation.BrandItems(ctx, provider.BrandItemsRequest{Request: request, BrandID: "shimano"})
				},
				Check: checkProductPage("Yamaha 500 Wh 36V/13.6Ah Frame Battery"),
			},
			{
				Name: "provider shown deals", Capability: provider.CapabilityDeals,
				Invoke: func(ctx context.Context, implementation provider.Provider) (any, error) {
					return implementation.Deals(ctx, provider.DealsRequest{Request: request})
				},
				Check: func(value any) error {
					page := value.(provider.DealPage)
					if len(page.Items) != 1 || page.Items[0].Product.OriginalPrice == nil {
						return fmt.Errorf("deals = %#v", page.Items)
					}
					return nil
				},
			},
			{
				Name: "listing filter definitions", Capability: provider.CapabilityFilters,
				Invoke: func(ctx context.Context, implementation provider.Provider) (any, error) {
					return implementation.Filters(ctx, provider.FiltersRequest{
						Request: request, Capability: provider.CapabilityDeals,
					})
				},
				Check: func(value any) error {
					result := value.(provider.FiltersResult)
					if len(result.Filters) != 2 || len(result.SortModes) != 1 {
						return fmt.Errorf("filters = %#v, sorts = %#v", result.Filters, result.SortModes)
					}
					return nil
				},
			},
			{
				Name: "item by provider URL", Capability: provider.CapabilityItem,
				Invoke: func(ctx context.Context, implementation provider.Provider) (any, error) {
					return implementation.Item(ctx, provider.ItemRequest{
						Request: request, IDOrURL: bikeDiscountBaseURL + "/en/yamaha-500-wh-36v/13.6ah-frame-battery",
					})
				},
				Check: func(value any) error {
					result := value.(provider.ItemResult)
					if result.Item.ID != "20166382" || result.Item.Price == nil || result.Item.Price.Amount != "299.99" {
						return fmt.Errorf("item = %#v", result.Item)
					}
					return nil
				},
			},
			{
				Name: "visible variant selection", Capability: provider.CapabilityVariantSelection,
				Invoke: func(ctx context.Context, implementation provider.Provider) (any, error) {
					return implementation.Item(ctx, provider.ItemRequest{
						Request: request, IDOrURL: bikeDiscountBaseURL + "/en/fixture-variant-product",
						Variants: []provider.VariantSelection{{Key: "Size", Value: "M"}},
					})
				},
				Check: func(value any) error {
					result := value.(provider.ItemResult)
					if result.Item.SelectedVariant == nil || result.Item.SelectedVariant.Attributes[0].Value != "M" {
						return fmt.Errorf("selected variant = %#v", result.Item.SelectedVariant)
					}
					return nil
				},
			},
		},
	})
}

func conformanceFixture(t *testing.T, name, wantURL string, wantQuery []provider.RequestValue) conformance.ResourceFixture {
	t.Helper()
	return conformance.ResourceFixture{
		Response: provider.ResourceResponse{Body: readCategoryFixture(t, name), StatusCode: http.StatusOK},
		CheckRequest: func(request provider.ResourceRequest) error {
			if request.Method != http.MethodGet || request.URL != wantURL {
				return fmt.Errorf("resource = %s %s, want GET %s", request.Method, request.URL, wantURL)
			}
			if len(request.Query) != len(wantQuery) || len(wantQuery) > 0 && !reflect.DeepEqual(request.Query, wantQuery) {
				return fmt.Errorf("query = %#v, want %#v", request.Query, wantQuery)
			}
			if request.Market != bikeDiscountMarket() || !request.Cache.Refresh || !request.Cache.StaleIfError || !request.Interactive {
				return fmt.Errorf("common request policy was not propagated: %#v", request)
			}
			if !reflect.DeepEqual(request.Transport.Preferred, bikeDiscountTransportOrder) {
				return fmt.Errorf("transport order = %v, want %v", request.Transport.Preferred, bikeDiscountTransportOrder)
			}
			return nil
		},
	}
}

func listingQuery() []provider.RequestValue {
	return []provider.RequestValue{
		{Name: "p", Values: []string{"1"}},
		{Name: "n", Values: []string{"48"}},
	}
}

func checkProductPage(wantName string) func(any) error {
	return func(value any) error {
		page := value.(provider.ProductPage)
		if len(page.Items) == 0 || page.Items[0].Name != wantName {
			return fmt.Errorf("products = %#v", page.Items)
		}
		return nil
	}
}
