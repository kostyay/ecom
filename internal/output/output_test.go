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

var testMarket = provider.Market{Country: "DE", Language: "en", Currency: "EUR"}

func TestEnvelopeGoldens(t *testing.T) {
	totalItems := 2
	totalPages := 1
	hasNext := false
	retrievedAt := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	price := &provider.Money{Amount: "79.95", Currency: "EUR", Display: "€79.95"}

	tests := []struct {
		name     string
		envelope Envelope
	}{
		{
			name: "listing",
			envelope: NewProductListing("bike-discount", testMarket, provider.ProductPage{
				Items: []provider.ProductSummary{{
					ID:          "p-1",
					URL:         "https://www.bike-discount.de/en/example",
					Name:        "Example helmet",
					Price:       price,
					RetrievedAt: retrievedAt,
					DetailLevel: provider.DetailLevelSummary,
				}},
				Page: provider.PageInfo{Number: 1, Size: 24, TotalItems: &totalItems, TotalPages: &totalPages, HasNext: &hasNext},
			}, Metadata{}),
		},
		{
			name: "item",
			envelope: NewItem("bike-discount", testMarket, provider.ItemResult{Item: provider.ItemDetail{
				ProductSummary: provider.ProductSummary{
					ID:          "p-1",
					Name:        "Example helmet",
					Price:       price,
					DetailLevel: provider.DetailLevelFull,
					Variants: []provider.Variant{
						{ID: "black-m", Attributes: []provider.Attribute{{Name: "color", Value: "black"}, {Name: "size", Value: "M"}}, Price: price, Availability: provider.AvailabilityInStock, Selected: true},
						{ID: "black-l", Attributes: []provider.Attribute{{Name: "color", Value: "black"}, {Name: "size", Value: "L"}}, Price: &provider.Money{Amount: "84.95", Currency: "EUR", Display: "€84.95"}, Availability: provider.AvailabilityOutOfStock},
					},
				},
				Description: "A full item.",
			}}, Metadata{}),
		},
		{
			name: "partial",
			envelope: NewProductListing("bike-discount", testMarket, provider.ProductPage{
				Items: []provider.ProductSummary{{ID: "p-1", Name: "Parsed item", DetailLevel: provider.DetailLevelSummary}},
				Page:  provider.PageInfo{Number: 1, Size: 24},
				Warnings: []provider.Warning{{
					Code:        provider.WarningCodePartialParsing,
					Message:     "one product card could not be parsed",
					URL:         "https://www.bike-discount.de/en/broken",
					FoundCount:  new(2),
					ParsedCount: new(1),
				}},
			}, Metadata{}),
		},
		{
			name: "cached",
			envelope: New("bike-discount", testMarket, struct {
				Status string `json:"status"`
			}{Status: "ready"}, nil, Metadata{Resources: []ResourceMetadata{
				{
					Cache:    provider.CacheMetadata{Hit: true, StoredAt: retrievedAt.Add(-2 * time.Hour), Age: 2 * time.Hour, TTL: 24 * time.Hour},
					Attempts: []provider.TransportAttempt{{Mode: provider.TransportHTTP, Outcome: provider.AttemptSucceeded, Duration: 125 * time.Millisecond}},
				},
				{
					Cache: provider.CacheMetadata{Hit: true, Stale: true, StoredAt: retrievedAt.Add(-26 * time.Hour), Age: 26 * time.Hour, TTL: 24 * time.Hour},
				},
			}}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := WriteJSON(&output, test.envelope); err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", test.name+".golden.json"))
			if err != nil {
				t.Fatal(err)
			}
			if output.String() != string(want) {
				t.Errorf("JSON output does not match %s golden\ngot:  %s\nwant: %s", test.name, output.String(), want)
			}
		})
	}
}

func TestEnvelopeOmitsOptionalFields(t *testing.T) {
	envelope := NewItem("bike-discount", testMarket, provider.ItemResult{Item: provider.ItemDetail{
		ProductSummary: provider.ProductSummary{ID: "p-1", Name: "Minimal", DetailLevel: provider.DetailLevelFull},
	}}, Metadata{})

	var output bytes.Buffer
	if err := WriteJSON(&output, envelope); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, absent := range []string{`"page"`, `"cache"`, `"transport_attempts"`, `"price"`, `"variants"`} {
		if strings.Contains(text, absent) {
			t.Errorf("output contains absent optional field %s: %s", absent, text)
		}
	}
	if !strings.Contains(text, `"warnings":[]`) {
		t.Errorf("output does not contain a stable empty warnings list: %s", text)
	}
}

func TestCacheAggregationForMixedResources(t *testing.T) {
	older := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	envelope := New("bike-discount", testMarket, struct{}{}, nil, Metadata{Resources: []ResourceMetadata{
		{Cache: provider.CacheMetadata{Hit: true, StoredAt: older, Age: 3 * time.Hour, TTL: 24 * time.Hour}},
		{Cache: provider.CacheMetadata{StoredAt: newer, Age: time.Hour, TTL: 12 * time.Hour}},
	}})

	if envelope.Cache == nil {
		t.Fatal("aggregate cache metadata is nil")
	}
	if envelope.Cache.Hit {
		t.Error("mixed cached and fresh resources were reported as a full cache hit")
	}
	if envelope.Cache.HitCount != 1 || envelope.Cache.ResourceCount != 2 {
		t.Errorf("cache counts = %d/%d, want 1/2", envelope.Cache.HitCount, envelope.Cache.ResourceCount)
	}
	if envelope.Cache.StoredAt == nil || !envelope.Cache.StoredAt.Equal(older) {
		t.Errorf("stored_at = %v, want oldest value %v", envelope.Cache.StoredAt, older)
	}
	if envelope.Cache.AgeSeconds != 3*60*60 {
		t.Errorf("age_seconds = %d, want 10800", envelope.Cache.AgeSeconds)
	}
	if envelope.Cache.TTLSeconds != 12*60*60 {
		t.Errorf("ttl_seconds = %d, want 43200", envelope.Cache.TTLSeconds)
	}
}

func TestWriteJSONReportsWriteFailure(t *testing.T) {
	want := errors.New("write stopped")
	err := WriteJSON(failingWriter{err: want}, New("bike-discount", testMarket, struct{}{}, nil, Metadata{}))
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped write error", err)
	}
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		value string
		want  Selection
	}{
		{"", Selection{Mode: ModeJSON}},
		{"json", Selection{Mode: ModeJSON}},
		{"table", Selection{Mode: ModeTable}},
		{`jsonpath={.data.items[*].price}`, Selection{Mode: ModeJSONPath, Template: `{.data.items[*].price}`}},
	}
	for _, test := range tests {
		got, err := ParseMode(test.value)
		if err != nil {
			t.Fatalf("ParseMode(%q): %v", test.value, err)
		}
		if got != test.want {
			t.Errorf("ParseMode(%q) = %#v, want %#v", test.value, got, test.want)
		}
	}
	if _, err := ParseMode("yaml"); err == nil {
		t.Fatal("ParseMode accepted an unsupported mode")
	}
	if _, err := ParseMode("jsonpath="); err == nil {
		t.Fatal("ParseMode accepted an empty JSONPath template")
	}
}

type failingWriter struct{ err error }

func (writer failingWriter) Write([]byte) (int, error) { return 0, writer.err }
