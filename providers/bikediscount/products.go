package bikediscount

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/kostyay/ecom/provider"
	"golang.org/x/net/html"
)

const bikeDiscountBaseURL = "https://www.bike-discount.de"

var (
	pricePattern   = regexp.MustCompile(`[0-9]+(?:[., ][0-9]+)*`)
	percentPattern = regexp.MustCompile(`(?i)(?:-\s*|save\s+)([0-9]{1,3})\s*%`)
	itemIDPattern  = regexp.MustCompile(`(?i)\bitem\s*(?:no\.?|number)\s*:\s*([^\s]+)`)
	eurPattern     = regexp.MustCompile(`(?i)\bEUR\b`)
	gbpPattern     = regexp.MustCompile(`(?i)\bGBP\b`)
	usdPattern     = regexp.MustCompile(`(?i)\bUSD\b`)
)

// Central selectors contain only semantic HTML contracts and conservative
// class hints. Fixture marker attributes are deliberately not selectors.
var productExtractionSelectors = struct {
	containerTags       map[string]bool
	containerClasses    []string
	nameTags            map[string]bool
	linkTag             string
	currentPriceTag     string
	currentPriceClass   string
	originalPriceClass  string
	stockTags           map[string]bool
	imageTag            string
	canonicalTag        string
	originalPriceLabels []string
	discountLabels      []string
	brandHints          []string
	imageAttributes     []string
}{
	containerTags:       map[string]bool{"article": true},
	containerClasses:    []string{"product-box"},
	nameTags:            map[string]bool{"h1": true, "h2": true, "h3": true},
	linkTag:             "a",
	currentPriceTag:     "strong",
	currentPriceClass:   "product-price",
	originalPriceClass:  "list-price-price",
	stockTags:           map[string]bool{"p": true, "span": true, "div": true},
	imageTag:            "img",
	canonicalTag:        "link",
	originalPriceLabels: []string{"recommended retail price", "rrp", "original price"},
	discountLabels:      []string{"discount amount", "you save", "saving"},
	brandHints:          []string{"brand", "manufacturer"},
	imageAttributes:     []string{"src", "data-src", "data-original"},
}

type htmlNode struct {
	tag      string
	attrs    map[string]string
	text     string
	children []*htmlNode
}

type productCardInformation struct {
	CatalogID string `json:"id"`
	Name      string `json:"name"`
	Brand     string `json:"brand"`
}

// ProductExtraction contains the useful product cards and recoverable parse warnings.
type ProductExtraction struct {
	Products []provider.ProductSummary
	Warnings []provider.Warning
}

// ExtractProductSummaries parses product cards from one listing document.
// pageURL is used only to resolve links that are present in the document.
func ExtractProductSummaries(document []byte, pageURL string) (ProductExtraction, error) {
	root, err := parseHTML(document)
	if err != nil {
		return ProductExtraction{}, fmt.Errorf("parse listing HTML: %w", err)
	}
	return extractProductSummaries(root, pageURL), nil
}

func extractProductSummaries(root *htmlNode, pageURL string) ProductExtraction {
	canonical := canonicalURL(root, pageURL)
	containers := descendants(root, func(node *htmlNode) bool {
		return productExtractionSelectors.containerTags[node.tag] || hasAnyClass(node, productExtractionSelectors.containerClasses)
	})
	products := make([]provider.ProductSummary, 0, len(containers))
	for _, container := range containers {
		product, ok := extractProduct(container, pageURL, canonical, len(containers) == 1)
		if ok {
			products = append(products, product)
		}
	}

	result := ProductExtraction{Products: products}
	if len(products) != len(containers) {
		found, parsed := len(containers), len(products)
		warning := provider.NewWarning(
			provider.WarningCodePartialParsing,
			"Some product entries could not be parsed.",
			errors.New("one or more product entries do not contain a name"),
		)
		warning.FoundCount = &found
		warning.ParsedCount = &parsed
		result.Warnings = append(result.Warnings, warning)
	}
	return result
}

