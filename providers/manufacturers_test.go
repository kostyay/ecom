package providers_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/provider/conformance"
	_ "github.com/kostyay/ecom/providers/canyon"
	_ "github.com/kostyay/ecom/providers/propain"
	_ "github.com/kostyay/ecom/providers/ytindustries"
)

type manufacturer struct {
	name, directory, country, query, extension string
	size, last, lastCount, total               int
}

var manufacturers = []manufacturer{
	{name: "yt-industries", directory: "ytindustries", country: "DE", query: "capra", extension: "json", size: 24, last: 3, lastCount: 9, total: 57},
	{name: "propain", directory: "propain", country: "DE", query: "tyee", extension: "html", size: 10, last: 4, lastCount: 9},
	{name: "canyon", directory: "canyon", country: "ES", query: "spectral", extension: "html", size: 24, last: 3, lastCount: 2, total: 50},
}
var captureTime = time.Date(2026, 10, 3, 14, 40, 0, 0, time.UTC)

func fixture(t *testing.T, m manufacturer, file string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(m.directory, "testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func searchRequest(m manufacturer, resources provider.ResourceService) provider.SearchRequest {
	return provider.SearchRequest{Market: provider.Market{Country: m.country, Language: "en", Currency: "EUR"}, Resources: resources, Query: m.query}
}

func registered(t *testing.T, name string) provider.Provider {
	t.Helper()
	p, ok := provider.Lookup(name)
	if !ok {
		t.Fatal("Provider not registered:", name)
	}
	return p
}

func searchFixtures(t *testing.T, m manufacturer, body []byte) []conformance.ResourceFixture {
	t.Helper()
	var fixtures []conformance.ResourceFixture
	if m.name == "yt-industries" {
		for _, file := range []string{"home.html", "context.json"} {
			fixtures = append(fixtures, conformance.ResourceFixture{Response: provider.ResourceResponse{Body: fixture(t, m, file), StatusCode: 200, RetrievedAt: captureTime}})
		}
	}
	return append(fixtures, conformance.ResourceFixture{Response: provider.ResourceResponse{Body: body, StatusCode: 200, RetrievedAt: captureTime}})
}

func TestManufacturerConformance(t *testing.T) {
	for _, m := range manufacturers {
		t.Run(m.name, func(t *testing.T) {
			pages := []struct {
				name, file, currency string
				page, count          int
				next                 bool
			}{
				{name: "first", file: "search", currency: "EUR", page: 1, count: m.size, next: true},
				{name: "second", file: "second", currency: "EUR", page: 2, count: m.size, next: true},
				{name: "last", file: "last", currency: "EUR", page: m.last, count: m.lastCount},
				{name: "empty", file: "empty", currency: "EUR", page: 1},
				{name: "unavailable currency", file: "search", currency: "USD", page: 1, count: m.size, next: true},
			}
			var fixtures []conformance.ResourceFixture
			for _, test := range pages {
				fixtures = append(fixtures, searchFixtures(t, m, fixture(t, m, test.file+"."+m.extension))...)
			}
			resources := conformance.NewFixtureService(fixtures...)
			var cases []conformance.OperationCase
			for _, test := range pages {
				cases = append(cases, conformance.OperationCase{
					Name: test.name, Capability: provider.CapabilitySearch,
					Invoke: func(ctx context.Context, p provider.Provider) (any, error) {
						r := searchRequest(m, resources)
						r.Page.Number, r.Market.Currency = test.page, test.currency
						if test.file == "empty" {
							r.Query = "zzzxxyy987654321"
						}
						return p.Search(ctx, r)
					},
					Check: func(value any) error {
						result := value.(provider.ProductPage)
						if len(result.Items) != test.count || result.Items == nil || result.Page.Number != test.page || result.Page.Size != m.size || result.Page.HasNext == nil || *result.Page.HasNext != test.next {
							return fmt.Errorf("items=%d, page=%+v", len(result.Items), result.Page)
						}
						if m.total != 0 {
							total, totalPages := m.total, m.last
							if test.file == "empty" {
								total, totalPages = 0, 0
							}
							if result.Page.TotalItems == nil || *result.Page.TotalItems != total || result.Page.TotalPages == nil || *result.Page.TotalPages != totalPages {
								return errors.New("wrong totals")
							}
						} else if result.Page.TotalItems != nil || result.Page.TotalPages != nil {
							return errors.New("Propain must not invent totals")
						}
						if test.currency == "USD" {
							if len(result.Warnings) != 1 || result.Warnings[0].Code != provider.WarningCodeCurrencyUnavailable || result.Warnings[0].RequestedCurrency != "USD" || result.Warnings[0].ActualCurrency != "EUR" {
								return fmt.Errorf("currency warnings=%+v", result.Warnings)
							}
						} else if len(result.Warnings) != 0 {
							return fmt.Errorf("unexpected warnings=%+v", result.Warnings)
						}
						for _, item := range result.Items {
							if item.RetrievedAt != captureTime || item.Price == nil || item.Price.Currency != "EUR" {
								return errors.New("missing retrieval time or actual EUR price")
							}
						}
						if test.file == "search" {
							item := result.Items[0]
							switch m.name {
							case "yt-industries":
								if item.ID != "018f0a8fb5f47205b57cdb09012b2e43" || item.Price.Amount != "34.90" || item.URL != "https://www.yt-industries.com/Parts-Accessories/Parts/cable-plug-set-JEFFSY-MK3-AL-Capra-MK3-AL/" || item.Brand != "YT" {
									return fmt.Errorf("wrong YT item: %+v", item)
								}
							case "propain":
								if item.ID != "619081" || item.Price.Amount != "5189.00" || item.OriginalPrice == nil || item.OriginalPrice.Amount != "5489.00" {
									return fmt.Errorf("wrong Propain item: %+v", item)
								}
							case "canyon":
								if item.ID != "4378" || item.Price.Amount != "2699.00" || item.Price.Display != "2.699 €" {
									return fmt.Errorf("wrong Canyon item: %+v", item)
								}
							}
						}
						return nil
					},
				})
			}
			p := registered(t, m.name)
			conformance.Run(t, conformance.Suite{
				Registration: provider.Registration{Name: m.name, SDKAPIVersion: provider.APIVersion, Implementation: p, Capabilities: p.Capabilities()},
				Resources:    resources, Cases: cases,
			})
		})
	}
}

type resourceFunc func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error)

