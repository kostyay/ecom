// Package buscocotxe implements public BuscoCotxe car search.
// Import this package for its registration side effect.
package buscocotxe

import (
	"bytes"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kostyay/ecom/provider"
	"golang.org/x/net/html"
)

const (
	pageSize    = 30
	websiteHost = "www.buscocotxe.ad"
)

var (
	carPath      = regexp.MustCompile(`^/ca/cotxe/([1-9][0-9]*)/[^/]+$`)
	pricePattern = regexp.MustCompile(`^(0|[1-9][0-9]*|[1-9][0-9]{0,2}(?:\.[0-9]{3})+)(,[0-9]{2})?\s*€$`)
	pagePattern  = regexp.MustCompile(`^([1-9][0-9]*(?:\.[0-9]{3})*) resultats\. Mostrant pàgina ([1-9][0-9]*) de ([1-9][0-9]*)\s*\.$`)
	kmPattern    = regexp.MustCompile(`(?i)\b([0-9]+(?:\.[0-9]{3})*)\s*Km\.`)
	yearPattern  = regexp.MustCompile(`\b(?:[0-9]{1,2}/)?(?:19|20)[0-9]{2}\b`)
	stockPattern = regexp.MustCompile(`(?i)\b(Estoc Estranger|Estoc|Venut|Reservat|Sota comanda|En fabricació)$`)
	imagePattern = regexp.MustCompile(`url\(\s*(?:'([^']+)'|"([^"]+)"|([^\s)]+))\s*\)`)
)

// parseListing returns the actual page number. Search must compare it with the
// requested page, because the site can silently return its last page instead.
// Explicit empty results have page number zero and no total page count because
// the website omits those fields.
func parseListing(document []byte, retrievedAt time.Time) (provider.ProductPage, error) {
	root, err := html.Parse(bytes.NewReader(document))
	if err != nil {
		return provider.ProductPage{}, parseError()
	}
	result := provider.ProductPage{Items: []provider.ProductSummary{}}
	var cards []*html.Node
	empty := false
	for node := range root.Descendants() {
		if node.Type != html.ElementNode || inCard(node.Parent) {
			continue
		}
		if isCard(node) {
			cards = append(cards, node)
			continue
		}
		if node.Data == "h2" && nodeText(node) == "No s'han trobat resultats." && hasClass(node.Parent, "uk-alert-danger") {
			empty = true
		}
		if node.Data != "li" && node.Data != "p" {
			continue
		}
		match := pagePattern.FindStringSubmatch(nodeText(node))
		if match == nil {
			continue
		}
		total, totalErr := strconv.Atoi(strings.ReplaceAll(match[1], ".", ""))
		number, numberErr := strconv.Atoi(match[2])
		pages, pagesErr := strconv.Atoi(match[3])
		if totalErr != nil || numberErr != nil || pagesErr != nil || number > pages || pages != 1+(total-1)/pageSize {
			return provider.ProductPage{}, parseError()
		}
		if result.Page.Number != 0 && (result.Page.Number != number || *result.Page.TotalItems != total || *result.Page.TotalPages != pages) {
			return provider.ProductPage{}, parseError()
		}
		result.Page = provider.PageInfo{Number: number, Size: pageSize, TotalItems: new(total), TotalPages: new(pages), HasNext: new(number < pages)}
	}
	if empty && len(cards) == 0 && result.Page.Number == 0 {
		result.Page = provider.PageInfo{Size: pageSize, TotalItems: new(0), HasNext: new(false)}
		return result, nil
	}
	if empty || result.Page.Number == 0 || len(cards) == 0 {
		return provider.ProductPage{}, parseError()
	}
	seen := make(map[string]bool)
	found := 0
	for _, card := range cards {
		item, err := parseCard(card, retrievedAt)
		if err == nil && seen[item.ID] {
			continue
		}
		found++
		if err != nil {
			continue
		}
		seen[item.ID] = true
		result.Items = append(result.Items, item)
	}
	if len(result.Items) == 0 {
		return provider.ProductPage{}, parseError()
	}
	if found != len(result.Items) {
		warning := provider.NewWarning(provider.WarningCodePartialParsing, "Some BuscoCotxe car entries could not be parsed.", nil)
		warning.FoundCount, warning.ParsedCount = new(found), new(len(result.Items))
		result.Warnings = []provider.Warning{warning}
	}
	return result, nil
}