func extractListing(document []byte, pageURL string) (ProductExtraction, *bool, error) {
	root, err := parseHTML(document)
	if err != nil {
		return ProductExtraction{}, nil, fmt.Errorf("parse listing HTML: %w", err)
	}
	return extractProductSummaries(root, pageURL), listingHasNext(root), nil
}

func extractProduct(container *htmlNode, pageURL, canonical string, useCanonical bool) (provider.ProductSummary, bool) {
	information := extractProductCardInformation(container)
	link := firstDescendant(container, func(node *htmlNode) bool {
		return node.tag == productExtractionSelectors.linkTag && strings.TrimSpace(node.attrs["href"]) != "" && nodeText(node) != ""
	})
	name := information.Name
	productURL := ""
	if link != nil {
		if name == "" {
			name = nodeText(link)
		}
		productURL = absoluteURL(link.attrs["href"], pageURL)
	}
	if name == "" {
		nameNode := firstDescendant(container, func(node *htmlNode) bool {
			return productExtractionSelectors.nameTags[node.tag] && nodeText(node) != ""
		})
		if nameNode != nil {
			name = nodeText(nameNode)
		}
	}
	if name == "" {
		return provider.ProductSummary{}, false
	}
	if productURL == "" && useCanonical {
		productURL = canonical
	}

	brand := information.Brand
	if brand == "" {
		brand = extractBrand(container)
	}
	product := provider.ProductSummary{
		ID:           extractItemID(container),
		URL:          productURL,
		Name:         name,
		Brand:        brand,
		ImageURL:     extractImageURL(container, pageURL),
		DetailLevel:  provider.DetailLevelSummary,
		Availability: provider.AvailabilityUnknown,
	}
	if information.CatalogID != "" {
		encoded, _ := json.Marshal(struct {
			CatalogID string `json:"catalog_id"`
		}{CatalogID: information.CatalogID})
		product.ProviderData = provider.Data{Name: encoded}
	}
	product.Price, product.OriginalPrice, product.DiscountAmount = extractPrices(container)
	product.DiscountPercent = extractDiscountPercent(container)
	product.Availability, product.StockText = extractAvailability(container)
	return product, true
}

func extractProductCardInformation(container *htmlNode) productCardInformation {
	value := strings.TrimSpace(container.attrs["data-product-information"])
	if value == "" {
		return productCardInformation{}
	}
	var information productCardInformation
	if err := json.Unmarshal([]byte(value), &information); err != nil {
		return productCardInformation{}
	}
	information.CatalogID = normalizeText(information.CatalogID)
	information.Name = normalizeText(information.Name)
	information.Brand = normalizeText(information.Brand)
	return information
}

func extractPrices(container *htmlNode) (current, original, discount *provider.Money) {
	priceNodes := descendants(container, func(node *htmlNode) bool {
		return hasClass(node, productExtractionSelectors.originalPriceClass) || hasClass(node, productExtractionSelectors.currentPriceClass) ||
			node.tag == productExtractionSelectors.currentPriceTag || strings.TrimSpace(node.attrs["aria-label"]) != ""
	})
	for _, node := range priceNodes {
		label := strings.ToLower(strings.TrimSpace(node.attrs["aria-label"]))
		text := nodeText(node)
		if hasClass(node, productExtractionSelectors.originalPriceClass) && original == nil {
			original = parseSelectedMoney(text, false)
			continue
		}
		if hasClass(node, productExtractionSelectors.currentPriceClass) && current == nil {
			current = parseSelectedMoney(text, true)
			continue
		}
		money := parseMoney(text)
		if money == nil {
			continue
		}
		switch {
		case containsAny(label, productExtractionSelectors.originalPriceLabels):
			if original == nil {
				original = money
			}
		case containsAny(label, productExtractionSelectors.discountLabels):
			if discount == nil {
				discount = money
			}
		case node.tag == productExtractionSelectors.currentPriceTag && current == nil:
			current = money
		}
	}
	return current, original, discount
}

