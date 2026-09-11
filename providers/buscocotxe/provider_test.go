package buscocotxe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/provider/conformance"
)

func TestProviderConformance(t *testing.T) {
	var fixtures []conformance.ResourceFixture
	var cases []conformance.OperationCase
	for _, test := range []struct {
		file        string
		page, count int
		partial     bool
		code        provider.ErrorCode
	}{
		{"search_page_1.html", 1, 30, false, ""},
		{"search_page_2.html", 2, 30, false, ""},
		{"search_last.html", 42, 7, false, ""},
		{"search_empty.html", 1, 0, false, ""},
		{"search_empty.html", 2, 0, false, ""},
		{"regressions.html", 1, 3, true, ""},
		{"search_out_of_range.html", 43, 0, false, provider.ErrorCodeInvalidProviderResult},
		{"unexpected.html", 1, 0, false, provider.ErrorCodeInvalidProviderResult},
	} {
		fixtures = append(fixtures, conformance.ResourceFixture{
			Response: provider.ResourceResponse{Body: readFixture(t, test.file), StatusCode: http.StatusOK, FinalURL: searchURL + "?search=BMW&pn=" + strconv.Itoa(test.page)},
			CheckRequest: func(request provider.ResourceRequest) error {
				if request.Method != http.MethodGet || request.URL != searchURL || request.Transport.Required != provider.TransportHTTP {
					return fmt.Errorf("unexpected request: %+v", request)
				}
				want := []provider.RequestValue{{Name: "search", Values: []string{"BMW"}}, {Name: "pn", Values: []string{strconv.Itoa(test.page)}}}
				if !reflect.DeepEqual(request.Query, want) {
					return fmt.Errorf("query=%+v", request.Query)
				}
				return nil
			},
		})
		cases = append(cases, conformance.OperationCase{
			Name: test.file + strconv.Itoa(test.page), Capability: provider.CapabilitySearch, WantPartialWarning: test.partial, WantErrorCode: test.code,
			Check: func(value any) error {
				page := value.(provider.ProductPage)
				if len(page.Items) != test.count || page.Page.Number != test.page {
					return fmt.Errorf("page=%+v", page)
				}
				if test.count == 0 && (page.Page.TotalPages != nil || page.Page.TotalItems == nil || *page.Page.TotalItems != 0 || page.Page.HasNext == nil || *page.Page.HasNext) {
					return fmt.Errorf("empty page=%+v", page.Page)
				}
				return nil
			},
		})
	}
	resources := conformance.NewFixtureService(fixtures...)
	for i := range cases {
		page := fixtures[i].Response.FinalURL
		u, _ := url.Parse(page)
		number, _ := strconv.Atoi(u.Query().Get("pn"))
		cases[i].Invoke = func(ctx context.Context, p provider.Provider) (any, error) {
			return p.Search(ctx, provider.SearchRequest{Market: testMarket(), Resources: resources, Query: "BMW", Page: provider.PageRequest{Number: number}})
		}
	}
	conformance.Run(t, conformance.Suite{Registration: registration(), Resources: resources, Cases: cases})
}

func TestSearchPolicies(t *testing.T) {
	query := "BMW & elèctric + X3?"
	market := provider.Market{Country: "DE", Language: "en", Currency: "USD"}
	cache := provider.CachePolicy{Refresh: true, StaleIfError: true}
	retrieved := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	resources := conformance.NewFixtureService(conformance.ResourceFixture{
		Response: provider.ResourceResponse{Body: readFixture(t, "search_page_1.html"), StatusCode: 200, RetrievedAt: retrieved, FinalURL: searchURL + "?pn=1&search=" + url.QueryEscape(query)},
		CheckRequest: func(request provider.ResourceRequest) error {
			if request.Market != market || request.Cache != cache || !request.Interactive {
				return fmt.Errorf("policies=%+v", request)
			}
			if request.Query[0].Name != "search" || len(request.Query[0].Values) != 1 || request.Query[0].Values[0] != query {
				return fmt.Errorf("query=%+v", request.Query)
			}
			if len(request.Headers) != 0 || len(request.Body.Bytes) != 0 || len(request.Transport.Preferred) != 0 {
				return fmt.Errorf("unexpected options=%+v", request)
			}
			return nil
		},
	})
	page, err := (implementation{}).Search(t.Context(), provider.SearchRequest{Market: market, Cache: cache, Interactive: true, Resources: resources, Query: "  " + query + "  "})
	if err != nil {
		t.Fatal(err)
	}
	if page.Page.Number != 1 || page.Items[0].RetrievedAt != retrieved || page.Items[0].Price.Currency != "EUR" {
		t.Fatalf("page=%+v", page)
	}
	if len(page.Warnings) != 1 || page.Warnings[0].Code != provider.WarningCodeCurrencyUnavailable || page.Warnings[0].RequestedCurrency != "USD" || page.Warnings[0].ActualCurrency != "EUR" {
		t.Fatalf("warnings=%+v", page.Warnings)
	}
}

