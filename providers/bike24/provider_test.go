package bike24

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/provider/conformance"
)

func TestProviderConformanceOffline(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "search.html"))
	if err != nil {
		t.Fatal(err)
	}
	resources := conformance.NewFixtureService(conformance.ResourceFixture{
		Response: provider.ResourceResponse{StatusCode: http.StatusOK, Body: body},
		CheckRequest: func(request provider.ResourceRequest) error {
			wantQuery := []provider.RequestValue{{Name: "searchTerm", Values: []string{"Garmin Edge"}}}
			if request.Method != http.MethodGet || request.URL != baseURL+"/search" || !reflect.DeepEqual(request.Query, wantQuery) {
				return fmt.Errorf("resource = %#v", request)
			}
			wantTransport := []provider.TransportMode{provider.TransportHTTP, provider.TransportBrowser, provider.TransportCDP}
			if !reflect.DeepEqual(request.Transport.Preferred, wantTransport) {
				return fmt.Errorf("transport order = %v", request.Transport.Preferred)
			}
			return nil
		},
	})
	request := provider.Request{
		Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"}, Resources: resources,
	}
	conformance.Run(t, conformance.Suite{
		Registration: registration(), Resources: resources,
		Cases: []conformance.OperationCase{{
			Name: "public product search", Capability: provider.CapabilitySearch,
			Invoke: func(ctx context.Context, registered provider.Provider) (any, error) {
				return registered.Search(ctx, provider.SearchRequest{Request: request, Query: "Garmin Edge"})
			},
			Check: func(value any) error {
				page := value.(provider.ProductPage)
				if len(page.Items) != 2 || page.Items[0].ID != "2963890" || page.Items[0].Price.Amount != "599.95" || page.Items[0].OriginalPrice.Amount != "649.99" || page.Items[1].Price.Amount != "749.00" {
					return fmt.Errorf("items = %#v", page.Items)
				}
				if page.Page.TotalItems == nil || *page.Page.TotalItems != 2 || page.Page.TotalPages == nil || *page.Page.TotalPages != 1 || page.Page.HasNext == nil || *page.Page.HasNext {
					return fmt.Errorf("page = %#v", page.Page)
				}
				if len(page.Warnings) != 1 || page.Warnings[0].Code != provider.WarningCodePartialParsing {
					return fmt.Errorf("warnings = %#v", page.Warnings)
				}
				return nil
			},
		}},
	})
}

func TestSearchRejectsUnsupportedInputBeforeFetch(t *testing.T) {
	_, err := (implementation{}).Search(t.Context(), provider.SearchRequest{
		Request: provider.Request{Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"}},
		Query:   "bike", Filters: []provider.Filter{{Key: "brand", Value: "Garmin"}},
	})
	if code, ok := provider.ErrorCodeOf(err); !ok || code != provider.ErrorCodeInvalidFilter {
		t.Fatalf("error = %v", err)
	}
}

func TestSearchStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	_, err := (implementation{}).Search(ctx, provider.SearchRequest{
		Request: provider.Request{
			Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"},
			Resources: resourceServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
				called = true
				return provider.ResourceResponse{}, nil
			}),
		},
		Query: "bike",
	})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("error = %v, resource called = %t", err, called)
	}
}

