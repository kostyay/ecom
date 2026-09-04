package bikediscount

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/kostyay/ecom/provider"
)

type fakeResourceService struct {
	requests  []provider.ResourceRequest
	responses []provider.ResourceResponse
	errors    []error
}

type fakeFallbackCore struct {
	failures map[provider.TransportMode]provider.ErrorCode
	attempts []provider.TransportMode
}

func (core *fakeFallbackCore) Fetch(_ context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
	for _, mode := range request.Transport.Preferred {
		core.attempts = append(core.attempts, mode)
		if code, fails := core.failures[mode]; fails {
			if code == provider.ErrorCodeBrowserChallengeRequired {
				return provider.ResourceResponse{Transport: mode}, provider.NewError(code, "manual action is required", nil)
			}
			continue
		}
		response := provider.ResourceResponse{Transport: mode, Body: []byte("direct")}
		if mode != provider.TransportHTTP {
			response.Body = nil
			response.Page = &provider.PageSnapshot{HTML: []byte("rendered")}
		}
		return response, nil
	}
	return provider.ResourceResponse{}, provider.NewError(provider.ErrorCodeTransportUnavailable, "no transport succeeded", nil)
}

func (service *fakeResourceService) Fetch(_ context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
	service.requests = append(service.requests, request)
	index := len(service.requests) - 1
	var response provider.ResourceResponse
	if index < len(service.responses) {
		response = service.responses[index]
	}
	if index < len(service.errors) {
		return response, service.errors[index]
	}
	return response, nil
}

func TestFetchResourcePassesMarketPolicyAndTransportOrder(t *testing.T) {
	service := &fakeResourceService{responses: []provider.ResourceResponse{{Body: []byte("page"), Transport: provider.TransportHTTP}}}
	market := provider.Market{Country: "CA", Language: "en", Currency: "CAD"}
	common := provider.Request{
		Market: market, Cache: provider.CachePolicy{Refresh: true, StaleIfError: true},
		Interactive: true, Resources: service,
	}
	target := resourceTarget{
		Path:  "/search",
		Query: []provider.RequestValue{{Name: "search", Values: []string{"vélo"}}},
		DOM:   []provider.DOMExtraction{{Name: "products", Selector: "article", Kind: provider.DOMHTML, All: true}},
	}

	response, err := fetchResource(t.Context(), common, target)
	if err != nil || string(responseDocument(response)) != "page" || len(service.requests) != 1 {
		t.Fatalf("fetchResource() = %#v, %v; requests = %d", response, err, len(service.requests))
	}
	request := service.requests[0]
	if request.Method != "GET" || request.URL != "https://www.bike-discount.de/en/search" || request.Market != market || request.Cache != common.Cache || !request.Interactive {
		t.Fatalf("resource request = %#v", request)
	}
	if !reflect.DeepEqual(request.Transport.Preferred, bikeDiscountTransportOrder) || !reflect.DeepEqual(request.Query, target.Query) || !reflect.DeepEqual(request.DOM, target.DOM) {
		t.Fatalf("resource policy = %#v", request)
	}

	// The provider request contains the full market. The Core uses these values
	// as part of its cache and browser-session scope.
	second := common
	second.Market.Country = "DE"
	if _, err := fetchResource(t.Context(), second, target); err != nil {
		t.Fatal(err)
	}
	if service.requests[0].Market == service.requests[1].Market {
		t.Fatal("different markets used the same resource scope")
	}
}

func TestFetchResourceAcceptsHTTPBrowserAndCDPResponses(t *testing.T) {
	tests := []struct {
		name     string
		failures map[provider.TransportMode]provider.ErrorCode
		wantMode provider.TransportMode
		attempts []provider.TransportMode
	}{
		{name: "HTTP", wantMode: provider.TransportHTTP, attempts: []provider.TransportMode{provider.TransportHTTP}},
		{name: "browser fallback", failures: map[provider.TransportMode]provider.ErrorCode{provider.TransportHTTP: provider.ErrorCodeAccessBlocked}, wantMode: provider.TransportBrowser, attempts: []provider.TransportMode{provider.TransportHTTP, provider.TransportBrowser}},
		{name: "CDP fallback", failures: map[provider.TransportMode]provider.ErrorCode{provider.TransportHTTP: provider.ErrorCodeAccessBlocked, provider.TransportBrowser: provider.ErrorCodeBrowserFailure}, wantMode: provider.TransportCDP, attempts: []provider.TransportMode{provider.TransportHTTP, provider.TransportBrowser, provider.TransportCDP}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			core := &fakeFallbackCore{failures: test.failures}
			got, err := fetchResource(t.Context(), bikeDiscountRequest(core), resourceTarget{Path: "/bike/sale"})
			if err != nil || got.Transport != test.wantMode || len(responseDocument(got)) == 0 {
				t.Fatalf("fetchResource() = %#v, %v", got, err)
			}
			if !reflect.DeepEqual(core.attempts, test.attempts) {
				t.Fatalf("transport attempts = %v, want %v", core.attempts, test.attempts)
			}
		})
	}
}

func TestFetchResourceReturnsChallengeFromFallbackCore(t *testing.T) {
	core := &fakeFallbackCore{failures: map[provider.TransportMode]provider.ErrorCode{
		provider.TransportHTTP:    provider.ErrorCodeAccessBlocked,
		provider.TransportBrowser: provider.ErrorCodeBrowserChallengeRequired,
	}}
	_, err := fetchResource(t.Context(), bikeDiscountRequest(core), resourceTarget{Path: "/bike/sale"})
	if !errors.Is(err, provider.ErrorCodeBrowserChallengeRequired) {
		t.Fatalf("fetchResource() error = %v", err)
	}
	want := []provider.TransportMode{provider.TransportHTTP, provider.TransportBrowser}
	if !reflect.DeepEqual(core.attempts, want) {
		t.Fatalf("challenge attempts = %v, want %v", core.attempts, want)
	}
}