func parseSelectedMoney(text string, last bool) *provider.Money {
	display := normalizeText(text)
	matches := pricePattern.FindAllStringIndex(display, -1)
	if len(matches) == 0 {
		return nil
	}
	selected := matches[0]
	if last {
		selected = matches[len(matches)-1]
	}
	candidate := display[selected[0]:selected[1]]
	suffix := strings.TrimSpace(display[selected[1]:])
	for _, marker := range []string{"€", "£", "$", "EUR", "GBP", "USD"} {
		if strings.HasPrefix(suffix, marker) {
			return parseMoney(candidate + " " + marker)
		}
	}
	prefix := strings.TrimSpace(display[:selected[0]])
	for _, marker := range []string{"€", "£", "$"} {
		if strings.HasSuffix(prefix, marker) {
			return parseMoney(marker + candidate)
		}
	}
	return nil
}

func parseMoney(text string) *provider.Money {
	display := normalizeText(text)
	currency := ""
	switch {
	case strings.Contains(display, "€") || eurPattern.MatchString(display):
		currency = "EUR"
	case strings.Contains(display, "£") || gbpPattern.MatchString(display):
		currency = "GBP"
	case strings.Contains(display, "$") || usdPattern.MatchString(display):
		currency = "USD"
	}
	match := pricePattern.FindString(display)
	if match == "" || currency == "" {
		return nil
	}
	amount := normalizeAmount(match)
	money := &provider.Money{Amount: amount, Currency: currency, Display: display}
	if money.Validate() != nil {
		return nil
	}
	return money
}

func normalizeAmount(value string) string {
	value = strings.ReplaceAll(value, " ", "")
	comma, dot := strings.LastIndex(value, ","), strings.LastIndex(value, ".")
	switch {
	case comma >= 0 && dot >= 0:
		if comma > dot {
			value = strings.ReplaceAll(value, ".", "")
			value = strings.Replace(value, ",", ".", 1)
		} else {
			value = strings.ReplaceAll(value, ",", "")
		}
	case comma >= 0:
		value = strings.Replace(value, ",", ".", 1)
	}
	return value
}

func extractDiscountPercent(container *htmlNode) *int {
	for _, node := range descendants(container, func(node *htmlNode) bool { return nodeText(node) != "" }) {
		match := percentPattern.FindStringSubmatch(nodeText(node))
		if len(match) != 2 {
			continue
		}
		var value int
		if _, err := fmt.Sscanf(match[1], "%d", &value); err == nil && value <= 100 {
			return &value
		}
	}
	return nil
}

func extractAvailability(container *htmlNode) (provider.Availability, string) {
	for _, node := range descendants(container, func(node *htmlNode) bool {
		return productExtractionSelectors.stockTags[node.tag]
	}) {
		text := nodeText(node)
		lower := strings.ToLower(text)
		switch {
		case containsAny(lower, []string{"currently unavailable", "out of stock", "not available"}):
			return provider.AvailabilityOutOfStock, text
		case strings.Contains(lower, "pre-order") || strings.Contains(lower, "preorder"):
			return provider.AvailabilityPreorder, text
		case strings.Contains(lower, "in stock"):
			return provider.AvailabilityInStock, text
		}
	}
	return provider.AvailabilityUnknown, ""
}

func extractBrand(container *htmlNode) string {
	for _, node := range descendants(container, func(node *htmlNode) bool {
		return hasHint(node, productExtractionSelectors.brandHints)
	}) {
		if value := normalizeText(node.attrs["content"]); value != "" {
			return value
		}
		if value := nodeText(node); value != "" {
			return value
		}
	}
	return ""
}

func extractImageURL(container *htmlNode, pageURL string) string {
	image := firstDescendant(container, func(node *htmlNode) bool {
		return node.tag == productExtractionSelectors.imageTag
	})
	if image == nil {
		return ""
	}
	for _, attribute := range productExtractionSelectors.imageAttributes {
		if value := absoluteURL(image.attrs[attribute], pageURL); value != "" {
			return value
		}
	}
	return ""
}