func TestSearchPageAndPolicy(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "search.html"))
	if err != nil {
		t.Fatal(err)
	}
	market := provider.Market{Country: "DE", Language: "en", Currency: "EUR"}
	cache := provider.CachePolicy{Refresh: true, StaleIfError: true}
	resources := resourceServiceFunc(func(_ context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
		wantQuery := []provider.RequestValue{
			{Name: "searchTerm", Values: []string{"Garmin"}},
			{Name: "page", Values: []string{"2"}},
		}
		if !reflect.DeepEqual(request.Query, wantQuery) || request.Market != market || request.Cache != cache || !request.Interactive {
			t.Fatalf("request = %#v", request)
		}
		body = bytes.ReplaceAll(body, []byte(`\"page\":0`), []byte(`\"page\":1`))
		body = bytes.ReplaceAll(body, []byte(`\"nbPages\":1`), []byte(`\"nbPages\":2`))
		return provider.ResourceResponse{Body: body, RetrievedAt: time.Unix(123, 0)}, nil
	})
	page, err := (implementation{}).Search(t.Context(), provider.SearchRequest{
		Request: provider.Request{Market: market, Cache: cache, Interactive: true, Resources: resources},
		Query:   "Garmin", Page: provider.PageRequest{Number: 2, Size: defaultPageSize},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Page.Number != 2 || page.Page.Size != defaultPageSize || page.Page.TotalItems == nil || *page.Page.TotalItems != 2 || page.Items[0].RetrievedAt != time.Unix(123, 0) {
		t.Fatalf("page = %#v", page)
	}
}

func TestExtractProductsUsesLaterValidDuplicate(t *testing.T) {
	document := []byte(`<article>
		<a href="/p2123.html?origin=SRP"></a>
		<a href="/p2123.html?origin=SRP" title="Valid product"><img src="data:image/png;base64,broken"><strong>5,00 €</strong></a>
	</article>`)
	items, warnings, err := extractProducts(document, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "Valid product" || items[0].ImageURL != "" || items[0].Price.Amount != "5.00" {
		t.Fatalf("items = %#v", items)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestProductPricesStayInTheirCards(t *testing.T) {
	document := []byte(`<main>
		<a href="/p1.html?origin=SRP" title="No price"></a>
		<a href="/p2.html?origin=SRP" title="Current before RRP"><b>99,00 €</b><s>129,00 € RRP</s></a>
		<a href="/p3.html?origin=SRP" title="One digit"><b>9 €</b></a>
	</main>`)
	items, _, err := extractProducts(document, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Price != nil || items[1].Price.Amount != "99.00" || items[1].OriginalPrice.Amount != "129.00" || items[2].Price.Amount != "9" {
		t.Fatalf("items = %#v", items)
	}
}

func TestSearchAcceptsVerifiedEmptyPage(t *testing.T) {
	document := []byte(`<script>{"initialResults":{"hitsPerPage":30,"indexName":"production_SEARCH_INDEX_EN","nbHits":0,"nbPages":0,"page":0,"searchTerm":"nothing"}}</script>
		<footer>selected delivery country Germany</footer>`)
	page, err := (implementation{}).Search(t.Context(), provider.SearchRequest{
		Request: provider.Request{
			Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"},
			Resources: resourceServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
				return provider.ResourceResponse{Body: document}, nil
			}),
		},
		Query: "nothing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items == nil || len(page.Items) != 0 || page.Page.TotalItems == nil || *page.Page.TotalItems != 0 {
		t.Fatalf("page = %#v", page)
	}
}

func TestSearchPageInfoIgnoresFieldOrderAndIndexName(t *testing.T) {
	document := []byte(`<script>self.__next_f.push([1,"{\"initialResults\":{\"page\":1,\"nbPages\":3,\"indexName\":\"renamed\",\"nbHits\":77,\"hitsPerPage\":30}}"])</script>`)
	page, ok := searchPageInfo(document)
	if !ok || page.Number != 2 || page.TotalItems == nil || *page.TotalItems != 77 || page.TotalPages == nil || *page.TotalPages != 3 {
		t.Fatalf("page = %#v, ok = %t", page, ok)
	}
}

func TestSearchRejectsWrongReturnedMarket(t *testing.T) {
	document := []byte(`<script>{"initialResults":{"hitsPerPage":30,"indexName":"production_SEARCH_INDEX_EN","nbHits":1,"nbPages":1,"page":0,"searchTerm":"bike"}}</script>
		<a href="/p1.html?origin=SRP" title="Bike">9 €</a><footer>selected delivery country United States</footer>`)
	_, err := (implementation{}).Search(t.Context(), provider.SearchRequest{
		Request: provider.Request{
			Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"},
			Resources: resourceServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
				return provider.ResourceResponse{Body: document}, nil
			}),
		},
		Query: "bike",
	})
	if code, ok := provider.ErrorCodeOf(err); !ok || code != provider.ErrorCodeHTTPFailure {
		t.Fatalf("error = %v, code = %q", err, code)
	}
}

func TestInputValidation(t *testing.T) {
	valid := provider.Request{Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"}}
	tests := []struct {
		name   string
		change func(*provider.SearchRequest)
		code   provider.ErrorCode
	}{
		{name: "empty query", change: func(request *provider.SearchRequest) { request.Query = " " }, code: provider.ErrorCodeInvalidFilter},
		{name: "sort", change: func(request *provider.SearchRequest) { request.Sort = &provider.Sort{Value: "price"} }, code: provider.ErrorCodeInvalidFilter},
		{name: "negative page", change: func(request *provider.SearchRequest) { request.Page.Number = -1 }, code: provider.ErrorCodeInvalidFilter},
		{name: "wrong page size", change: func(request *provider.SearchRequest) { request.Page.Size = 48 }, code: provider.ErrorCodeInvalidFilter},
		{name: "shipping", change: func(request *provider.SearchRequest) { request.Pricing.IncludeShipping = true }, code: provider.ErrorCodeInvalidProviderConfig},
		{name: "market", change: func(request *provider.SearchRequest) { request.Market.Currency = "USD" }, code: provider.ErrorCodeInvalidResourceRequest},
		{name: "no resources", change: func(*provider.SearchRequest) {}, code: provider.ErrorCodeInvalidResourceRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := provider.SearchRequest{Request: valid, Query: "bike"}
			test.change(&request)
			_, err := (implementation{}).Search(t.Context(), request)
			if code, ok := provider.ErrorCodeOf(err); !ok || code != test.code {
				t.Fatalf("error = %v, code = %q", err, code)
			}
		})
	}
}

func TestSearchMapsBrowserFailure(t *testing.T) {
	_, err := (implementation{}).Search(t.Context(), provider.SearchRequest{
		Request: provider.Request{
			Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"},
			Resources: resourceServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
				return provider.ResourceResponse{Transport: provider.TransportBrowser}, errors.New("navigation failed")
			}),
		},
		Query: "bike",
	})
	if code, ok := provider.ErrorCodeOf(err); !ok || code != provider.ErrorCodeBrowserFailure {
		t.Fatalf("error = %v, code = %q", err, code)
	}
}

