package bikediscount

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/kostyay/ecom/provider"
)

func TestItemByOwnedURLParsesFullDetails(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{Body: readCategoryFixture(t, "item.html")}}}
	result, err := (implementation{}).Item(t.Context(), provider.ItemRequest{
		Request: bikeDiscountRequest(service),
		IDOrURL: bikeDiscountBaseURL + "/en/yamaha-500-wh-36v/13.6ah-frame-battery",
	})
	if err != nil {
		t.Fatal(err)
	}
	item := result.Item
	if item.ID != "20166382" || item.Name != "Yamaha 500 Wh 36V/13.6Ah Frame Battery" ||
		item.URL != bikeDiscountBaseURL+"/en/yamaha-500-wh-36v/13.6ah-frame-battery" || item.DetailLevel != provider.DetailLevelFull {
		t.Fatalf("item identity = %#v", item.ProductSummary)
	}
	if item.Price == nil || item.Price.Amount != "299.99" || item.Price.Display != "299,99 €" ||
		item.OriginalPrice == nil || item.OriginalPrice.Amount != "819.00" {
		t.Errorf("item prices = %#v, %#v", item.Price, item.OriginalPrice)
	}
	if item.Availability != provider.AvailabilityInStock || item.StockText != "In stock, delivery time 1-3 Days" {
		t.Errorf("item stock = %q, %q", item.Availability, item.StockText)
	}
	if item.Description != "Sanitized product description." {
		t.Errorf("description = %q", item.Description)
	}
	wantAttributes := []provider.Attribute{{Name: "Manufacturer number", Value: "fixture-manufacturer-number"}, {Name: "EAN", Value: "0000000000000"}}
	if !reflect.DeepEqual(item.Attributes, wantAttributes) {
		t.Errorf("attributes = %#v, want %#v", item.Attributes, wantAttributes)
	}
	if len(service.requests) != 1 || service.requests[0].URL != item.URL {
		t.Errorf("resource requests = %#v", service.requests)
	}
	if err := item.Validate(); err != nil {
		t.Errorf("ItemDetail.Validate() error = %v", err)
	}
}

