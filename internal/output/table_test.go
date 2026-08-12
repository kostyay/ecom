package output

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
)

func TestTableGoldens(t *testing.T) {
	discount := 25
	total := 2
	next := false
	envelopes := map[string]Envelope{
		"listing": NewListing("bike-discount", testMarket, []provider.ProductSummary{
			{
				ID: "p-1", Name: "Vélo helmet", Brand: "Märké",
				Price:           &provider.Money{Amount: "79.95", Currency: "EUR", Display: "€79.95"},
				OriginalPrice:   &provider.Money{Amount: "99.95", Currency: "EUR", Display: "€99.95"},
				DiscountPercent: &discount, Availability: provider.AvailabilityInStock,
				URL: "https://example.test/p-1",
			},
			{
				ID: "p-2", Name: strings.Repeat("界", 50),
				PriceRange: &provider.PriceRange{
					Minimum: provider.Money{Amount: "10.00", Currency: "EUR", Display: "€10"},
					Maximum: provider.Money{Amount: "20.00", Currency: "EUR", Display: "€20"},
				},
				URL: "https://example.test/" + strings.Repeat("long/", 20),
			},
		}, provider.PageInfo{Number: 1, Size: 24, TotalItems: &total, HasNext: &next}, []provider.Warning{{Code: provider.WarningCodePartialParsing, Message: "one card failed", ItemID: "p-3"}}, nil, Metadata{Resources: []ResourceMetadata{{Cache: provider.CacheMetadata{Hit: true, Age: time.Hour, TTL: 24 * time.Hour}}}}),
		"item": NewItem("bike-discount", testMarket, provider.ItemResult{Item: provider.ItemDetail{
			ProductSummary: provider.ProductSummary{ID: "p-1", Name: "Trail bike", Brand: "Acme", Attributes: []provider.Attribute{{Name: "Frame", Value: "Carbon"}}, Variants: []provider.Variant{
				{ID: "m-red", Attributes: []provider.Attribute{{Name: "size", Value: "M"}, {Name: "color", Value: "red"}}, Price: &provider.Money{Amount: "999.00", Currency: "EUR", Display: "€999.00"}, Availability: provider.AvailabilityInStock, Selected: true},
				{ID: "l-blue", Attributes: []provider.Attribute{{Name: "size", Value: "L"}, {Name: "color", Value: "blue"}}, Availability: provider.AvailabilityOutOfStock},
			}}, Description: "A capable trail bike."},
		}, Metadata{}),
	}

	for name, envelope := range envelopes {
		t.Run(name, func(t *testing.T) {
			var got bytes.Buffer
			if err := Write(&got, envelope, Selection{Mode: ModeTable}); err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", name+".golden.table"))
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != string(want) {
				t.Errorf("table output does not match golden\ngot:\n%s\nwant:\n%s", got.String(), want)
			}
		})
	}
}

func TestTableRequiredResultTypes(t *testing.T) {
	help := provider.Help{
		Name: "bike-discount", DisplayName: "Bike-Discount", Description: "Bike products",
		Capabilities: []provider.CapabilityHelp{{Name: provider.CapabilitySearch, Supported: true, Description: "Find products"}},
		Search:       &provider.SearchHelp{QueryRequired: true, Syntax: "words", Examples: []string{"trail bike"}},
		Filters:      []provider.FilterDefinition{{Key: "brand", Type: provider.FilterTypeEnum, AllowedValues: []provider.FilterValue{{Value: "shimano", Label: "Shimano"}}, AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}}},
		SortModes:    []provider.SortMode{{Value: "price-asc", Label: "Lowest price"}},
		Pagination:   &provider.PaginationHelp{Mode: provider.PaginationPageNumber, FirstPage: 1, DefaultPageSize: 48, SupportedPageSizes: []int{24, 48}},
		Markets:      &provider.MarketRestrictions{Countries: []string{"DE"}, Languages: []string{"en"}, Currencies: []string{"EUR"}},
		Access:       &provider.AccessRequirements{Authentication: provider.AuthenticationNone, Browser: provider.BrowserFallback, SupportsCDP: true},
		Transport:    []provider.TransportNote{{Mode: provider.TransportHTTP, UseWhen: "first choice"}},
		Warnings:     []provider.HelpWarning{{Code: "challenge", Message: "Manual action can be necessary"}},
	}

	tests := []struct {
		name string
		data any
		want []string
	}{
		{"help", provider.HelpResult{Help: help}, []string{"Provider Help", "Capabilities", "Filters", "Sort modes", "Page sizes:", "Access notes"}},
		{"categories", ListingData[provider.Category]{Items: []provider.Category{{ID: "c1", Name: "Bikes", Path: "Sports / Bikes", HasChildren: true}}}, []string{"ID  NAME", "Sports / Bikes"}},
		{"brands", ListingData[provider.Brand]{Items: []provider.Brand{{ID: "b1", Name: "Shimano"}}}, []string{"ID  NAME", "Shimano"}},
		{"deals", ListingData[provider.Deal]{Items: []provider.Deal{{Product: provider.ProductSummary{ID: "d1", Name: "Deal", DiscountPercent: intPointer(10)}}}}, []string{"ID  NAME", "10%"}},
		{"filters", provider.FiltersResult{Filters: help.Filters, SortModes: help.SortModes}, []string{"Filters", "Sort modes", "brand"}},
		{"empty", ListingData[provider.ProductSummary]{Items: nil}, []string{"(no products)"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got bytes.Buffer
			if err := WriteTable(&got, New("bike-discount", testMarket, test.data, nil, Metadata{})); err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !strings.Contains(got.String(), want) {
					t.Errorf("output does not contain %q:\n%s", want, got.String())
				}
			}
		})
	}
}

func TestTableTruncationDoesNotChangeSource(t *testing.T) {
	name := strings.Repeat("界", 80)
	item := provider.ProductSummary{Name: name}
	envelope := NewListing("bike-discount", testMarket, []provider.ProductSummary{item}, provider.PageInfo{}, nil, nil, Metadata{})
	var output bytes.Buffer
	if err := WriteTable(&output, envelope); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "…") {
		t.Fatalf("long Unicode cell was not marked as truncated:\n%s", output.String())
	}
	if item.Name != name {
		t.Fatal("table rendering changed source data")
	}
}

func TestWriteDispatchAndFailures(t *testing.T) {
	envelope := New("bike-discount", testMarket, struct{}{}, nil, Metadata{})
	var jsonOutput bytes.Buffer
	if err := Write(&jsonOutput, envelope, Selection{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(jsonOutput.String(), `{"schema_version":"1"`) {
		t.Fatalf("default output is not JSON: %s", jsonOutput.String())
	}

	want := errors.New("write stopped")
	if err := WriteTable(failingWriter{err: want}, envelope); !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped write error", err)
	}
	var jsonPathOutput bytes.Buffer
	if err := Write(&jsonPathOutput, envelope, Selection{Mode: ModeJSONPath, Template: "{.provider}"}); err != nil {
		t.Fatal(err)
	}
	if jsonPathOutput.String() != "bike-discount" {
		t.Fatalf("JSONPath output = %q, want bike-discount", jsonPathOutput.String())
	}
	if err := Write(&bytes.Buffer{}, envelope, Selection{Mode: "yaml"}); err == nil {
		t.Fatal("unknown output mode was accepted")
	}
}