func TestFetchResourcePreservesStableTransportErrors(t *testing.T) {
	for _, code := range []provider.ErrorCode{
		provider.ErrorCodeAccessBlocked,
		provider.ErrorCodeHTTPFailure,
		provider.ErrorCodeBrowserFailure,
		provider.ErrorCodeBrowserChallengeRequired,
		provider.ErrorCodeBrowserChallengeTimeout,
		provider.ErrorCodeTransportUnavailable,
	} {
		t.Run(string(code), func(t *testing.T) {
			service := &fakeResourceService{errors: []error{provider.NewError(code, "safe failure", errors.New("internal"))}}
			_, err := fetchResource(t.Context(), bikeDiscountRequest(service), resourceTarget{Path: "/bike/sale"})
			if !errors.Is(err, code) {
				t.Fatalf("fetchResource() error = %v, want %s", err, code)
			}
		})
	}
}

func TestFetchResourceValidatesBeforeServiceCall(t *testing.T) {
	service := &fakeResourceService{}
	tests := []struct {
		name    string
		request provider.Request
		target  resourceTarget
	}{
		{name: "missing service", request: provider.Request{Market: bikeDiscountMarket()}, target: resourceTarget{Path: "/search"}},
		{name: "invalid market", request: provider.Request{Market: provider.Market{Country: "DE", Language: "en", Currency: "eur"}, Resources: service}, target: resourceTarget{Path: "/search"}},
		{name: "unknown language", request: provider.Request{Market: provider.Market{Country: "DE", Language: "nl", Currency: "EUR"}, Resources: service}, target: resourceTarget{Path: "/search"}},
		{name: "foreign URL", request: provider.Request{Market: bikeDiscountMarket(), Resources: service}, target: resourceTarget{URL: "https://shop.example/item"}},
		{name: "wrong URL language", request: provider.Request{Market: bikeDiscountMarket(), Resources: service}, target: resourceTarget{URL: bikeDiscountBaseURL + "/de/item"}},
		{name: "mixed target", request: provider.Request{Market: bikeDiscountMarket(), Resources: service}, target: resourceTarget{URL: bikeDiscountBaseURL + "/en/item", Path: "/item"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := fetchResource(t.Context(), test.request, test.target)
			if !errors.Is(err, provider.ErrorCodeInvalidResourceRequest) {
				t.Fatalf("fetchResource() error = %v", err)
			}
		})
	}
	if len(service.requests) != 0 {
		t.Fatalf("invalid requests reached service: %d", len(service.requests))
	}
}

func TestFetchResourceRejectsShippingPolicyBeforeServiceCall(t *testing.T) {
	service := &fakeResourceService{}
	request := bikeDiscountRequest(service)
	request.Pricing.IncludeShipping = true

	_, err := fetchResource(t.Context(), request, resourceTarget{Path: "/search"})
	if !errors.Is(err, provider.ErrorCodeInvalidProviderConfig) {
		t.Fatalf("fetchResource() error = %v, want invalid_provider_config", err)
	}
	if len(service.requests) != 0 {
		t.Fatalf("unsupported price policy reached service: %d requests", len(service.requests))
	}
}

func TestCurrencyWarningsReportActualDisplayedCurrencyWithoutConversion(t *testing.T) {
	products := []provider.ProductSummary{
		{Price: &provider.Money{Amount: "10.00", Currency: "EUR", Display: "10,00 €"}},
		{OriginalPrice: &provider.Money{Amount: "12.00", Currency: "EUR", Display: "12,00 €"}},
		{Variants: []provider.Variant{{Price: &provider.Money{Amount: "9.00", Currency: "EUR", Display: "9,00 €"}}}},
	}
	warnings := currencyWarnings("CAD", products...)
	if len(warnings) != 1 || warnings[0].Code != provider.WarningCodeCurrencyUnavailable || warnings[0].RequestedCurrency != "CAD" || warnings[0].ActualCurrency != "EUR" {
		t.Fatalf("currency warnings = %#v", warnings)
	}
	if got := currencyWarnings("EUR", products...); len(got) != 0 {
		t.Fatalf("matching currency warnings = %#v", got)
	}
}

func TestFetchResourceNormalizesUncodedFailures(t *testing.T) {
	service := &fakeResourceService{
		responses: []provider.ResourceResponse{{Transport: provider.TransportCDP}},
		errors:    []error{errors.New("connector detail")},
	}
	_, err := fetchResource(t.Context(), bikeDiscountRequest(service), resourceTarget{Path: "/search"})
	if !errors.Is(err, provider.ErrorCodeBrowserFailure) || err.Error() != "the Bike-Discount resource request failed" {
		t.Fatalf("fetchResource() error = %v", err)
	}
}

func bikeDiscountRequest(service provider.ResourceService) provider.Request {
	return provider.Request{Market: bikeDiscountMarket(), Resources: service}
}

func bikeDiscountMarket() provider.Market {
	return provider.Market{Country: "DE", Language: "en", Currency: "EUR"}
}

var _ provider.ResourceService = (*fakeResourceService)(nil)
var _ provider.ResourceService = (*fakeFallbackCore)(nil)
