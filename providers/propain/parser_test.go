package propain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
	"golang.org/x/net/html"
)

func TestPriceRangeStartingPriceAndStock(t *testing.T) {
	for _, test := range []struct {
		name, price, stock, wantPrice, wantOriginal string
		wantRange, starting, valid                  bool
	}{
		{
			name: "sale", stock: "instock", wantPrice: "1999.00", wantOriginal: "2999.00", valid: true,
			price: `<del><span class="woocommerce-Price-amount">2.999,00 €</span></del><ins><span class="woocommerce-Price-amount">1.999,00 €</span></ins><span class="screen-reader-text">Original price 2.999,00 €</span>`,
		},
		{
			name: "starting", stock: "onbackorder", wantPrice: "1599.00", starting: true, valid: true,
			price: `from <ins><span class="woocommerce-Price-amount">1.599,00 €</span></ins>`,
		},
		{
			name: "range", stock: "outofstock", wantRange: true, valid: true,
			price: `<span class="woocommerce-Price-amount">1.599,00 €</span> – <span class="woocommerce-Price-amount">2.999,00 €</span>`,
		},
		{name: "bad price", stock: "instock", price: `<span class="woocommerce-Price-amount">secret</span>`},
		{name: "no price", stock: "instock"},
		{
			name: "inverted range", stock: "instock",
			price: `<span class="woocommerce-Price-amount">2.999,00 €</span> – <span class="woocommerce-Price-amount">1.599,00 €</span>`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := `<ul><li class="product post-123 ` + test.stock + `"><a class="woocommerce-loop-product__link" href="/en/product/bike/"><h2 class="woocommerce-loop-product__title">Bike</h2></a><p class="price">` + test.price + `</p></li></ul>`
			root, err := html.Parse(strings.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			var card *html.Node
			for n := range root.Descendants() {
				if n.Data == "li" {
					card = n
					break
				}
			}
			item, ok := parseProduct(card, time.Time{})
			if ok != test.valid {
				t.Fatalf("valid=%v item=%+v", ok, item)
			}
			if !ok {
				return
			}
			if test.wantPrice != "" && (item.Price == nil || item.Price.Amount != test.wantPrice) {
				t.Fatalf("price=%+v", item.Price)
			}
			if test.wantOriginal != "" && (item.OriginalPrice == nil || item.OriginalPrice.Amount != test.wantOriginal) {
				t.Fatalf("original=%+v", item.OriginalPrice)
			}
			if test.wantRange && (item.PriceRange == nil || item.PriceRange.Minimum.Amount != "1599.00" || item.PriceRange.Maximum.Amount != "2999.00") {
				t.Fatalf("range=%+v", item.PriceRange)
			}
			if test.starting && (string(item.ProviderData[Name]) != `{"starting_price":true}` || item.Price.Display != "from 1.599,00 €") {
				t.Fatalf("starting=%+v", item)
			}
			wantStock := map[string]provider.Availability{"instock": provider.AvailabilityInStock, "outofstock": provider.AvailabilityOutOfStock, "onbackorder": provider.AvailabilityPreorder}[test.stock]
			if item.Availability != wantStock {
				t.Fatalf("stock=%s", item.Availability)
			}
		})
	}
}
func TestEmptyGridAndInvalidCardsAreNotEmptySearches(t *testing.T) {
	for _, body := range []string{`<ul class="products"></ul>`, `<ul class="products"><li class="product post-1"></li></ul>`} {
		if _, err := parseSearch(provider.ResourceResponse{Body: []byte(body)}, 1, "EUR"); !errors.Is(err, provider.ErrorCodeInvalidProviderResult) {
			t.Fatal(err)
		}
	}
}
