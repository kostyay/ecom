package tradeinn

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/provider/conformance"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func testRequest(resources provider.ResourceService) provider.SearchRequest {
	return provider.SearchRequest{Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"}, Resources: resources, Query: "powertube"}
}

func TestProviderConformance(t *testing.T) {
	for _, test := range []struct {
		name, query        string
		files              []string
		page, count, total int
		hasNext            bool
	}{
		{"first", "powertube", []string{"search.json"}, 1, 45, 48, true},
		{"last", "powertube", []string{"search.json", "last.json"}, 2, 3, 48, false},
		{"after last", "powertube", []string{"search.json", "last.json"}, 3, 0, 48, false},
		{"expanded", "zzzxxyy987654321", []string{"expanded.json"}, 1, 9, 9, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var fixtures []conformance.ResourceFixture
			for i, file := range test.files {
				fixtures = append(fixtures, conformance.ResourceFixture{
					Response: provider.ResourceResponse{StatusCode: 200, FinalURL: searchURL, Body: readFixture(t, file)},
					CheckRequest: func(r provider.ResourceRequest) error {
						form, err := url.ParseQuery(string(r.Body.Bytes))
						if err != nil {
							return err
						}
						token := "null"
						if i > 0 {
							token = "fixture-page-2"
						}
						if r.Method != http.MethodPost || r.URL != searchURL || r.Transport.Required != provider.TransportHTTP || form.Get("palabras") != test.query || form.Get("nextToken") != token || form.Get("action") != "buscador_google" || form.Get("id_tienda") != "0" || form.Get("idioma") != "eng" || form.Get("visitorid") != visitorID {
							return errors.New("unexpected Tradeinn search request")
						}
						return nil
					},
				})
			}
			resources := conformance.NewFixtureService(fixtures...)
			conformance.Run(t, conformance.Suite{
				Registration: registration(), Resources: resources,
				Cases: []conformance.OperationCase{{
					Name: test.name, Capability: provider.CapabilitySearch,
					Invoke: func(ctx context.Context, p provider.Provider) (any, error) {
						r := testRequest(resources)
						r.Query, r.Page.Number = test.query, test.page
						return p.Search(ctx, r)
					},
					Check: func(value any) error {
						p := value.(provider.ProductPage)
						if len(p.Items) != test.count || p.Page.Number != test.page || p.Page.Size != 45 || p.Page.TotalItems == nil || *p.Page.TotalItems != test.total || p.Page.HasNext == nil || *p.Page.HasNext != test.hasNext || p.Page.TotalPages != nil {
							return fmt.Errorf("unexpected page: %+v; items=%d", p.Page, len(p.Items))
						}
						if test.name == "expanded" {
							if len(p.Warnings) != 1 || p.Warnings[0].Code != provider.WarningCodeSearchSemanticsUnverified || !strings.Contains(string(p.ProviderData[Name]), `"expanded_query":true`) {
								return errors.New("expanded query was not reported")
							}
						} else if len(p.Warnings) != 0 {
							return fmt.Errorf("unexpected warnings: %+v", p.Warnings)
						}
						if test.name == "first" && (p.Items[0].ID != "139041968" || p.Items[0].Name != "Bosch Powertube Vertical down tube battery" || p.Items[0].Price.Amount != "739.99" || p.Items[0].Price.Display != "739.99 €" || strings.Contains(p.Items[0].URL, "?") || !strings.Contains(p.Items[44].URL, "/traininn/")) {
							return errors.New("product values do not match the fixture")
						}
						return nil
					},
				}},
			})
		})
	}
}

