package bikediscount

import (
	"errors"
	"reflect"
	"testing"

	"github.com/kostyay/ecom/provider"
)

func TestDealsUsesStableSalePageWithListingArguments(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{
		Body: readCategoryFixture(t, "listing_page_1.html"),
	}}}
	page, err := (implementation{}).Deals(t.Context(), provider.DealsRequest{
		Request: bikeDiscountRequest(service),
		Page:    provider.PageRequest{Number: 2, Size: 48},
		Sort:    &provider.Sort{Value: "standard"},
		Filters: []provider.Filter{
			{Key: "manufacturer", Value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			{Key: "properties", Value: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			{Key: "properties", Value: "cccccccccccccccccccccccccccccccc"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("deals = %#v, want one explicit RRP deal", page.Items)
	}
	product := page.Items[0].Product
	if product.Price == nil || product.Price.Amount != "299.99" || product.OriginalPrice == nil || product.OriginalPrice.Amount != "819.00" {
		t.Errorf("deal prices = current %#v, original %#v", product.Price, product.OriginalPrice)
	}
	if product.DiscountAmount != nil || product.DiscountPercent != nil {
		t.Errorf("unshown deal values were estimated: amount %#v, percent %#v", product.DiscountAmount, product.DiscountPercent)
	}
	if err := page.Items[0].Validate(); err != nil {
		t.Errorf("Deal.Validate() error = %v", err)
	}
	if page.Page.Number != 2 || page.Page.Size != 48 || page.Page.HasNext == nil || !*page.Page.HasNext {
		t.Errorf("deal page = %#v", page.Page)
	}
	wantQuery := []provider.RequestValue{
		{Name: "p", Values: []string{"2"}}, {Name: "n", Values: []string{"48"}},
		{Name: "order", Values: []string{"standard"}},
		{Name: "manufacturer", Values: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		{Name: "properties", Values: []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb|cccccccccccccccccccccccccccccccc"}},
	}
	if len(service.requests) != 1 || service.requests[0].URL != bikeDiscountBaseURL+"/en/bike/sale" || !reflect.DeepEqual(service.requests[0].Query, wantQuery) {
		t.Errorf("deal resource request = %#v", service.requests)
	}
}

func TestDealsKeepsOnlyExplicitReductions(t *testing.T) {
	document := []byte(`<main><section aria-label="Products">
		<article><a href="/en/rrp">RRP deal</a><span aria-label="Recommended retail price">100,00 €</span><strong>80,00 €</strong></article>
		<article><a href="/en/shown">Shown amount and percent</a><p aria-label="You save">20,00 €</p><strong>80,00 €</strong><span>-20%</span></article>
		<article><a href="/en/label-only">Sale label only</a><strong>50,00 €</strong><span>Sale</span></article>
		<article><a href="/en/specification">Efficiency specification</a><strong>70,00 €</strong><span>Efficiency 95%</span></article>
		<article><a href="/en/normal">Normal product</a><strong>60,00 €</strong></article>
	</section></main>`)
	service := &fakeResourceService{responses: []provider.ResourceResponse{{Body: document}}}
	page, err := (implementation{}).Deals(t.Context(), provider.DealsRequest{Request: bikeDiscountRequest(service)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Product.Name != "RRP deal" || page.Items[1].Product.Name != "Shown amount and percent" {
		t.Fatalf("explicit deals = %#v", page.Items)
	}
	shown := page.Items[1].Product
	if shown.OriginalPrice != nil || shown.DiscountAmount == nil || shown.DiscountAmount.Amount != "20.00" || shown.DiscountPercent == nil || *shown.DiscountPercent != 20 {
		t.Errorf("shown reductions = original %#v, amount %#v, percent %#v", shown.OriginalPrice, shown.DiscountAmount, shown.DiscountPercent)
	}
	for _, deal := range page.Items {
		if err := deal.Validate(); err != nil {
			t.Errorf("Deal.Validate() error = %v", err)
		}
	}
}

func TestDealsReturnsValidEmptyPageWhenNoReductionIsShown(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{
		Body: readCategoryFixture(t, "listing_page_2.html"),
	}}}
	page, err := (implementation{}).Deals(t.Context(), provider.DealsRequest{Request: bikeDiscountRequest(service)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("deals = %#v, want empty page", page.Items)
	}
	if page.Page.Number != 1 || page.Page.Size != 48 || page.Page.HasNext == nil || *page.Page.HasNext {
		t.Errorf("empty deal page = %#v", page.Page)
	}
}

func TestDealsRejectsUnsupportedListingArgumentsBeforeFetch(t *testing.T) {
	service := &fakeResourceService{}
	requests := []provider.DealsRequest{
		{Page: provider.PageRequest{Number: 1, Size: 24}},
		{Sort: &provider.Sort{Value: "price-low"}},
		{Filters: []provider.Filter{{Key: "manufacturer", Value: "not-an-id"}}},
		{Filters: []provider.Filter{{Key: "unknown", Value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}},
	}
	for _, request := range requests {
		request.Request = bikeDiscountRequest(service)
		if _, err := (implementation{}).Deals(t.Context(), request); !errors.Is(err, provider.ErrorCodeInvalidFilter) {
			t.Errorf("Deals(%#v) error = %v", request, err)
		}
	}
	if len(service.requests) != 0 {
		t.Fatalf("invalid deal requests reached resource service: %d", len(service.requests))
	}
}
