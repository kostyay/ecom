package buscocotxe

import (
	json "encoding/json/v2"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
)

func TestListingFixtures(t *testing.T) {
	var manifest struct {
		Fixtures []struct {
			ID       string `json:"id"`
			Path     string `json:"path"`
			Expected struct {
				IDs           []string           `json:"ids"`
				CardCount     int                `json:"card_count"`
				Number        int                `json:"number"`
				TotalItems    int                `json:"total_items"`
				TotalPages    int                `json:"total_pages"`
				HasNext       bool               `json:"has_next"`
				SoldIDs       []string           `json:"sold_ids"`
				MissingPrices []string           `json:"price_on_request_ids"`
				Prices        map[string]*string `json:"prices"`
				Error         string             `json:"error"`
				FirstItem     struct {
					ID           string `json:"id"`
					URL          string `json:"url"`
					Name         string `json:"name"`
					PriceDisplay string `json:"price_display"`
				} `json:"first_item"`
			} `json:"expected"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal(readFixture(t, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	retrieved := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	for _, fixture := range manifest.Fixtures {
		t.Run(fixture.ID, func(t *testing.T) {
			page, err := parseListing(readFixture(t, fixture.Path), retrieved)
			if fixture.Expected.Error != "" {
				if !errors.Is(err, provider.ErrorCodeInvalidProviderResult) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if fixture.ID == "regressions" {
				if len(page.Items) != 3 || len(page.Warnings) != 1 {
					t.Fatalf("result = %+v", page)
				}
				warning := page.Warnings[0]
				if warning.Code != provider.WarningCodePartialParsing || *warning.FoundCount != 4 || *warning.ParsedCount != 3 {
					t.Fatalf("warning = %+v", warning)
				}
				for _, item := range page.Items {
					amount, ok := fixture.Expected.Prices[item.ID]
					if !ok {
						t.Fatalf("unexpected item %s", item.ID)
					}
					if amount == nil {
						if item.Price != nil {
							t.Fatalf("description became price: %+v", item.Price)
						}
					} else if item.Price == nil || item.Price.Amount != *amount {
						t.Fatalf("price = %+v, want %s", item.Price, *amount)
					}
				}
				return
			}
			if len(page.Items) != fixture.Expected.CardCount || len(page.Warnings) != 0 {
				t.Fatalf("count=%d warnings=%+v", len(page.Items), page.Warnings)
			}
			if page.Page.Number != fixture.Expected.Number || page.Page.Size != 30 || *page.Page.TotalItems != fixture.Expected.TotalItems || *page.Page.HasNext != fixture.Expected.HasNext {
				t.Fatalf("page = %+v", page.Page)
			}
			if fixture.Expected.TotalPages != 0 && (page.Page.TotalPages == nil || *page.Page.TotalPages != fixture.Expected.TotalPages) {
				t.Fatalf("total pages = %v", page.Page.TotalPages)
			}
			if fixture.ID == "search_empty" && page.Page.TotalPages != nil {
				t.Fatalf("empty page has an invented page count: %+v", page.Page)
			}
			for i, item := range page.Items {
				if err := item.Validate(); err != nil {
					t.Fatal(err)
				}
				if item.ID != fixture.Expected.IDs[i] || item.RetrievedAt != retrieved || item.DetailLevel != provider.DetailLevelSummary {
					t.Fatalf("item = %+v", item)
				}
				if slices.Contains(fixture.Expected.MissingPrices, item.ID) && item.Price != nil {
					t.Fatalf("missing price: %+v", item)
				}
				if slices.Contains(fixture.Expected.SoldIDs, item.ID) && (item.Price != nil || item.Availability != provider.AvailabilityOutOfStock || !strings.EqualFold(item.StockText, "VENUT")) {
					t.Fatalf("sold item = %+v", item)
				}
			}
			if len(page.Items) > 0 {
				item, first := page.Items[0], fixture.Expected.FirstItem
				if item.ID != first.ID || item.Name != first.Name || item.URL != first.URL {
					t.Fatalf("first item = %+v", item)
				}
				if strings.Contains(first.PriceDisplay, "€") && (item.Price == nil || item.Price.Display != first.PriceDisplay) {
					t.Fatalf("first price = %+v", item.Price)
				}
			}
		})
	}
}

func TestListingAttributes(t *testing.T) {
	page, err := parseListing(readFixture(t, "carfinder_page_1.html"), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	item := page.Items[0]
	for _, attribute := range []provider.Attribute{{Name: "kilometers", Value: "62.000 km"}, {Name: "year", Value: "2021"}, {Name: "body_type", Value: "4x4, Suv, Pickup"}} {
		if !slices.Contains(item.Attributes, attribute) {
			t.Fatalf("attributes = %+v", item.Attributes)
		}
	}
	if item.Availability != provider.AvailabilityInStock || item.StockText != "Estoc" || !strings.Contains(item.ImageURL, "/lib/thumbkmk.php?src=") || item.Brand != "" {
		t.Fatalf("item = %+v", item)
	}
}

func TestDisplayedMoney(t *testing.T) {
	for display, amount := range map[string]string{"0 €": "0", "9 €": "9", "19.999 €": "19999", "19.999,50 €": "19999.50", "19999,05 €": "19999.05", "0,50 €": "0.50"} {
		price, err := parsePrice(display)
		if err != nil || price.Amount != amount || price.Currency != "EUR" || price.Display != display {
			t.Errorf("%s: price=%+v error=%v", display, price, err)
		}
	}
	for _, display := range []string{"-1 €", "19.99 €", "01 €", "19,5 €", "19,999 €", "250 €/mes", "1.000 USD", "1e3 €", "1 € 2 €"} {
		if _, err := parsePrice(display); err == nil {
			t.Errorf("accepted %q", display)
		}
	}
}

func TestListingInvalidAndMissingFields(t *testing.T) {
	valid := `<a href="https://www.buscocotxe.ad/ca/cotxe/123/car" kmk-seguiment="llistat" kmk-seguiment-iditem="123"><div class="box-titol"><h2>Car</h2></div><div class="box-overlay"><div class="uk-float-right">19.999,50 €</div></div><p><em>12.950 Km. 11/2020 Estoc Estranger</em>Old price 80.000 €</p></a>`
	metadata := `<p><strong>1</strong> resultats. Mostrant pàgina <strong>1</strong> de <strong>1</strong>.</p>`
	for name, card := range map[string]string{
		"foreign host":  strings.Replace(valid, "www.buscocotxe.ad", "evil.example", 1),
		"credentials":   strings.Replace(valid, "https://", "https://secret@", 1),
		"port":          strings.Replace(valid, "www.buscocotxe.ad", "www.buscocotxe.ad:444", 1),
		"scheme":        strings.Replace(valid, "https:", "javascript:", 1),
		"id mismatch":   strings.Replace(valid, `iditem="123"`, `iditem="124"`, 1),
		"encoded slash": strings.Replace(valid, "/123/car", "/123/car%2Fextra", 1),
		"missing title": strings.Replace(valid, "<h2>Car</h2>", "", 1),
		"invalid price": strings.Replace(valid, "19.999,50 €", "-1 €", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseListing([]byte(card+metadata), time.Time{})
			if !errors.Is(err, provider.ErrorCodeInvalidProviderResult) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error = %v", err)
			}
		})
	}
	for _, document := range []string{"", "<html><script>1 resultats. Mostrant pàgina 1 de 1.</script></html>", valid, valid + strings.Replace(metadata, "de <strong>1", "de <strong>2", 1), valid + `<p>999999999999999999999999 resultats. Mostrant pàgina 1 de 1.</p>`, strings.Replace(valid, "Old price 80.000 €", metadata, 1)} {
		if _, err := parseListing([]byte(document), time.Time{}); err == nil {
			t.Fatalf("accepted invalid page %q", document)
		}
	}
	page, err := parseListing([]byte(valid+metadata), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].ImageURL != "" || page.Items[0].StockText != "Estoc Estranger" || !slices.Contains(page.Items[0].Attributes, provider.Attribute{Name: "year", Value: "11/2020"}) {
		t.Fatalf("item=%+v", page.Items[0])
	}
	minimal := `<a href="/ca/cotxe/123/car" kmk-seguiment="llistat"><div class="box-titol"><h2>Car</h2></div></a>`
	page, err = parseListing([]byte(minimal+metadata), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].Price != nil || page.Items[0].Availability != "" || len(page.Items[0].Attributes) != 0 {
		t.Fatalf("invented optional data: %+v", page.Items[0])
	}
	for _, image := range []struct{ source, want string }{
		{"/uploads/fotos_items/123/car.jpg", "https://www.buscocotxe.ad/uploads/fotos_items/123/car.jpg"},
		{"https://other.example/car.jpg", ""},
		{"javascript:alert", ""},
		{"https://secret@www.buscocotxe.ad/car.jpg", ""},
		{"/img/buscocotxe.ad/nopic.svg", ""},
	} {
		imageNode := `<div class="box-imatge" style="background-image: url('` + image.source + `')"></div>`
		document := strings.Replace(minimal, "</a>", imageNode+"</a>", 1) + metadata
		page, err := parseListing([]byte(document), time.Time{})
		if err != nil || page.Items[0].ImageURL != image.want {
			t.Fatalf("image %q: result=%+v error=%v", image.source, page, err)
		}
	}
	page, err = parseListing([]byte(valid+valid+metadata), time.Time{})
	if err != nil || len(page.Items) != 1 || len(page.Warnings) != 0 {
		t.Fatalf("duplicate result = %+v, %v", page, err)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
