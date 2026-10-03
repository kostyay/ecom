package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/kostyay/ecom/internal/cli"
	"github.com/kostyay/ecom/internal/output"
	"github.com/kostyay/ecom/provider"
)

type manufacturerRoundTripper func(*http.Request) (*http.Response, error)

func (f manufacturerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestManufacturerCLI(t *testing.T) {
	const specialQuery = `bike & size=M + "quoted"?`
	for _, test := range []struct {
		name, directory, country, host, query, id, price string
		size, requests                                   int
	}{
		{name: "yt-industries", directory: "ytindustries", country: "DE", host: "www.yt-industries.com", query: "capra", id: "018f0a8fb5f47205b57cdb09012b2e43", price: "34.90 €", size: 24, requests: 3},
		{name: "propain", directory: "propain", country: "DE", host: "www.propain-bikes.com", query: "tyee", id: "619081", price: "5.189,00 €", size: 10, requests: 1},
		{name: "canyon", directory: "canyon", country: "ES", host: "www.canyon.com", query: "spectral", id: "4378", price: "2.699 €", size: 24, requests: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			t.Setenv("ECOM_CACHE_PATH", filepath.Join(directory, "cache.db"))
			t.Setenv("ECOM_MARKET_COUNTRY", test.country)
			t.Setenv("ECOM_MARKET_LANGUAGE", "en")
			t.Setenv("ECOM_MARKET_CURRENCY", "EUR")
			t.Setenv("ECOM_NETWORK_REQUESTS_PER_SECOND", "1000")
			t.Setenv("ECOM_NETWORK_RETRIES", "0")
			query, page, calls := test.query, 1, 0
			previous := http.DefaultClient.Transport
			http.DefaultClient.Transport = manufacturerRoundTripper(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.Scheme != "https" || request.URL.Host != test.host {
					return nil, errors.New("unexpected manufacturer host")
				}
				file := "search"
				if page == 2 {
					file = "second"
				}
				if query == specialQuery {
					file = "empty"
				}
				switch test.name {
				case "yt-industries":
					file += ".json"
					if request.URL.RawQuery != "" {
						return nil, errors.New("unexpected YT query parameters")
					}
					switch request.URL.Path {
					case "/":
						if request.Method != http.MethodGet {
							return nil, errors.New("unexpected storefront method")
						}
						file = "home.html"
					case "/store-api/context":
						if request.Method != http.MethodGet || request.Header.Get("sw-access-key") != "FIXTUREPUBLICKEY" {
							return nil, errors.New("unexpected YT context request")
						}
						file = "context.json"
					case "/store-api/search":
						if request.Method != http.MethodPost || request.Header.Get("sw-access-key") != "FIXTUREPUBLICKEY" || request.Header.Get("Content-Type") != "application/json" {
							return nil, errors.New("unexpected YT search method or headers")
						}
						var body struct {
							Search         string                     `json:"search"`
							Page           int                        `json:"page"`
							Limit          int                        `json:"limit"`
							TotalCountMode int                        `json:"total-count-mode"`
							Associations   map[string]json.RawMessage `json:"associations"`
						}
						if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
							return nil, err
						}
						if body.Search != query || body.Page != page || body.Limit != 24 || body.TotalCountMode != 1 {
							return nil, errors.New("incorrect serialized YT search")
						}
						var cover struct {
							Associations map[string]json.RawMessage `json:"associations"`
						}
						if body.Associations["seoUrls"] == nil || json.Unmarshal(body.Associations["cover"], &cover) != nil || cover.Associations["media"] == nil {
							return nil, errors.New("missing YT product associations")
						}
					default:
						return nil, errors.New("unexpected YT endpoint")
					}
				case "propain":
					file += ".html"
					path := "/en/"
					if page == 2 {
						path = "/en/page/2/"
					}
					want := url.Values{"s": {query}, "post_type": {"product"}, "wcpbc-manual-country": {"DE"}}
					if request.Method != http.MethodGet || request.URL.Path != path || !reflect.DeepEqual(request.URL.Query(), want) {
						return nil, errors.New("incorrect serialized Propain search")
					}
				case "canyon":
					file += ".html"
					start, pn := "0", "0"
					if page == 2 {
						start, pn = "24", "1"
					}
					want := url.Values{"q": {query}, "searchType": {"bikes"}, "start": {start}, "sz": {"24"}, "pn": {pn}, "searchredirect": {"false"}}
					if request.Method != http.MethodGet || request.URL.Path != "/en-es/search/" || !reflect.DeepEqual(request.URL.Query(), want) {
						return nil, errors.New("incorrect serialized Canyon search")
					}
				}
				body, err := os.ReadFile(filepath.Join("providers", test.directory, "testdata", file))
				if err != nil {
					return nil, err
				}
				if test.name == "yt-industries" && file == "empty.json" {
					// Fixed server reply for the special-character query, not an echo of the request.
					encoded, err := json.Marshal(specialQuery)
					if err != nil {
						return nil, err
					}
					body = bytes.Replace(body, []byte(`"zzzxxyy987654321"`), encoded, 1)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
			})
			t.Cleanup(func() { http.DefaultClient.Transport = previous })

			type searchOutput struct {
				Provider string                                      `json:"provider"`
				Market   provider.Market                             `json:"market"`
				Data     output.ListingData[provider.ProductSummary] `json:"data"`
				Page     provider.PageInfo                           `json:"page"`
				Cache    *output.CacheMetadata                       `json:"cache"`
				Warnings []provider.Warning                          `json:"warnings"`
			}
			run := func(extra ...string) searchOutput {
				t.Helper()
				args := []string{"search", "  " + query + "  ", "--page", strconv.Itoa(page), "--provider", test.name, "--log-file", filepath.Join(directory, "ecom.log")}
				args = append(args, extra...)
				var stdout, stderr bytes.Buffer
				if status := cli.Run(t.Context(), args, &stdout, &stderr); status != 0 || stderr.Len() != 0 {
					t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
				}
				var result searchOutput
				if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Provider != test.name || result.Market.Country != test.country || result.Market.Currency != "EUR" || result.Page.Number != page || result.Cache == nil || len(result.Warnings) != 0 {
					t.Fatalf("unexpected command output: %+v", result)
				}
				return result
			}

			first := run()
			if len(first.Data.Items) != test.size || first.Page.HasNext == nil || !*first.Page.HasNext || first.Cache.Hit || calls != test.requests {
				t.Fatalf("first page=%+v cache=%+v calls=%d", first.Page, first.Cache, calls)
			}
			item := first.Data.Items[0]
			if item.ID != test.id || item.Price == nil || item.Price.Display != test.price {
				t.Fatalf("first item=%+v", item)
			}

			cached := run()
			if !cached.Cache.Hit || cached.Cache.HitCount != test.requests || calls != test.requests || len(cached.Data.Items) != test.size || cached.Data.Items[0].ID != test.id {
				t.Fatalf("cache replay=%+v calls=%d", cached.Cache, calls)
			}

			refreshed := run("--refresh")
			if refreshed.Cache.Hit || refreshed.Cache.HitCount != 0 || calls != 2*test.requests || len(refreshed.Data.Items) != test.size {
				t.Fatalf("refresh=%+v calls=%d", refreshed.Cache, calls)
			}

			page = 2
			second := run()
			if len(second.Data.Items) != test.size || second.Data.Items[0].ID == test.id || calls != 2*test.requests+1 {
				t.Fatalf("second page items=%d calls=%d", len(second.Data.Items), calls)
			}

			query, page = specialQuery, 1
			empty := run()
			if empty.Data.Items == nil || len(empty.Data.Items) != 0 || empty.Page.HasNext == nil || *empty.Page.HasNext || calls != 2*test.requests+2 {
				t.Fatalf("special query page=%+v items=%d calls=%d", empty.Page, len(empty.Data.Items), calls)
			}
		})
	}
}
