package canyon

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/providers/internal/shoputil"
	"golang.org/x/net/html"
)

var productPathPattern = regexp.MustCompile(`^/en-es/.+/([0-9]+)\.html$`)

func parseSearch(response provider.ResourceResponse, page int, currency string) (provider.ProductPage, error) {
	root, err := html.Parse(bytes.NewReader(response.Body))
	if err != nil {
		return provider.ProductPage{}, shoputil.InvalidResult("Canyon returned invalid HTML")
	}
	result := provider.ProductPage{Items: []provider.ProductSummary{}, Page: provider.PageInfo{Number: page, Size: pageSize, HasNext: new(false)}}
	grid := shoputil.FindClass(root, "js-productGrid")
	if grid == nil {
		if page != 1 || shoputil.FindClass(root, "js-noResults-search") == nil {
			return provider.ProductPage{}, shoputil.InvalidResult("Canyon did not return bike search results")
		}
		result.Page.TotalItems, result.Page.TotalPages = new(0), new(0)
		return shoputil.Finish(result, 0, currency)
	}
	tab := shoputil.FindClass(root, "js-productSearchTabSelected")
	total, err := strconv.Atoi(shoputil.Attr(tab, "data-count"))
	if err != nil || total <= 0 || shoputil.Attr(tab, "data-search-tab-type") != "bikes" {
		return provider.ProductPage{}, shoputil.InvalidResult("Canyon returned invalid bike search metadata")
	}
	footer := shoputil.FindClass(root, "js-grid-footer")
	number, err := strconv.Atoi(shoputil.Attr(footer, "data-page-number"))
	size := shoputil.Attr(footer, "data-page-size")
	if err != nil || number != page-1 || size != "24.0" && size != "24" {
		return provider.ProductPage{}, shoputil.InvalidResult("Canyon returned a different search page")
	}
	totalPages := total / pageSize
	if total%pageSize != 0 {
		totalPages++
	}
	next := shoputil.FindClass(footer, "js-showMoreGrid")
	if (next != nil) != (page < totalPages) {
		return provider.ProductPage{}, shoputil.InvalidResult("Canyon returned inconsistent pagination")
	}
	if next != nil && !validNextPageURL(shoputil.Attr(next, "data-url"), page) {
		return provider.ProductPage{}, shoputil.InvalidResult("Canyon returned an invalid next page")
	}
	result.Page.TotalItems, result.Page.TotalPages, result.Page.HasNext = new(total), new(totalPages), new(next != nil)
	found := 0
	seen := make(map[string]bool)
	for node := range grid.Descendants() {
		if !shoputil.HasClass(node, "js-productTileWrapper") {
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
	if found != min(pageSize, max(0, total-(page-1)*pageSize)) {
		return provider.ProductPage{}, shoputil.InvalidResult("Canyon returned an inconsistent product count")
	}
	return shoputil.Finish(result, found, currency)
}

func validNextPageURL(value string, currentPage int) bool {
	link, err := url.Parse(value)
	if err != nil || link.Scheme != "https" || link.Host != "www.canyon.com" || link.Path != "/en-es/shop/" || link.User != nil {
		return false
	}
	query := link.Query()
	return query.Get("start") == strconv.Itoa(currentPage*pageSize) && query.Get("sz") == "24" && query.Get("searchType") == "bikes"
}

func parseProduct(node *html.Node, stamp time.Time) (provider.ProductSummary, bool) {
	link := shoputil.FindClass(node, "productTileDefault__productName")
	item := provider.ProductSummary{
		URL: shoputil.ProductURL(shoputil.Attr(link, "href"), baseURL, "/en-es/"), Name: shoputil.Text(link), Brand: "Canyon",
		DetailLevel: provider.DetailLevelSummary, RetrievedAt: stamp, Availability: provider.AvailabilityUnknown,
	}
	parsed, err := url.Parse(item.URL)
	if err != nil || item.Name == "" {
		return item, false
	}
	match := productPathPattern.FindStringSubmatch(parsed.Path)
	if match == nil {
		return item, false
	}
	item.ID = match[1]
	item.ImageURL = shoputil.Attr(shoputil.FindClass(node, "productTileDefault__image"), "src")
	item.Price = shoputil.Euro(shoputil.Text(shoputil.FindClass(node, "productTile__priceSale")))
	if item.Price == nil {
		return item, false
	}
	if strings.HasPrefix(strings.ToLower(item.Price.Display), "from ") {
		item.ProviderData = provider.Data{Name: json.RawMessage(`{"starting_price":true}`)}
	}
	if original := shoputil.FindClass(node, "productTile__priceOriginal"); original != nil {
		item.OriginalPrice = shoputil.Euro(shoputil.Text(original))
		if item.OriginalPrice == nil {
			return item, false
		}
	}
	for _, text := range strings.Split(shoputil.Attr(link, "aria-label"), ",") {
		switch strings.TrimSpace(text) {
		case "Out of stock":
			item.Availability, item.StockText = provider.AvailabilityOutOfStock, "Out of stock"
		case "In stock":
			item.Availability, item.StockText = provider.AvailabilityInStock, "In stock"
		}
	}
	return item, item.Validate() == nil
}
