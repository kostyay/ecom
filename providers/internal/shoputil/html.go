package shoputil

import (
	"regexp"
	"strings"

	"github.com/kostyay/ecom/provider"
	"golang.org/x/net/html"
)

// Attr returns an HTML attribute, or an empty string for a missing node or attribute.
func Attr(n *html.Node, key string) string {
	if n != nil {
		for _, a := range n.Attr {
			if a.Key == key {
				return a.Val
			}
		}
	}
	return ""
}

// HasClass matches complete CSS class names.
func HasClass(n *html.Node, class string) bool {
	for value := range strings.FieldsSeq(Attr(n, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

// FindClass returns the first descendant with a CSS class.
func FindClass(n *html.Node, class string) *html.Node {
	if n != nil {
		for child := range n.Descendants() {
			if HasClass(child, class) {
				return child
			}
		}
	}
	return nil
}

// Text returns normalized visible text, excluding scripts and accessibility duplicates.
func Text(n *html.Node) string {
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil || node.Data == "script" || node.Data == "style" || HasClass(node, "screen-reader-text") {
			return
		}
		if node.Type == html.TextNode {
			out.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(out.String()), " ")
}

var euroPattern = regexp.MustCompile(`^(?:[Ff]rom\s+)?((?:[0-9]{1,3}(?:\.[0-9]{3})+|[0-9]+)(?:,[0-9]{2})?)\s*(?:€|EUR)$`)

// Euro parses the German/Spanish storefront format without binary floating point.
func Euro(display string) *provider.Money {
	display = strings.Join(strings.Fields(display), " ")
	match := euroPattern.FindStringSubmatch(display)
	if match == nil {
		return nil
	}
	amount := strings.ReplaceAll(strings.ReplaceAll(match[1], ".", ""), ",", ".")
	if !strings.Contains(amount, ".") {
		amount += ".00"
	}
	price := &provider.Money{Amount: amount, Currency: "EUR", Display: display}
	if price.Validate() != nil {
		return nil
	}
	return price
}
