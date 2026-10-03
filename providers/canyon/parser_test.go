package canyon

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/provider/conformance"
	"github.com/kostyay/ecom/providers/internal/shoputil"
	"golang.org/x/net/html"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestStartingPricesDiscountsAndUnknownStock(t *testing.T) {
	result, err := parseSearch(provider.ResourceResponse{Body: fixture(t, "search.html")}, 1, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	first := result.Items[0]
	if first.Availability != provider.AvailabilityUnknown || first.Brand != "Canyon" || first.URL != "https://www.canyon.com/en-es/mountain-bikes/trail-bikes/spectral/al/spectral-6/4378.html" {
		t.Fatalf("first=%+v", first)
	}
	starting := result.Items[23]
	if starting.Price.Amount != "3799.00" || starting.Price.Display != "From 3.799 €" || string(starting.ProviderData[Name]) != `{"starting_price":true}` {
		t.Fatalf("starting=%+v", starting)
	}
	last, err := parseSearch(provider.ResourceResponse{Body: fixture(t, "last.html")}, 3, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if last.Items[0].Availability != provider.AvailabilityOutOfStock || last.Items[0].OriginalPrice.Amount != "6999.00" {
		t.Fatalf("outlet=%+v", last.Items[0])
	}
}
func TestPaginationAndResultFailures(t *testing.T) {
	original := fixture(t, "search.html")
	for _, test := range []struct{ old, new string }{
		{`data-search-tab-type="bikes"`, `data-search-tab-type="gears"`},
		{`data-count="50"`, `data-count="0"`},
		{`data-page-size="24.0"`, `data-page-size="48.0"`},
		{`productTileDefault__productName link`, `missing-name link`},
	} {
		body := bytes.ReplaceAll(original, []byte(test.old), []byte(test.new))
		if bytes.Equal(body, original) {
			t.Fatal("mutation did not apply:", test.old)
		}
		if _, err := parseSearch(provider.ResourceResponse{Body: body}, 1, "EUR"); !errors.Is(err, provider.ErrorCodeInvalidProviderResult) {
			t.Fatalf("%s: %v", test.old, err)
		}
	}
	if _, err := parseSearch(provider.ResourceResponse{Body: fixture(t, "empty.html")}, 2, "EUR"); !errors.Is(err, provider.ErrorCodeInvalidProviderResult) {
		t.Fatal("out-of-range page accepted as empty")
	}
}
func TestNextPageLinkValidation(t *testing.T) {
	valid := baseURL + "/en-es/shop/?start=24&sz=24&searchType=bikes"
	for _, test := range []struct {
		name, url string
		valid     bool
	}{
		{"valid", valid, true},
		{"encoded offset", strings.Replace(valid, "start=24", "start=%32%34", 1), true},
		{"duplicate offset keeps first", valid + "&start=48", true},
		{"unrelated malformed query", valid + "&unused=%ZZ", true},
		{"invalid URL", ":bad", false},
		{"HTTP", strings.Replace(valid, "https:", "http:", 1), false},
		{"foreign host", strings.Replace(valid, "www.canyon.com", "evil.example", 1), false},
		{"credentials", strings.Replace(valid, "https://", "https://secret@", 1), false},
		{"other market", strings.Replace(valid, "/en-es/", "/en-us/", 1), false},
		{"other endpoint", strings.Replace(valid, "/shop/", "/search/", 1), false},
		{"relative URL", strings.TrimPrefix(valid, baseURL), false},
		{"wrong offset", strings.Replace(valid, "start=24", "start=48", 1), false},
		{"wrong size", strings.Replace(valid, "sz=24", "sz=48", 1), false},
		{"wrong tab", strings.Replace(valid, "bikes", "gears", 1), false},
		{"missing offset", strings.Replace(valid, "start=24&", "", 1), false},
		{"missing size", strings.Replace(valid, "sz=24&", "", 1), false},
		{"missing tab", strings.Replace(valid, "&searchType=bikes", "", 1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := html.Parse(bytes.NewReader(fixture(t, "search.html")))
			if err != nil {
				t.Fatal(err)
			}
			next := shoputil.FindClass(root, "js-showMoreGrid")
			if next == nil {
				t.Fatal("missing next-page fixture link")
			}
			for i := range next.Attr {
				if next.Attr[i].Key == "data-url" {
					next.Attr[i].Val = test.url
				}
			}
			var body bytes.Buffer
			if err := html.Render(&body, root); err != nil {
				t.Fatal(err)
			}
			result, err := parseSearch(provider.ResourceResponse{Body: body.Bytes()}, 1, "EUR")
			if test.valid {
				if err != nil || len(result.Items) != 24 {
					t.Fatalf("items=%d err=%v", len(result.Items), err)
				}
			} else if !errors.Is(err, provider.ErrorCodeInvalidProviderResult) || err.Error() != "Canyon returned an invalid next page" {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestBrowserPageSnapshot(t *testing.T) {
	resources := conformance.NewFixtureService(conformance.ResourceFixture{Response: provider.ResourceResponse{StatusCode: 200, FinalURL: baseURL + "/en-es/search/?q=spectral", Body: []byte("not HTML"), Page: &provider.PageSnapshot{HTML: fixture(t, "search.html")}, Transport: provider.TransportBrowser}})
	result, err := (implementation{}).Search(t.Context(), provider.SearchRequest{Resources: resources, Market: provider.Market{Country: "ES", Language: "en", Currency: "EUR"}, Query: "spectral"})
	if err != nil || len(result.Items) != 24 {
		t.Fatalf("items=%d err=%v", len(result.Items), err)
	}
}
