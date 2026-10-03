package ytindustries

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/provider/conformance"
)

func fixtureDocument(t *testing.T) searchDocument {
	t.Helper()
	body, err := os.ReadFile("testdata/search.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc searchDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
func TestProductDetailsAndSafety(t *testing.T) {
	doc := fixtureDocument(t)
	var good product
	if err := json.Unmarshal(doc.Elements[2], &good); err != nil {
		t.Fatal(err)
	}
	item, ok := parseProduct(good, time.Time{})
	if !ok || item.Price.Amount != "2799.00" || item.OriginalPrice.Amount != "3499.00" || !strings.HasSuffix(item.URL, "?number=103473") {
		t.Fatalf("item=%+v", item)
	}
	for name, mutate := range map[string]func(*product){
		"ID":    func(p *product) { p.ID = "wrong" },
		"price": func(p *product) { p.Price.UnitPrice = "1e3" }, "list price": func(p *product) { p.Price.ListPrice.Price = "-1" },
		"foreign host": func(p *product) { p.URLs[0].Path = "https://evil.example/product" },
		"credentials":  func(p *product) { p.URLs[0].Path = "https://secret@www.yt-industries.com/product" },
		"number":       func(p *product) { p.URLs[0].Path = "Bikes/Enduro-Capra/?number=another" },
	} {
		t.Run(name, func(t *testing.T) {
			var p product
			if err := json.Unmarshal(doc.Elements[2], &p); err != nil {
				t.Fatal(err)
			}
			mutate(&p)
			if _, ok := parseProduct(p, time.Time{}); ok {
				t.Fatal("invalid product accepted")
			}
		})
	}
	good.Cheapest.HasRange = true
	good.Cheapest.UnitPrice = "2599"
	good.Extensions.Availability.Preorder = true
	item, ok = parseProduct(good, time.Time{})
	if !ok || item.Price.Amount != "2599.00" || string(item.ProviderData[Name]) != `{"starting_price":true}` || item.Availability != provider.AvailabilityPreorder {
		t.Fatalf("starting/preorder=%+v", item)
	}
	good.URLs = nil
	item, ok = parseProduct(good, time.Time{})
	if !ok || item.URL != "" {
		t.Fatal("missing SEO URL must remain absent without discarding an identified product")
	}
}
func TestCanonicalURLSelectionOrder(t *testing.T) {
	doc := fixtureDocument(t)
	for _, test := range []struct {
		name                                   string
		canonical, deleted, sameProduct, valid bool
	}{
		{"noncanonical", false, false, true, true},
		{"deleted", true, true, true, true},
		{"other product", true, false, false, true},
		{"invalid first matching URL", true, false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var p product
			if err := json.Unmarshal(doc.Elements[2], &p); err != nil {
				t.Fatal(err)
			}
			p.URLs = append(p.URLs, p.URLs[0])
			p.URLs[0].Path = "https://evil.example/product"
			p.URLs[0].Canonical, p.URLs[0].Deleted = test.canonical, test.deleted
			if !test.sameProduct {
				p.URLs[0].ProductID = "another-product"
			}
			item, valid := parseProduct(p, time.Time{})
			if valid != test.valid || valid && item.URL != "https://www.yt-industries.com/Bikes/Enduro-Capra/CORE-2/CORE-2-AL/?number=103473" {
				t.Fatalf("URL=%q valid=%v", item.URL, valid)
			}
		})
	}
}

func TestMetadataAndInvalidEntries(t *testing.T) {
	for name, mutate := range map[string]func(*searchDocument){
		"negative total": func(d *searchDocument) { d.Total = new(-1) }, "missing total": func(d *searchDocument) { d.Total = nil },
		"query": func(d *searchDocument) { d.Filters.Search = "other" },
		"size":  func(d *searchDocument) { d.Limit = 12 }, "alias": func(d *searchDocument) { d.Alias = "error" },
		"count": func(d *searchDocument) { d.Elements = d.Elements[:1] },
	} {
		t.Run(name, func(t *testing.T) {
			doc := fixtureDocument(t)
			mutate(&doc)
			body, _ := json.Marshal(doc)
			_, err := parseSearch(provider.ResourceResponse{Body: body}, "capra", 1, "EUR")
			if !errors.Is(err, provider.ErrorCodeInvalidProviderResult) {
				t.Fatal(err)
			}
		})
	}
	doc := fixtureDocument(t)
	doc.Elements = []json.RawMessage{doc.Elements[0], doc.Elements[0], json.RawMessage(`{"id":false}`)}
	doc.Total = new(3)
	body, _ := json.Marshal(doc)
	result, err := parseSearch(provider.ResourceResponse{Body: body}, "capra", 1, "EUR")
	if err != nil || len(result.Items) != 1 || len(result.Warnings) != 1 || *result.Warnings[0].FoundCount != 3 || *result.Warnings[0].ParsedCount != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	doc.Elements = []json.RawMessage{json.RawMessage(`{"id":false}`)}
	doc.Total = new(1)
	body, _ = json.Marshal(doc)
	if _, err := parseSearch(provider.ResourceResponse{Body: body}, "capra", 1, "EUR"); !errors.Is(err, provider.ErrorCodeInvalidProviderResult) {
		t.Fatal(err)
	}
}
func TestContextMustMatchBeforeSearch(t *testing.T) {
	home, err := os.ReadFile("testdata/home.html")
	if err != nil {
		t.Fatal(err)
	}
	contextBody, err := os.ReadFile("testdata/context.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{}`, `null`, strings.ReplaceAll(string(contextBody), `"DE"`, `"US"`), strings.ReplaceAll(string(contextBody), `"EUR"`, `"USD"`), strings.ReplaceAll(string(contextBody), `"en-GB"`, `"de-DE"`)} {
		resources := conformance.NewFixtureService(conformance.ResourceFixture{Response: provider.ResourceResponse{StatusCode: 200, Body: home}}, conformance.ResourceFixture{Response: provider.ResourceResponse{StatusCode: 200, Body: []byte(body)}})
		_, err := (implementation{}).Search(t.Context(), provider.SearchRequest{Resources: resources, Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"}, Query: "capra"})
		if !errors.Is(err, provider.ErrorCodeInvalidProviderResult) {
			t.Fatal(err)
		}
		if calls, _ := resources.Stats(); calls != 2 {
			t.Fatal("search ran with incorrect context")
		}
	}
}
