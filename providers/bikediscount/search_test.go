package bikediscount

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/kostyay/ecom/provider"
)

func TestSearchUsesCurrentRequestAndListingParser(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{
		Body:     readCategoryFixture(t, "search_current.html"),
		FinalURL: bikeDiscountBaseURL + "/en/search?search=powertube&p=2&n=48",
	}}}
	page, err := (implementation{}).Search(t.Context(), provider.SearchRequest{
		Request: bikeDiscountRequest(service), Query: "  powertube  ",
		Page: provider.PageRequest{Number: 2, Size: 48}, Sort: &provider.Sort{Value: "standard"},
		Filters: []provider.Filter{
			{Key: "manufacturer", Value: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
			{Key: "properties", Value: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			{Key: "properties", Value: "cccccccccccccccccccccccccccccccc"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Name != "Cover Cap PowerTube For Charging Socket" || page.Items[0].ID != "20088648" ||
		page.Items[0].Brand != "Bosch" || page.Items[0].Price == nil || page.Items[0].Price.Amount != "1.99" ||
		page.Items[1].OriginalPrice == nil || page.Items[1].OriginalPrice.Amount != "899.00" {
		t.Fatalf("search products = %#v", page.Items)
	}
	if page.Page.Number != 2 || page.Page.Size != 48 || page.Page.HasNext == nil || !*page.Page.HasNext {
		t.Errorf("page info = %#v", page.Page)
	}
	if len(page.Warnings) != 0 {
		t.Fatalf("search warnings = %#v", page.Warnings)
	}
	wantQuery := []provider.RequestValue{
		{Name: "search", Values: []string{"powertube"}},
		{Name: "p", Values: []string{"2"}},
		{Name: "n", Values: []string{"48"}},
		{Name: "order", Values: []string{"standard"}},
		{Name: "manufacturer", Values: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		{Name: "properties", Values: []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb|cccccccccccccccccccccccccccccccc"}},
	}
	if len(service.requests) != 1 || service.requests[0].URL != bikeDiscountBaseURL+"/en/search" || !reflect.DeepEqual(service.requests[0].Query, wantQuery) {
		t.Errorf("resource request = %#v", service.requests)
	}
}

func TestSearchUsesVerifiedPagingDefaultsAndCurrencyWarning(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{Body: readCategoryFixture(t, "search_current.html")}}}
	request := bikeDiscountRequest(service)
	request.Market.Currency = "USD"
	page, err := (implementation{}).Search(t.Context(), provider.SearchRequest{Request: request, Query: "battery"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Page.Number != 1 || page.Page.Size != 48 {
		t.Errorf("default page = %#v", page.Page)
	}
	wantQuery := []provider.RequestValue{
		{Name: "search", Values: []string{"battery"}},
		{Name: "p", Values: []string{"1"}},
		{Name: "n", Values: []string{"48"}},
	}
	if !reflect.DeepEqual(service.requests[0].Query, wantQuery) {
		t.Errorf("default query = %#v, want %#v", service.requests[0].Query, wantQuery)
	}
	if len(page.Warnings) != 1 || page.Warnings[0].Code != provider.WarningCodeCurrencyUnavailable ||
		page.Warnings[0].RequestedCurrency != "USD" || page.Warnings[0].ActualCurrency != "EUR" {
		t.Fatalf("warnings = %#v", page.Warnings)
	}
}

func TestSearchRejectsInvalidInputsBeforeNetwork(t *testing.T) {
	tests := []struct {
		name   string
		change func(*provider.SearchRequest)
	}{
		{name: "empty query", change: func(request *provider.SearchRequest) { request.Query = " \t " }},
		{name: "page below one", change: func(request *provider.SearchRequest) { request.Page.Number = -1 }},
		{name: "unsupported page size", change: func(request *provider.SearchRequest) { request.Page.Size = 24 }},
		{name: "unverified sort", change: func(request *provider.SearchRequest) { request.Sort = &provider.Sort{Value: "price-low"} }},
		{name: "unknown filter", change: func(request *provider.SearchRequest) {
			request.Filters = []provider.Filter{{Key: "available", Value: "true"}}
		}},
		{name: "invalid manufacturer", change: func(request *provider.SearchRequest) {
			request.Filters = []provider.Filter{{Key: "manufacturer", Value: "shimano"}}
		}},
		{name: "repeated manufacturer", change: func(request *provider.SearchRequest) {
			request.Filters = []provider.Filter{
				{Key: "manufacturer", Value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				{Key: "manufacturer", Value: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeResourceService{}
			request := provider.SearchRequest{Request: bikeDiscountRequest(service), Query: "bike"}
			test.change(&request)
			_, err := (implementation{}).Search(context.Background(), request)
			if !errors.Is(err, provider.ErrorCodeInvalidFilter) {
				t.Fatalf("Search() error = %v", err)
			}
			if len(service.requests) != 0 {
				t.Fatalf("invalid search reached the resource service: %#v", service.requests)
			}
		})
	}
}