func TestItemNumberResolvesThroughSearchBeforeItemPage(t *testing.T) {
	search := []byte(`<html><body><article data-product-number="20166382"><a href="/en/yamaha-500-wh-36v/13.6ah-frame-battery">Battery</a><strong>299,99 €</strong></article></body></html>`)
	service := &fakeResourceService{responses: []provider.ResourceResponse{{Body: search}, {Body: readCategoryFixture(t, "item.html")}}}
	result, err := (implementation{}).Item(t.Context(), provider.ItemRequest{
		Request: bikeDiscountRequest(service), IDOrURL: "20166382",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Item.ID != "20166382" || len(service.requests) != 2 {
		t.Fatalf("result = %#v; requests = %#v", result, service.requests)
	}
	if service.requests[0].URL != bikeDiscountBaseURL+"/en/search" || service.requests[0].Query[0].Name != "search" || service.requests[0].Query[0].Values[0] != "20166382" {
		t.Errorf("numeric resolution request = %#v", service.requests[0])
	}
	if service.requests[1].URL != bikeDiscountBaseURL+"/en/yamaha-500-wh-36v/13.6ah-frame-battery" {
		t.Errorf("item request URL = %q", service.requests[1].URL)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("resolution warnings = %#v", result.Warnings)
	}
}

func TestItemNumberRequiresExactSearchMatch(t *testing.T) {
	search := []byte(`<html><body><article data-product-number="20166383"><a href="/en/other">Other</a><strong>1,00 €</strong></article></body></html>`)
	service := &fakeResourceService{responses: []provider.ResourceResponse{{Body: search}}}
	_, err := (implementation{}).Item(t.Context(), provider.ItemRequest{Request: bikeDiscountRequest(service), IDOrURL: "20166382"})
	if !errors.Is(err, provider.ErrorCodeInvalidFilter) || len(service.requests) != 1 {
		t.Fatalf("Item() error = %v; requests = %d", err, len(service.requests))
	}
}

func TestExtractItemDetailParsesVisibleVariants(t *testing.T) {
	item, err := ExtractItemDetail(readCategoryFixture(t, "item_variants.html"), bikeDiscountBaseURL+"/en/fixture-variant-product")
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Variants) != 3 || item.Variants[0].Attributes[0] != (provider.Attribute{Name: "Size", Value: "S"}) ||
		!item.Variants[1].Selected || item.Variants[2].Availability != provider.AvailabilityOutOfStock || item.Variants[2].StockText != "Currently unavailable" {
		t.Fatalf("variants = %#v", item.Variants)
	}
	if item.Price == nil || item.Price.Display != "from 59,99 €" || item.PriceRange != nil {
		t.Errorf("from price = %#v; range = %#v", item.Price, item.PriceRange)
	}
	for _, variant := range item.Variants {
		if variant.ID != "" || len(variant.ProviderData) != 0 {
			t.Errorf("unverified internal identifier leaked into variant: %#v", variant)
		}
	}
}

func TestExtractItemDetailPreservesEqualAndDifferentVariantPrices(t *testing.T) {
	tests := []struct {
		name      string
		prices    []string
		wantRange [2]string
	}{
		{name: "equal", prices: []string{"59,99 €", "59,99 €"}, wantRange: [2]string{"59.99", "59.99"}},
		{name: "different", prices: []string{"69,99 €", "59,99 €"}, wantRange: [2]string{"59.99", "69.99"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := `<html><head><link rel="canonical" href="https://www.bike-discount.de/en/priced"></head><body><article><h1>Priced</h1><p>Item no: 123</p><strong>from 59,99 €</strong><fieldset><legend>Size</legend>` +
				`<label><input type="radio"> S <strong>` + test.prices[0] + `</strong></label>` +
				`<label><input type="radio"> M <strong>` + test.prices[1] + `</strong></label></fieldset></article></body></html>`
			item, err := ExtractItemDetail([]byte(document), bikeDiscountBaseURL+"/en/priced")
			if err != nil {
				t.Fatal(err)
			}
			if item.Variants[0].Price == nil || item.Variants[0].Price.Display != test.prices[0] || item.Variants[1].Price == nil || item.Variants[1].Price.Display != test.prices[1] {
				t.Fatalf("variant prices = %#v", item.Variants)
			}
			if item.PriceRange == nil || item.PriceRange.Minimum.Amount != test.wantRange[0] || item.PriceRange.Maximum.Amount != test.wantRange[1] {
				t.Errorf("price range = %#v, want %v", item.PriceRange, test.wantRange)
			}
		})
	}
}

func TestItemSelectsExactVisibleVariant(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{Body: readCategoryFixture(t, "item_variants.html")}}}
	request := provider.ItemRequest{
		Request: bikeDiscountRequest(service), IDOrURL: bikeDiscountBaseURL + "/en/fixture-variant-product",
		Variants: []provider.VariantSelection{{Key: "Size", Value: "M"}},
	}
	result, err := (implementation{}).Item(t.Context(), request)
	if err != nil || result.Item.SelectedVariant == nil || result.Item.SelectedVariant.Attributes[0].Value != "M" {
		t.Fatalf("Item() = %#v, %v", result, err)
	}

	service = &fakeResourceService{responses: []provider.ResourceResponse{{Body: readCategoryFixture(t, "item_variants.html")}}}
	request.Request = bikeDiscountRequest(service)
	request.Variants[0].Value = "XL"
	_, err = (implementation{}).Item(t.Context(), request)
	if !errors.Is(err, provider.ErrorCodeVariantNotFound) || !strings.Contains(err.Error(), "Size=L, Size=M, Size=S") {
		t.Fatalf("invalid selection error = %v", err)
	}
}

func TestItemRejectsInvalidIdentifiersBeforeNetwork(t *testing.T) {
	tests := []string{
		"", "opaque-id", "http://www.bike-discount.de/en/item", "https://bike-discount.de/en/item",
		"https://shop.example/en/item", "https://www.bike-discount.de/de/item", "https://www.bike-discount.de/%65n/item#fragment",
	}
	for _, identifier := range tests {
		t.Run(identifier, func(t *testing.T) {
			service := &fakeResourceService{}
			_, err := (implementation{}).Item(t.Context(), provider.ItemRequest{Request: bikeDiscountRequest(service), IDOrURL: identifier})
			if !errors.Is(err, provider.ErrorCodeInvalidFilter) || len(service.requests) != 0 {
				t.Fatalf("Item(%q) error = %v; requests = %d", identifier, err, len(service.requests))
			}
		})
	}
}

func TestExtractItemDetailRejectsMalformedPartials(t *testing.T) {
	tests := [][]byte{
		[]byte(`<html><body><p>no article</p></body></html>`),
		[]byte(`<html><body><article><h1>Name</h1><p>Item no: 1</p></article></body></html>`),
		[]byte(`<html><head><link rel="canonical" href="https://other.example/en/item"></head><body><article><h1>Name</h1><p>Item no: 1</p></article></body></html>`),
		[]byte(`<html><head><link rel="canonical" href="https://www.bike-discount.de/en/item"></head><body><article><h1>Name</h1></article></body></html>`),
	}
	for index, document := range tests {
		if _, err := ExtractItemDetail(document, bikeDiscountBaseURL+"/en/item"); err == nil {
			t.Errorf("malformed partial %d did not fail", index)
		}
	}
}
