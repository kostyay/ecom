package shoputil

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/provider/conformance"
)

func TestValidateSearch(t *testing.T) {
	request := func() provider.SearchRequest {
		return provider.SearchRequest{Query: "bike", Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"}, Resources: conformance.NewFixtureService()}
	}
	for _, test := range []struct {
		name       string
		page, size int
		currency   string
		want       int
	}{
		{name: "defaults", currency: "EUR", want: 1},
		{name: "explicit page", page: 2, size: 24, currency: "EUR", want: 2},
		{name: "last supported page", page: 1000, currency: "EUR", want: 1000},
		{name: "other requested currency", currency: "USD", want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := request()
			r.Page.Number, r.Page.Size, r.Market.Currency = test.page, test.size, test.currency
			page, err := ValidateSearch(t.Context(), r, "DE", 24)
			if err != nil || page != test.want {
				t.Fatalf("page=%d err=%v", page, err)
			}
		})
	}
	for _, test := range []struct {
		name   string
		mutate func(*provider.SearchRequest)
		code   provider.ErrorCode
	}{
		{"empty", func(r *provider.SearchRequest) { r.Query = " " }, provider.ErrorCodeInvalidFilter},
		{"invalid UTF8", func(r *provider.SearchRequest) { r.Query = "\xff" }, provider.ErrorCodeInvalidFilter},
		{"negative page", func(r *provider.SearchRequest) { r.Page.Number = -1 }, provider.ErrorCodeInvalidFilter},
		{"deep page", func(r *provider.SearchRequest) { r.Page.Number = 1001 }, provider.ErrorCodeInvalidFilter},
		{"size", func(r *provider.SearchRequest) { r.Page.Size = 11 }, provider.ErrorCodeInvalidFilter},
		{"negative size", func(r *provider.SearchRequest) { r.Page.Size = -1 }, provider.ErrorCodeInvalidFilter},
		{"filter", func(r *provider.SearchRequest) { r.Filters = []provider.Filter{{Key: "size", Value: "M"}} }, provider.ErrorCodeInvalidFilter},
		{"sort", func(r *provider.SearchRequest) { r.Sort = &provider.Sort{} }, provider.ErrorCodeInvalidFilter},
		{"shipping", func(r *provider.SearchRequest) { r.Pricing.IncludeShipping = true }, provider.ErrorCodeInvalidProviderConfig},
		{"country", func(r *provider.SearchRequest) { r.Market.Country = "AD" }, provider.ErrorCodeInvalidResourceRequest},
		{"language", func(r *provider.SearchRequest) { r.Market.Language = "de" }, provider.ErrorCodeInvalidResourceRequest},
		{"malformed currency", func(r *provider.SearchRequest) { r.Market.Currency = "eur" }, provider.ErrorCodeInvalidResourceRequest},
		{"resources", func(r *provider.SearchRequest) { r.Resources = nil }, provider.ErrorCodeInvalidResourceRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := request()
			test.mutate(&r)
			if _, err := ValidateSearch(t.Context(), r, "DE", 24); !errors.Is(err, test.code) {
				t.Fatalf("error=%v, want %v", err, test.code)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	if err := ValidateConfig(nil); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfig(map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfig(map[string]any{"unknown": true}); !errors.Is(err, provider.ErrorCodeInvalidProviderConfig) {
		t.Fatal(err)
	}
}

func TestFetchFailures(t *testing.T) {
	const endpoint = "https://shop.example/en/search/"
	for _, test := range []struct {
		name      string
		response  provider.ResourceResponse
		err, want error
	}{
		{name: "blocked", response: provider.ResourceResponse{StatusCode: 403}, want: provider.ErrorCodeAccessBlocked},
		{name: "unauthorized", response: provider.ResourceResponse{StatusCode: 401}, want: provider.ErrorCodeAccessBlocked},
		{name: "server", response: provider.ResourceResponse{StatusCode: 503}, want: provider.ErrorCodeHTTPFailure},
		{name: "foreign redirect", response: provider.ResourceResponse{StatusCode: 200, FinalURL: "https://evil.example/en/search/"}, want: provider.ErrorCodeInvalidProviderResult},
		{name: "market redirect", response: provider.ResourceResponse{StatusCode: 200, FinalURL: "https://shop.example/de/search/"}, want: provider.ErrorCodeInvalidProviderResult},
		{name: "credentials", response: provider.ResourceResponse{StatusCode: 200, FinalURL: "https://secret@shop.example/en/search/"}, want: provider.ErrorCodeInvalidProviderResult},
		{name: "scheme", response: provider.ResourceResponse{StatusCode: 200, FinalURL: "http://shop.example/en/search/"}, want: provider.ErrorCodeInvalidProviderResult},
		{name: "malformed URL", response: provider.ResourceResponse{StatusCode: 200, FinalURL: ":bad"}, want: provider.ErrorCodeInvalidProviderResult},
		{name: "raw error", err: errors.New("secret"), want: provider.ErrorCodeHTTPFailure},
		{name: "coded error", err: provider.NewError(provider.ErrorCodeResponseTooLarge, "too large", nil), want: provider.ErrorCodeResponseTooLarge},
		{name: "canceled", err: context.Canceled, want: context.Canceled},
		{name: "deadline", err: context.DeadlineExceeded, want: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			resources := conformance.NewFixtureService(conformance.ResourceFixture{Response: test.response, Err: test.err})
			request := provider.Request{Resources: resources, Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"}}
			_, err := Fetch(t.Context(), request, provider.ResourceRequest{Method: http.MethodGet, URL: endpoint, Transport: provider.TransportPolicy{Required: provider.TransportHTTP}})
			if !errors.Is(err, test.want) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error=%v, want %v", err, test.want)
			}
		})
	}
}
