package shoputil

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestEuro(t *testing.T) {
	for _, test := range []struct{ display, amount, wantDisplay string }{
		{"2.699 €", "2699.00", "2.699 €"},
		{"5.189,00\u00a0€", "5189.00", "5.189,00 €"},
		{"9,95 EUR", "9.95", "9,95 EUR"},
		{"From 3.799 €", "3799.00", "From 3.799 €"},
		{"from 1.599,00 €", "1599.00", "from 1.599,00 €"},
		{"0,00 €", "0.00", "0,00 €"},
	} {
		price := Euro(test.display)
		if price == nil || price.Amount != test.amount || price.Display != test.wantDisplay {
			t.Fatalf("%q: %+v", test.display, price)
		}
	}
	for _, value := range []string{"", "1e3 €", "NaN €", "-2,00 €", "2.69 €", "12.34,00 €", "9,9 €", "2.699 USD", "or from 45 €/Mo.", "1.000 – 2.000 €", "99", "EUR 19,99", "00,00 €"} {
		if price := Euro(value); price != nil {
			t.Fatalf("accepted %q: %+v", value, price)
		}
	}
}
func TestOwnedProductURL(t *testing.T) {
	for _, value := range []string{"https://evil.example/en/product/bike/", "//evil.example/en/product/bike/", "https://www.shop.example.evil/en/product/bike/", "https://secret@www.shop.example/en/product/bike/", "http://www.shop.example/en/product/bike/", "https://www.shop.example:444/en/product/bike/", "javascript:alert(1)", "/en/other/bike/", ""} {
		if got := ProductURL(value, "https://www.shop.example", "/en/product/"); got != "" {
			t.Fatalf("accepted %q: %q", value, got)
		}
	}
	if got := ProductURL("/en/product/bike/?tracking=secret#color", "https://www.shop.example", "/en/product/"); got != "https://www.shop.example/en/product/bike/" {
		t.Fatal(got)
	}
}
func BenchmarkHasClass(b *testing.B) {
	node := &html.Node{Attr: []html.Attribute{{Key: "class", Val: "js-gtmTileWrapper js-inlineAddToCartFeedback js-productTileWrapper productTileDefault productTileDefault--bike"}}}
	b.ReportAllocs()
	for b.Loop() {
		HasClass(node, "js-productTileWrapper")
	}
}

func TestHTMLHelpers(t *testing.T) {
	root, err := html.Parse(strings.NewReader(`<p class="one
 two"><span>1.299,00&nbsp;<b>€</b></span><script>secret</script><span class="screen-reader-text">duplicate</span></p>`))
	if err != nil {
		t.Fatal(err)
	}
	node := FindClass(root, "two")
	if node == nil || Text(node) != "1.299,00 €" || HasClass(node, "on") {
		t.Fatalf("node=%v text=%q", node, Text(node))
	}
	if Text(nil) != "" || FindClass(nil, "x") != nil || Attr(nil, "x") != "" {
		t.Fatal("nil helpers failed")
	}
}