func TestRequestPoliciesAndCacheIdentity(t *testing.T) {
	stamp := time.Date(2026, 9, 19, 17, 22, 51, 0, time.UTC)
	fixture := conformance.ResourceFixture{Response: provider.ResourceResponse{Body: readFixture(t, "search.json"), StatusCode: 200, RetrievedAt: stamp}}
	resources := conformance.NewFixtureService(fixture, fixture, fixture)
	r := testRequest(resources)
	r.Market = provider.Market{Country: "AD", Language: "en", Currency: "USD"}
	r.Cache = provider.CachePolicy{Refresh: true, StaleIfError: true}
	r.Interactive = true
	r.Query = "  Bosch & battery + 625Wh?  "
	for range 2 {
		p, err := (implementation{}).Search(t.Context(), r)
		if err != nil {
			t.Fatal(err)
		}
		if p.Items[0].Price.Amount != "675.99" || p.Items[0].RetrievedAt != stamp || len(p.Warnings) != 1 || p.Warnings[0].RequestedCurrency != "USD" || p.Warnings[0].ActualCurrency != "EUR" {
			t.Fatalf("result=%+v", p)
		}
	}
	r.Query = "different query"
	if _, err := (implementation{}).Search(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	requests := resources.Requests()
	if requests[0].CachePartition != requests[1].CachePartition || requests[0].CachePartition == requests[2].CachePartition || len(requests[0].CachePartition) != 64 {
		t.Fatal("unstable or colliding cache identity")
	}
	for _, req := range requests {
		if req.Market != r.Market || req.Cache != r.Cache || !req.Interactive || !req.Body.Sensitive {
			t.Fatal("request policy not forwarded")
		}
		headers := make(http.Header)
		for _, h := range req.Headers {
			headers[h.Name] = h.Values
		}
		if headers.Get("Cookie") != "id_pais=4" || headers.Get("Referer") != "https://www.tradeinn.com/en?country=ad" || headers.Get("Content-Type") != "application/x-www-form-urlencoded" || headers.Get("User-Agent") == "" {
			t.Fatalf("headers=%v", headers)
		}
	}
	form, err := url.ParseQuery(string(requests[0].Body.Bytes))
	if err != nil || form.Get("palabras") != "Bosch & battery + 625Wh?" {
		t.Fatalf("form=%v, err=%v", form, err)
	}
}

func TestParserFailuresAndPartialResults(t *testing.T) {
	doc, err := decodeSearch(readFixture(t, "search.json"))
	if err != nil {
		t.Fatal(err)
	}
	good := doc.Results[0]
	for name, raw := range map[string]string{
		"invalid JSON": "<html>blocked</html>", "empty body": "", "missing metadata": `{}`, "null": `null`,
		"API error": `{"status":"INVALID_ARGUMENT","totalSize":0}`, "nested error": `{"error":{"message":"secret"},"totalSize":0}`,
		"missing products": `{"totalSize":48}`, "negative total": `{"totalSize":-1}`, "empty cursor": `{"totalSize":0,"nextPageToken":"secret"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeSearch([]byte(raw))
			if !errors.Is(err, provider.ErrorCodeInvalidProviderResult) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error=%v", err)
			}
		})
	}
	for name, mutate := range map[string]func(*searchProduct){
		"foreign host": func(p *searchProduct) { p.Product.URI = "https://evil.example/bikeinn/en/battery/139041968/p" },
		"credentials": func(p *searchProduct) {
			p.Product.URI = "https://secret@www.tradeinn.com/bikeinn/en/battery/139041968/p"
		},
		"wrong ID":             func(p *searchProduct) { p.ID = "12" },
		"empty title":          func(p *searchProduct) { p.Product.Title = " " },
		"missing market price": func(p *searchProduct) { delete(p.Product.Attributes, "price_all_71_to_80") },
		"bad price": func(p *searchProduct) {
			a := p.Product.Attributes["price_all_71_to_80"]
			a.Text = []string{"75:1e2"}
			p.Product.Attributes["price_all_71_to_80"] = a
		},
		"duplicate price": func(p *searchProduct) {
			a := p.Product.Attributes["price_all_71_to_80"]
			a.Text = append(a.Text, a.Text...)
			p.Product.Attributes["price_all_71_to_80"] = a
		},
		"missing shop": func(p *searchProduct) { delete(p.Product.Attributes, "shopId") },
	} {
		t.Run(name, func(t *testing.T) {
			var entry searchProduct
			if err := json.Unmarshal(good, &entry); err != nil {
				t.Fatal(err)
			}
			mutate(&entry)
			bad, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			partial := searchDocument{Results: []jsontext.Value{good, bad, good, jsontext.Value(`{"id":false}`)}, TotalSize: new(4)}
			result, err := parseProducts(partial, "75", "powertube", 1, time.Time{})
			if err != nil || len(result.Items) != 1 || len(result.Warnings) != 1 || *result.Warnings[0].FoundCount != 4 || *result.Warnings[0].ParsedCount != 1 {
				t.Fatalf("partial=%+v, err=%v", result, err)
			}
			partial.Results = []jsontext.Value{bad}
			if _, err := parseProducts(partial, "75", "powertube", 1, time.Time{}); !errors.Is(err, provider.ErrorCodeInvalidProviderResult) {
				t.Fatalf("all-invalid error=%v", err)
			}
		})
	}
	empty, err := decodeSearch([]byte(`{"results":[],"totalSize":0}`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := parseProducts(empty, "75", "no match", 1, time.Time{})
	if err != nil || p.Items == nil || len(p.Items) != 0 || *p.Page.HasNext || *p.Page.TotalItems != 0 {
		t.Fatalf("empty=%+v, err=%v", p, err)
	}
}

func TestHelpAndInvalidInputs(t *testing.T) {
	p, ok := provider.Lookup(Name)
	if !ok {
		t.Fatal("Tradeinn is not registered")
	}
	first, err := p.Help(t.Context(), provider.HelpRequest{})
	if err != nil || first.Help.Validate() != nil || len(p.Capabilities()) != 1 || !p.Supports(provider.CapabilitySearch) {
		t.Fatalf("help=%+v, err=%v", first, err)
	}
	second, err := p.Help(t.Context(), provider.HelpRequest{Market: provider.Market{Country: "FR"}})
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("Help must be offline and deterministic")
	}
	if p.ValidateConfig(nil) != nil || p.ValidateConfig(map[string]any{}) != nil || !errors.Is(p.ValidateConfig(map[string]any{"secret": true}), provider.ErrorCodeInvalidProviderConfig) {
		t.Fatal("unexpected configuration validation")
	}
	for name, mutate := range map[string]func(*provider.SearchRequest){
		"empty": func(r *provider.SearchRequest) { r.Query = " " }, "short": func(r *provider.SearchRequest) { r.Query = "ab" },
		"negative page": func(r *provider.SearchRequest) { r.Page.Number = -1 }, "large page": func(r *provider.SearchRequest) { r.Page.Number = 11 },
		"size": func(r *provider.SearchRequest) { r.Page.Size = 20 }, "negative size": func(r *provider.SearchRequest) { r.Page.Size = -1 },
		"filter": func(r *provider.SearchRequest) { r.Filters = []provider.Filter{{Key: "shop", Value: "bikeinn"}} }, "sort": func(r *provider.SearchRequest) { r.Sort = &provider.Sort{} },
		"shipping": func(r *provider.SearchRequest) { r.Pricing.IncludeShipping = true }, "country": func(r *provider.SearchRequest) { r.Market.Country = "FR" },
		"language": func(r *provider.SearchRequest) { r.Market.Language = "de" }, "currency": func(r *provider.SearchRequest) { r.Market.Currency = "eur" },
		"resources": func(r *provider.SearchRequest) { r.Resources = nil },
	} {
		t.Run(name, func(t *testing.T) {
			resources := conformance.NewFixtureService()
			r := testRequest(resources)
			mutate(&r)
			if _, err := p.Search(t.Context(), r); err == nil {
				t.Fatal("invalid input accepted")
			}
			if calls, _ := resources.Stats(); calls != 0 {
				t.Fatal("invalid input made a resource request")
			}
		})
	}
}

func TestTransportFailuresAndPaginationCycle(t *testing.T) {
	for name, test := range map[string]struct {
		status    int
		finalURL  string
		err, want error
	}{
		"blocked": {status: 403, want: provider.ErrorCodeAccessBlocked}, "server": {status: 503, want: provider.ErrorCodeHTTPFailure},
		"redirect":    {status: 200, finalURL: "https://www.tradeinn.com/en", want: provider.ErrorCodeInvalidProviderResult},
		"raw error":   {err: errors.New("secret"), want: provider.ErrorCodeHTTPFailure},
		"coded error": {err: provider.NewError(provider.ErrorCodeResponseTooLarge, "too large", nil), want: provider.ErrorCodeResponseTooLarge},
		"canceled":    {err: context.Canceled, want: context.Canceled}, "deadline": {err: context.DeadlineExceeded, want: context.DeadlineExceeded},
	} {
		t.Run(name, func(t *testing.T) {
			resources := conformance.NewFixtureService(conformance.ResourceFixture{Response: provider.ResourceResponse{StatusCode: test.status, FinalURL: test.finalURL, Body: readFixture(t, "search.json")}, Err: test.err})
			_, err := (implementation{}).Search(t.Context(), testRequest(resources))
			if !errors.Is(err, test.want) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error=%v, want %v", err, test.want)
			}
		})
	}
	fixture := conformance.ResourceFixture{Response: provider.ResourceResponse{StatusCode: 200, Body: readFixture(t, "search.json")}}
	resources := conformance.NewFixtureService(fixture, fixture)
	r := testRequest(resources)
	r.Page.Number = 3
	if _, err := (implementation{}).Search(t.Context(), r); !errors.Is(err, provider.ErrorCodeInvalidProviderResult) {
		t.Fatalf("cycle error=%v", err)
	}
	if calls, _ := resources.Stats(); calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	resources = conformance.NewFixtureService()
	if _, err := (implementation{}).Search(ctx, testRequest(resources)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls, _ := resources.Stats(); calls != 0 {
		t.Fatal("canceled request fetched a resource")
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	fixture.CheckRequest = func(provider.ResourceRequest) error { cancel(); return nil }
	resources = conformance.NewFixtureService(fixture)
	if _, err := (implementation{}).Search(ctx, testRequest(resources)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