func extractItemID(container *htmlNode) string {
	for _, key := range []string{"data-product-id", "data-product-number", "data-item-number"} {
		if value := normalizeText(container.attrs[key]); value != "" {
			return value
		}
	}
	sku := firstDescendant(container, func(node *htmlNode) bool {
		return node.tag == "input" && strings.EqualFold(strings.TrimSpace(node.attrs["name"]), "dtgs-gtm-product-sku")
	})
	if sku != nil {
		if value := normalizeText(sku.attrs["value"]); value != "" {
			return value
		}
	}
	match := itemIDPattern.FindStringSubmatch(nodeText(container))
	if len(match) == 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func canonicalURL(root *htmlNode, pageURL string) string {
	node := firstDescendant(root, func(node *htmlNode) bool {
		return node.tag == productExtractionSelectors.canonicalTag && strings.EqualFold(node.attrs["rel"], "canonical")
	})
	if node == nil {
		return ""
	}
	return absoluteURL(node.attrs["href"], pageURL)
}

func absoluteURL(reference, pageURL string) string {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return ""
	}
	base, err := url.Parse(pageURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		base, _ = url.Parse(bikeDiscountBaseURL)
	}
	parsed, err := url.Parse(reference)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	return resolved.String()
}

func parseHTML(document []byte) (*htmlNode, error) {
	documentRoot, err := html.Parse(bytes.NewReader(document))
	if err != nil {
		return nil, err
	}
	return convertHTMLNode(documentRoot), nil
}

func convertHTMLNode(source *html.Node) *htmlNode {
	node := &htmlNode{}
	switch source.Type {
	case html.ElementNode:
		node.tag = strings.ToLower(source.Data)
		node.attrs = make(map[string]string, len(source.Attr))
		for _, attribute := range source.Attr {
			node.attrs[strings.ToLower(attribute.Key)] = attribute.Val
		}
	case html.TextNode:
		node.text = source.Data
	}
	for child := source.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode && child.Type != html.TextNode && child.Type != html.DocumentNode {
			continue
		}
		node.children = append(node.children, convertHTMLNode(child))
	}
	return node
}

func descendants(root *htmlNode, match func(*htmlNode) bool) []*htmlNode {
	var result []*htmlNode
	var visit func(*htmlNode)
	visit = func(node *htmlNode) {
		if node != root && match(node) {
			result = append(result, node)
		}
		for _, child := range node.children {
			visit(child)
		}
	}
	visit(root)
	return result
}

func firstDescendant(root *htmlNode, match func(*htmlNode) bool) *htmlNode {
	for _, child := range root.children {
		if match(child) {
			return child
		}
		if found := firstDescendant(child, match); found != nil {
			return found
		}
	}
	return nil
}

func nodeText(node *htmlNode) string {
	var builder strings.Builder
	var appendText func(*htmlNode)
	appendText = func(current *htmlNode) {
		builder.WriteString(current.text)
		builder.WriteByte(' ')
		for _, child := range current.children {
			appendText(child)
		}
	}
	appendText(node)
	return normalizeText(builder.String())
}

func directText(node *htmlNode) string {
	var builder strings.Builder
	for _, child := range node.children {
		if child.tag == "" && child.text != "" {
			builder.WriteString(child.text)
			builder.WriteByte(' ')
		}
	}
	return normalizeText(builder.String())
}

func normalizeText(value string) string {
	return strings.Join(strings.FieldsFunc(value, unicode.IsSpace), " ")
}

func containsAny(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func hasHint(node *htmlNode, hints []string) bool {
	values := strings.ToLower(strings.Join([]string{
		node.attrs["class"], node.attrs["itemprop"], node.attrs["aria-label"], node.attrs["data-field"],
	}, " "))
	return containsAny(values, hints)
}

func hasClass(node *htmlNode, class string) bool {
	for _, value := range strings.Fields(node.attrs["class"]) {
		if value == class {
			return true
		}
	}
	return false
}

func hasAnyClass(node *htmlNode, classes []string) bool {
	for _, class := range classes {
		if hasClass(node, class) {
			return true
		}
	}
	return false
}
