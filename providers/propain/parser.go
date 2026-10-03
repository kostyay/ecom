package propain

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/providers/internal/shoputil"
	"golang.org/x/net/html"
)

func parseSearch(response provider.ResourceResponse, page int, currency string) (provider.ProductPage, error) {
	root, err := html.Parse(bytes.NewReader(response.Body))
	if err != nil {
		return provider.ProductPage{}, shoputil.InvalidResult("Propain returned invalid HTML")
	}
	result := provider.ProductPage{Items: []provider.ProductSummary{}, Page: provider.PageInfo{Number: page, Size: pageSize, HasNext: new(false)}}
	grid := shoputil.FindClass(root, "products")
	if grid == nil {
		if shoputil.FindClass(root, "elementor-products-nothing-found") == nil {
			return provider.ProductPage{}, shoputil.InvalidResult("Propain did not return search results")
		}
		return shoputil.Finish(result, 0, currency)
	}
	nav := shoputil.FindClass(root, "woocommerce-pagination")
	if nav != nil {
		current, err := strconv.Atoi(shoputil.Text(shoputil.FindClass(nav, "current")))
		if err != nil || current != page {
			return provider.ProductPage{}, shoputil.InvalidResult("Propain returned a different search page")
		}
		next := shoputil.FindClass(nav, "next")
		if next != nil {
			expected := fmtPagePath(page + 1)
			if shoputil.ProductURL(shoputil.Attr(next, "href"), baseURL, expected) != baseURL+expected {
				return provider.ProductPage{}, shoputil.InvalidResult("Propain returned an invalid next page")
			}
			result.Page.HasNext = new(true)
		}
	} else if page != 1 {
		return provider.ProductPage{}, shoputil.InvalidResult("Propain omitted pagination metadata")
	}
	found := 0
	seen := make(map[string]bool)
	for node := range grid.Descendants() {
		if node.Data != "li" || !shoputil.HasClass(node, "product") {
			continue
		}
		found++
		item, ok := parseProduct(node, response.RetrievedAt)
		if !ok || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		result.Items = append(result.Items, item)
	}
	if found == 0 || found > pageSize || *result.Page.HasNext && found != pageSize {
		return provider.ProductPage{}, shoputil.InvalidResult("Propain returned an inconsistent product count")
	}
	return shoputil.Finish(result, found, currency)
}

func fmtPagePath(page int) string { return "/en/page/" + strconv.Itoa(page) + "/" }

func parseProduct(node *html.Node, stamp time.Time) (provider.ProductSummary, bool) {
	item := provider.ProductSummary{Name: shoputil.Text(shoputil.FindClass(node, "woocommerce-loop-product__title")), DetailLevel: provider.DetailLevelSummary, RetrievedAt: stamp, Availability: provider.AvailabilityUnknown}
	for _, class := range strings.Fields(shoputil.Attr(node, "class")) {
		if id, found := strings.CutPrefix(class, "post-"); found {
			if n, err := strconv.Atoi(id); err == nil && n > 0 {
				item.ID = id
			}
		}
	}
	link := shoputil.FindClass(node, "woocommerce-loop-product__link")
	item.URL = shoputil.ProductURL(shoputil.Attr(link, "href"), baseURL, "/en/product/")
	if item.ID == "" || item.Name == "" || item.URL == "" {
		return item, false
	}
	for child := range node.Descendants() {
		if child.Data == "img" && shoputil.HasClass(child, "attachment-woocommerce_thumbnail") {
			item.ImageURL = shoputil.Attr(child, "src")
			break
		}
	}
	priceNode := shoputil.FindClass(node, "price")
	if priceNode == nil {
		return item, false
	}
	var prices []*provider.Money
	for child := range priceNode.Descendants() {
		if !shoputil.HasClass(child, "woocommerce-Price-amount") {
			continue
		}
		price := shoputil.Euro(shoputil.Text(child))
		if price == nil {
			return item, false
		}
		original := false
		for parent := child.Parent; parent != nil && parent != priceNode; parent = parent.Parent {
			if parent.Data == "del" {
				original = true
			}
		}
		if original {
			item.OriginalPrice = price
		} else {
			prices = append(prices, price)
		}
	}
	switch len(prices) {
	case 1:
		item.Price = prices[0]
	case 2:
		item.PriceRange = &provider.PriceRange{Minimum: *prices[0], Maximum: *prices[1]}
	default:
		return item, false
	}
	if strings.HasPrefix(strings.ToLower(shoputil.Text(priceNode)), "from ") {
		item.ProviderData = provider.Data{Name: json.RawMessage(`{"starting_price":true}`)}
		if item.Price != nil {
			item.Price.Display = "from " + item.Price.Display
		}
	}
	switch {
	case shoputil.HasClass(node, "onbackorder"):
		item.Availability = provider.AvailabilityPreorder
	case shoputil.HasClass(node, "outofstock"):
		item.Availability = provider.AvailabilityOutOfStock
	case shoputil.HasClass(node, "instock"):
		item.Availability = provider.AvailabilityInStock
	}
	return item, item.Validate() == nil
}
