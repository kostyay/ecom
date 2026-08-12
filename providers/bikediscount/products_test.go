package bikediscount

import (
	"os"
	"testing"

	"github.com/kostyay/ecom/provider"
)

func TestExtractProductSummariesFromFixtures(t *testing.T) {
	tests := []struct {
		name             string
		fixture          string
		pageURL          string
		wantName         string
		wantID           string
		wantURL          string
		wantPrice        string
		wantPriceDisplay string
		wantOriginal     string
		wantAvailability provider.Availability
		wantStock        string
	}{
		{
			name: "normal", fixture: "listing_page_2.html",
			pageURL:  "https://www.bike-discount.de/en/bike/sale?p=2&n=48",
			wantName: "Fixture page-two product", wantURL: "https://www.bike-discount.de/en/fixture-page-two-product",
			wantPrice: "49.99", wantPriceDisplay: "49,99 €", wantAvailability: provider.AvailabilityUnknown,
		},
		{
			name: "discounted", fixture: "listing_page_1.html",
			pageURL:   "https://www.bike-discount.de/en/bike/sale?p=1&n=48",
			wantName:  "Yamaha 500 Wh 36V/13.6Ah Frame Battery",
			wantURL:   "https://www.bike-discount.de/en/yamaha-500-wh-36v/13.6ah-frame-battery",
			wantPrice: "299.99", wantPriceDisplay: "299,99 €", wantOriginal: "819.00",
			wantAvailability: provider.AvailabilityUnknown,
		},
		{
			name: "unavailable", fixture: "item_unavailable.html",
			pageURL:  "https://www.bike-discount.de/en/fixture-unavailable-product",
			wantName: "Fixture unavailable product", wantID: "FIXTURE-UNAVAILABLE",
			wantURL:   "https://www.bike-discount.de/en/fixture-unavailable-product",
			wantPrice: "79.99", wantPriceDisplay: "79,99 €",
			wantAvailability: provider.AvailabilityOutOfStock, wantStock: "Currently unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := ExtractProductSummaries(readProductFixture(t, test.fixture), test.pageURL)
			if err != nil {
				t.Fatalf("extractProductSummaries() error = %v", err)
			}
			if len(result.Products) != 1 || len(result.Warnings) != 0 {
				t.Fatalf("result counts = %d products, %d warnings; want 1, 0", len(result.Products), len(result.Warnings))
			}
			product := result.Products[0]
			if product.Name != test.wantName || product.ID != test.wantID || product.URL != test.wantURL {
				t.Errorf("identity = %q, %q, %q; want %q, %q, %q", product.Name, product.ID, product.URL, test.wantName, test.wantID, test.wantURL)
			}
			if product.Price == nil || product.Price.Amount != test.wantPrice || product.Price.Display != test.wantPriceDisplay || product.Price.Currency != "EUR" {
				t.Errorf("price = %#v; want %s EUR with display %q", product.Price, test.wantPrice, test.wantPriceDisplay)
			}
			if test.wantOriginal == "" {
				if product.OriginalPrice != nil {
					t.Errorf("original price = %#v; want nil", product.OriginalPrice)
				}
			} else if product.OriginalPrice == nil || product.OriginalPrice.Amount != test.wantOriginal {
				t.Errorf("original price = %#v; want %s", product.OriginalPrice, test.wantOriginal)
			}
			if product.DiscountAmount != nil || product.DiscountPercent != nil {
				t.Errorf("unshown reductions were added: amount=%#v percent=%#v", product.DiscountAmount, product.DiscountPercent)
			}
			if product.Availability != test.wantAvailability || product.StockText != test.wantStock {
				t.Errorf("stock = %q, %q; want %q, %q", product.Availability, product.StockText, test.wantAvailability, test.wantStock)
			}
			if product.Brand != "" || product.ImageURL != "" {
				t.Errorf("missing fixture fields were inferred: brand=%q image=%q", product.Brand, product.ImageURL)
			}
			if product.DetailLevel != provider.DetailLevelSummary {
				t.Errorf("detail level = %q, want summary", product.DetailLevel)
			}
			if err := product.Validate(); err != nil {
				t.Errorf("ProductSummary.Validate() error = %v", err)
			}
		})
	}
}

func TestExtractProductSummariesReturnsPartialParsingWarning(t *testing.T) {
	result, err := ExtractProductSummaries(
		readProductFixture(t, "listing_partial.html"),
		"https://www.bike-discount.de/en/bike/sale",
	)
	if err != nil {
		t.Fatalf("extractProductSummaries() error = %v", err)
	}
	if len(result.Products) != 1 || result.Products[0].Name != "Yamaha 500 Wh 36V/13.6Ah Frame Battery" {
		t.Fatalf("products = %#v; want the valid partial product", result.Products)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %#v; want one warning", result.Warnings)
	}
	warning := result.Warnings[0]
	if warning.Code != provider.WarningCodePartialParsing || warning.FoundCount == nil || *warning.FoundCount != 2 || warning.ParsedCount == nil || *warning.ParsedCount != 1 {
		t.Errorf("warning = %#v; want partial_parsing with found=2 and parsed=1", warning)
	}
	if warning.Cause() == nil {
		t.Error("partial warning does not retain its internal cause")
	}
}

func TestExtractProductSummaryUsesOnlyExplicitOptionalFields(t *testing.T) {
	document := []byte(`<html><body><article data-product-number="P-12">
		<meta itemprop="brand" content="Example Brand">
		<h2>Example product</h2><img data-src="/media/product.jpg">
		<p aria-label="You save">20,00 €</p><p><strong>€79.99</strong></p><span>-20%</span>
	</article></body></html>`)
	result, err := ExtractProductSummaries(document, "https://www.bike-discount.de/en/list")
	if err != nil || len(result.Products) != 1 {
		t.Fatalf("extract result = %#v, %v", result, err)
	}
	product := result.Products[0]
	if product.ID != "P-12" || product.Brand != "Example Brand" || product.ImageURL != "https://www.bike-discount.de/media/product.jpg" {
		t.Errorf("optional fields = ID %q, brand %q, image %q", product.ID, product.Brand, product.ImageURL)
	}
	if product.DiscountAmount == nil || product.DiscountAmount.Amount != "20.00" || product.DiscountPercent == nil || *product.DiscountPercent != 20 {
		t.Errorf("explicit discounts = %#v, %#v", product.DiscountAmount, product.DiscountPercent)
	}
	if product.Price == nil || product.Price.Amount != "79.99" || product.Price.Display != "€79.99" {
		t.Errorf("current price = %#v; want exact €79.99 display", product.Price)
	}
}

func readProductFixture(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile("testdata/catalog/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