func (f resourceFunc) Fetch(ctx context.Context, r provider.ResourceRequest) (provider.ResourceResponse, error) {
	return f(ctx, r)
}

func TestManufacturerHelpIsOfflineAndMarketIndependent(t *testing.T) {
	for _, m := range manufacturers {
		t.Run(m.name, func(t *testing.T) {
			calls := 0
			resources := resourceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
				calls++
				return provider.ResourceResponse{}, errors.New("unexpected help resource request")
			})
			p := registered(t, m.name)
			help, err := p.Help(t.Context(), provider.HelpRequest{Resources: resources})
			if err != nil {
				t.Fatal(err)
			}
			other, err := p.Help(t.Context(), provider.HelpRequest{Resources: resources, Market: provider.Market{Country: "FR"}})
			if err != nil || !reflect.DeepEqual(help, other) || calls != 0 {
				t.Fatalf("help changed or fetched resources: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestManufacturerRejectsBeforeResourceFetch(t *testing.T) {
	for _, m := range manufacturers {
		t.Run(m.name, func(t *testing.T) {
			for _, test := range []struct {
				name string
				code error
			}{
				{"country", provider.ErrorCodeInvalidResourceRequest},
				{"page size", provider.ErrorCodeInvalidFilter},
				{"canceled", context.Canceled},
			} {
				t.Run(test.name, func(t *testing.T) {
					calls := 0
					// Record every attempt, including calls made with a canceled context.
					resources := resourceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
						calls++
						return provider.ResourceResponse{}, errors.New("unexpected resource request")
					})
					r := searchRequest(m, resources)
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					switch test.name {
					case "country":
						r.Market.Country = "US"
					case "page size":
						r.Page.Size = m.size + 1
					case "canceled":
						cancel()
					}
					_, err := registered(t, m.name).Search(ctx, r)
					if !errors.Is(err, test.code) || calls != 0 {
						t.Fatalf("error=%v resource calls=%d", err, calls)
					}
				})
			}
		})
	}
}

func TestManufacturerMalformedResultsAndWrongPages(t *testing.T) {
	for _, m := range manufacturers {
		t.Run(m.name, func(t *testing.T) {
			for _, body := range []string{"", `{}`, `null`, `<html>secret error</html>`} {
				resources := conformance.NewFixtureService(searchFixtures(t, m, []byte(body))...)
				_, err := registered(t, m.name).Search(t.Context(), searchRequest(m, resources))
				if !errors.Is(err, provider.ErrorCodeInvalidProviderResult) || strings.Contains(err.Error(), "secret") {
					t.Fatalf("body=%q, err=%v", body, err)
				}
			}
			resources := conformance.NewFixtureService(searchFixtures(t, m, fixture(t, m, "search."+m.extension))...)
			r := searchRequest(m, resources)
			r.Page.Number = 2
			if _, err := registered(t, m.name).Search(t.Context(), r); !errors.Is(err, provider.ErrorCodeInvalidProviderResult) {
				t.Fatalf("wrong page accepted: %v", err)
			}
		})
	}
}

func TestManufacturerPartialParsing(t *testing.T) {
	for _, m := range manufacturers {
		t.Run(m.name, func(t *testing.T) {
			body := fixture(t, m, "search."+m.extension)
			var old, replacement string
			switch m.name {
			case "yt-industries":
				old, replacement = `"name": "cable plug set JEFFSY MK3 AL \u0026 Capra MK3 AL"`, `"name": ""`
			case "propain":
				old, replacement = `class="woocommerce-loop-product__title"> Tyee 6.1 CF L Trail 29 – Ready to Ride `, `class="woocommerce-loop-product__title">`
			case "canyon":
				old, replacement = `href="https://www.canyon.com/en-es/mountain-bikes/trail-bikes/spectral/al/spectral-6/4378.html?dwvar_4378_pv_rahmenfarbe=M178_P04"`, `href="https://evil.example/4378.html"`
			}
			altered := bytes.ReplaceAll(body, []byte(old), []byte(replacement))
			if bytes.Equal(body, altered) {
				t.Fatal("test mutation did not change the fixture")
			}
			resources := conformance.NewFixtureService(searchFixtures(t, m, altered)...)
			result, err := registered(t, m.name).Search(t.Context(), searchRequest(m, resources))
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Items) != m.size-1 || len(result.Warnings) != 1 || result.Warnings[0].Code != provider.WarningCodePartialParsing || *result.Warnings[0].FoundCount != m.size || *result.Warnings[0].ParsedCount != m.size-1 {
				t.Fatalf("items=%d warnings=%+v", len(result.Items), result.Warnings)
			}
		})
	}
}
