package wallapop

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/provider/conformance"
)

func TestProviderConformanceOffline(t *testing.T) {
	resources := conformance.NewFixtureService(
		fixture(t, "components.json", apiBaseURL+"/components"),
		fixture(t, "section.json", apiBaseURL+"/section"),
		fixture(t, "item.html", websiteBaseURL+"/item/open-up-200"),
	)
	request := provider.Request{
		Market:    provider.Market{Country: "AD", Language: "en", Currency: "EUR"},
		Resources: resources,
	}
	conformance.Run(t, conformance.Suite{
		Registration: registration(), Resources: resources,
		Cases: []conformance.OperationCase{
			{
				Name: "public listing search", Capability: provider.CapabilitySearch,
				Invoke: func(ctx context.Context, registered provider.Provider) (any, error) {
					return registered.Search(ctx, provider.SearchRequest{Request: request, Query: "gravel"})
				},
				Check: func(value any) error {
					page := value.(provider.ProductPage)
					if len(page.Items) != 2 || page.Items[0].ID != "trek-checkpoint-alr-4-100" || page.Items[0].Price.Amount != "875" {
						return fmt.Errorf("items = %#v", page.Items)
					}
					return nil
				},
			},
			{
				Name: "public item page", Capability: provider.CapabilityItem,
				Invoke: func(ctx context.Context, registered provider.Provider) (any, error) {
					return registered.Item(ctx, provider.ItemRequest{Request: request, IDOrURL: "open-up-200"})
				},
				Check: func(value any) error {
					item := value.(provider.ItemResult).Item
					if item.Name != "OPEN U.P." || item.Description != "Carbon gravel bike." || item.Price.Amount != "1800" {
						return fmt.Errorf("item = %#v", item)
					}
					return nil
				},
			},
		},
	})
}

func TestSearchOptionsAndDistanceFilter(t *testing.T) {
	options, err := parseSearchOptions([]provider.Filter{
		{Key: "latitude", Value: "42.5063"}, {Key: "longitude", Value: "1.5218"},
		{Key: "max_distance_km", Value: "100"}, {Key: "max_price", Value: "1000"},
	}, &provider.Sort{Value: "closest"})
	if err != nil {
		t.Fatal(err)
	}
	if options.orderBy != "closest" || options.maxDistance == nil || *options.maxPrice != 1000 {
		t.Fatalf("options = %#v", options)
	}
	if distance := haversine(options.latitude, options.longitude, 42.511, 1.547); distance < 1 || distance > 3 {
		t.Fatalf("distance = %f km", distance)
	}
	var section sectionDocument
	if err := json.Unmarshal(readFixture(t, "section.json"), &section); err != nil {
		t.Fatal(err)
	}
	if _, err := section.Data.Section.Items[0].summaryAt(time.Time{}, options); err != nil {
		t.Fatalf("near listing was filtered: %v", err)
	}
	if _, err := section.Data.Section.Items[1].summaryAt(time.Time{}, options); !errors.Is(err, errFiltered) {
		t.Fatalf("expensive listing error = %v, want filtered", err)
	}
	if _, err := parseSearchOptions([]provider.Filter{{Key: "latitude", Value: "42"}}, nil); err == nil {
		t.Fatal("latitude without longitude did not fail")
	}
}

func TestSearchPartialParsingCountsOnlyMalformedListings(t *testing.T) {
	resources := conformance.NewFixtureService(
		fixture(t, "components.json", apiBaseURL+"/components"),
		conformance.ResourceFixture{Response: provider.ResourceResponse{StatusCode: http.StatusOK, Body: []byte(`{
			"data":{"section":{"items":[
				{"title":"Good","price":{"amount":1,"currency":"EUR"},"web_slug":"good","location":{}},
				{"reserved":true},
				{"title":"Broken","price":{"amount":1,"currency":"EUR"},"location":{}}
			]}},"meta":{}
		}`)}},
	)
	page, err := (implementation{}).Search(t.Context(), provider.SearchRequest{
		Request: provider.Request{Market: provider.Market{Country: "AD", Language: "en", Currency: "EUR"}, Resources: resources},
		Query:   "bike",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(page.Warnings) != 1 {
		t.Fatalf("page = %#v", page)
	}
	warning := page.Warnings[0]
	if warning.FoundCount == nil || *warning.FoundCount != 3 || warning.ParsedCount == nil || *warning.ParsedCount != 2 {
		t.Fatalf("warning = %#v", warning)
	}
}

func TestParsePageProps(t *testing.T) {
	tests := []struct {
		name     string
		document string
		wantErr  bool
	}{
		{name: "reversed attributes", document: `<html><body>__NEXT_DATA__<script type="application/json" id="__NEXT_DATA__">{"props":{"pageProps":{"item":{"slug":"bike"}}}}</script></body></html>`},
		{name: "missing script", document: `<html><body>__NEXT_DATA__</body></html>`, wantErr: true},
		{name: "invalid JSON", document: `<script id="__NEXT_DATA__">{</script>`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			props, err := parsePageProps([]byte(test.document))
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, want error %t", err, test.wantErr)
			}
			if !test.wantErr && props.Item.Slug != "bike" {
				t.Fatalf("slug = %q", props.Item.Slug)
			}
		})
	}
}

func fixture(t *testing.T, name, wantURL string) conformance.ResourceFixture {
	t.Helper()
	return conformance.ResourceFixture{
		Response: provider.ResourceResponse{Body: readFixture(t, name), StatusCode: http.StatusOK},
		CheckRequest: func(request provider.ResourceRequest) error {
			if request.Method != http.MethodGet || request.URL != wantURL || request.Transport.Required != provider.TransportHTTP {
				return fmt.Errorf("resource = %#v", request)
			}
			return nil
		},
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}