func TestConfigValidation(t *testing.T) {
	if err := (implementation{}).ValidateConfig(nil); err != nil {
		t.Fatal(err)
	}
	err := (implementation{}).ValidateConfig(map[string]any{"page_size": 30})
	if code, ok := provider.ErrorCodeOf(err); !ok || code != provider.ErrorCodeInvalidProviderConfig {
		t.Fatalf("error = %v, code = %q", err, code)
	}
}

func FuzzExtractProducts(f *testing.F) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "search.html"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(fixture)
	f.Add([]byte(`<a href="/p21.html?origin=SRP" title="Product"><strong>1.234,56 €</strong></a>`))
	f.Add([]byte(`<a href="https://evil.example/p21.html?origin=SRP" title="Product">9,99 €</a>`))
	f.Add([]byte(`<a href="/p0.html?origin=SRP" title="Product">0000000€0000000</a>`))
	f.Add([]byte(`<a href="/p0.html?origin=SRP" title="Product"><img src="#invalid"></a>`))
	f.Fuzz(func(t *testing.T, document []byte) {
		items, _, err := extractProducts(document, time.Time{})
		if err != nil {
			return
		}
		for _, item := range items {
			if err := item.Validate(); err != nil {
				t.Fatalf("invalid product from %q: %#v: %v", document, item, err)
			}
		}
	})
}

type resourceServiceFunc func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error)

func (function resourceServiceFunc) Fetch(ctx context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
	return function(ctx, request)
}