func TestProviderHelpAndValidation(t *testing.T) {
	p, ok := provider.Lookup(Name)
	if !ok {
		t.Fatal("Provider not registered")
	}
	help, err := p.Help(t.Context(), provider.HelpRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := help.Help.Validate(); err != nil {
		t.Fatal(err)
	}
	again, err := p.Help(t.Context(), provider.HelpRequest{Market: provider.Market{Country: "FR", Language: "fr", Currency: "USD"}})
	if err != nil || !reflect.DeepEqual(help, again) {
		t.Fatalf("Help is not deterministic: %v", err)
	}
	if len(p.Capabilities()) != 1 || !p.Supports(provider.CapabilitySearch) {
		t.Fatalf("capabilities=%v", p.Capabilities())
	}
	for _, config := range []map[string]any{nil, {}} {
		if err := p.ValidateConfig(config); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.ValidateConfig(map[string]any{"secret": "do-not-print"}); !errors.Is(err, provider.ErrorCodeInvalidProviderConfig) || strings.Contains(err.Error(), "do-not-print") {
		t.Fatalf("config error=%v", err)
	}
	for name, mutate := range map[string]func(*provider.SearchRequest){
		"empty":         func(r *provider.SearchRequest) { r.Query = " \n " },
		"page":          func(r *provider.SearchRequest) { r.Page.Number = -1 },
		"size":          func(r *provider.SearchRequest) { r.Page.Size = 20 },
		"negative size": func(r *provider.SearchRequest) { r.Page.Size = -1 },
		"filter":        func(r *provider.SearchRequest) { r.Filters = []provider.Filter{{Key: "price", Value: "1000"}} },
		"sort":          func(r *provider.SearchRequest) { r.Sort = &provider.Sort{} },
		"shipping":      func(r *provider.SearchRequest) { r.Pricing.IncludeShipping = true },
		"market":        func(r *provider.SearchRequest) { r.Market.Currency = "usd" },
		"resources":     func(r *provider.SearchRequest) { r.Resources = nil },
	} {
		t.Run(name, func(t *testing.T) {
			resources := conformance.NewFixtureService()
			request := provider.SearchRequest{Market: testMarket(), Resources: resources, Query: "BMW"}
			mutate(&request)
			_, err := p.Search(t.Context(), request)
			if err == nil {
				t.Fatal("invalid request accepted")
			}
			if requests, _ := resources.Stats(); requests != 0 {
				t.Fatal("invalid request made a resource call")
			}
		})
	}
}

func TestSearchFailures(t *testing.T) {
	for name, test := range map[string]struct {
		status   int
		finalURL string
		err      error
		code     error
	}{
		"forbidden":        {status: 403, code: provider.ErrorCodeAccessBlocked},
		"server":           {status: 503, code: provider.ErrorCodeHTTPFailure},
		"foreign redirect": {status: 200, finalURL: "https://evil.example/ca/search?search=BMW", code: provider.ErrorCodeInvalidProviderResult},
		"login redirect":   {status: 200, finalURL: "https://www.buscocotxe.ad/ca/login", code: provider.ErrorCodeInvalidProviderResult},
		"changed query":    {status: 200, finalURL: searchURL + "?search=Audi", code: provider.ErrorCodeInvalidProviderResult},
		"changed page":     {status: 200, finalURL: searchURL + "?search=BMW&pn=2", code: provider.ErrorCodeInvalidProviderResult},
		"extra query":      {status: 200, finalURL: searchURL + "?search=BMW&preuMax=1000", code: provider.ErrorCodeInvalidProviderResult},
		"duplicate query":  {status: 200, finalURL: searchURL + "?search=BMW&search=Audi", code: provider.ErrorCodeInvalidProviderResult},
		"malformed URL":    {status: 200, finalURL: searchURL + "?search=%zz", code: provider.ErrorCodeInvalidProviderResult},
		"credentials":      {status: 200, finalURL: "https://secret@www.buscocotxe.ad/ca/search?search=BMW", code: provider.ErrorCodeInvalidProviderResult},
		"raw failure":      {err: errors.New("secret response"), code: provider.ErrorCodeHTTPFailure},
		"coded failure":    {err: provider.NewError(provider.ErrorCodeAccessBlocked, "blocked", errors.New("secret cause")), code: provider.ErrorCodeAccessBlocked},
		"cancel":           {err: context.Canceled, code: context.Canceled},
		"deadline":         {err: context.DeadlineExceeded, code: context.DeadlineExceeded},
	} {
		t.Run(name, func(t *testing.T) {
			resources := conformance.NewFixtureService(conformance.ResourceFixture{Response: provider.ResourceResponse{StatusCode: test.status, FinalURL: test.finalURL, Body: readFixture(t, "search_page_1.html")}, Err: test.err})
			_, err := (implementation{}).Search(t.Context(), provider.SearchRequest{Market: testMarket(), Resources: resources, Query: "BMW"})
			if !errors.Is(err, test.code) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error=%v, want %v", err, test.code)
			}
			if calls, _ := resources.Stats(); calls != 1 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}

func TestSearchCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	resources := conformance.NewFixtureService()
	_, err := (implementation{}).Search(ctx, provider.SearchRequest{Market: testMarket(), Resources: resources, Query: "BMW"})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls, _ := resources.Stats(); calls != 0 {
		t.Fatal("canceled request fetched a resource")
	}
	ctx, cancel = context.WithCancel(t.Context())
	resources = conformance.NewFixtureService(conformance.ResourceFixture{CheckRequest: func(provider.ResourceRequest) error { cancel(); return nil }, Response: provider.ResourceResponse{StatusCode: 200, Body: readFixture(t, "search_page_1.html")}})
	_, err = (implementation{}).Search(ctx, provider.SearchRequest{Market: testMarket(), Resources: resources, Query: "BMW"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during fetch: %v", err)
	}
}

func testMarket() provider.Market {
	return provider.Market{Country: "AD", Language: "ca", Currency: "EUR"}
}