func parseCard(card *html.Node, retrievedAt time.Time) (provider.ProductSummary, error) {
	u := websiteURL(attribute(card, "href"))
	if u == nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return provider.ProductSummary{}, errors.New("invalid car URL")
	}
	match := carPath.FindStringSubmatch(u.Path)
	if match == nil {
		return provider.ProductSummary{}, errors.New("invalid car path")
	}
	id := match[1]
	if claimed := attribute(card, "kmk-seguiment-iditem"); claimed != "" && claimed != id {
		return provider.ProductSummary{}, errors.New("car IDs do not match")
	}
	item := provider.ProductSummary{
		ID: id, URL: u.String(), Name: nodeText(find(find(card, "", "box-titol"), "h2", "")),
		DetailLevel: provider.DetailLevelSummary, RetrievedAt: retrievedAt,
	}
	if item.Name == "" {
		return provider.ProductSummary{}, errors.New("car name is missing")
	}
	display := nodeText(find(find(card, "", "box-overlay"), "", "uk-float-right"))
	var err error
	item.Price, err = parsePrice(display)
	if err != nil {
		return provider.ProductSummary{}, err
	}
	summary := nodeText(find(card, "em", ""))
	if km := kmPattern.FindStringSubmatch(summary); km != nil {
		item.Attributes = append(item.Attributes, provider.Attribute{Name: "kilometers", Value: km[1] + " km"})
	}
	if year := yearPattern.FindString(summary); year != "" {
		item.Attributes = append(item.Attributes, provider.Attribute{Name: "year", Value: year})
	}
	if body := nodeText(find(find(card, "", "box-footer"), "", "uk-float-left")); body != "" {
		item.Attributes = append(item.Attributes, provider.Attribute{Name: "body_type", Value: body})
	}
	if stock := stockPattern.FindString(summary); stock != "" {
		item.StockText = stock
	}
	if strings.EqualFold(display, "VENUT") {
		item.StockText = display
	}
	switch strings.ToLower(item.StockText) {
	case "estoc", "estoc estranger":
		item.Availability = provider.AvailabilityInStock
	case "venut", "reservat":
		item.Availability = provider.AvailabilityOutOfStock
	}
	style := attribute(find(card, "", "box-imatge"), "style")
	if image := imagePattern.FindStringSubmatch(style); image != nil {
		for _, value := range image[1:] {
			if u := websiteURL(value); u != nil && u.Path != "/img/buscocotxe.ad/nopic.svg" {
				item.ImageURL = u.String()
				break
			}
		}
	}
	return item, item.Validate()
}

func parsePrice(display string) (*provider.Money, error) {
	if display == "" || strings.EqualFold(display, "Consultar preu") || strings.EqualFold(display, "VENUT") {
		return nil, nil
	}
	if !pricePattern.MatchString(display) {
		return nil, errors.New("invalid displayed price")
	}
	amount := strings.TrimSpace(strings.TrimSuffix(display, "€"))
	amount = strings.ReplaceAll(strings.ReplaceAll(amount, ".", ""), ",", ".")
	price := &provider.Money{Amount: amount, Currency: "EUR", Display: display}
	return price, price.Validate()
}

func websiteURL(value string) *url.URL {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil || u.User != nil || u.Opaque != "" || strings.Contains(u.Path, "\\") {
		return nil
	}
	u = (&url.URL{Scheme: "https", Host: websiteHost}).ResolveReference(u)
	if u.Scheme != "https" || !strings.EqualFold(u.Host, websiteHost) {
		return nil
	}
	u.Host = websiteHost
	return u
}

func isCard(node *html.Node) bool {
	return node != nil && node.Type == html.ElementNode && node.Data == "a" && attribute(node, "kmk-seguiment") == "llistat"
}

func inCard(node *html.Node) bool {
	for ; node != nil; node = node.Parent {
		if isCard(node) {
			return true
		}
	}
	return false
}

func find(node *html.Node, tag, class string) *html.Node {
	if node == nil {
		return nil
	}
	for child := range node.Descendants() {
		if child.Type == html.ElementNode && (tag == "" || child.Data == tag) && (class == "" || hasClass(child, class)) {
			return child
		}
	}
	return nil
}

func hasClass(node *html.Node, class string) bool {
	for value := range strings.FieldsSeq(attribute(node, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func attribute(node *html.Node, key string) string {
	if node != nil {
		for _, attr := range node.Attr {
			if attr.Key == key {
				return attr.Val
			}
		}
	}
	return ""
}

func nodeText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var text strings.Builder
	for child := range node.Descendants() {
		if child.Type == html.TextNode {
			text.WriteString(child.Data)
			text.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(text.String()), " ")
}

func parseError() error {
	return provider.NewError(provider.ErrorCodeInvalidProviderResult, "BuscoCotxe returned invalid listing data", nil)
}
