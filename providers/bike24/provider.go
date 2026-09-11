// Package bike24 implements the Bike24 commerce provider.
// Import this package for its registration side effect.
package bike24

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kostyay/ecom/provider"
	"golang.org/x/net/html"
)

const (
	// Name is the stable provider identifier.
	Name            = "bike24"
	baseURL         = "https://www.bike24.com"
	defaultPageSize = 30
)

var (
	productPathPattern     = regexp.MustCompile(`^/p([0-9]+)\.html$`)
	pricePattern           = regexp.MustCompile(`[0-9](?:[0-9.,\x{00a0} ]*[0-9])?\s*(?:€|EUR)`)
	searchMetadataPatterns = [...]*regexp.Regexp{
		regexp.MustCompile(`\\?"hitsPerPage\\?":([0-9]+)`),
		regexp.MustCompile(`\\?"nbHits\\?":([0-9]+)`),
		regexp.MustCompile(`\\?"nbPages\\?":([0-9]+)`),
		regexp.MustCompile(`\\?"page\\?":([0-9]+)`),
	}
)

type implementation struct{}

func init() { provider.MustRegister(registration()) }

func registration() provider.Registration {
	return provider.Registration{
		Name: Name, SDKAPIVersion: provider.APIVersion, Implementation: implementation{},
		Capabilities: []provider.CapabilityName{provider.CapabilitySearch},
	}
}

func (implementation) ValidateConfig(configuration map[string]any) error {
	for key := range configuration {
		return provider.NewError(provider.ErrorCodeInvalidProviderConfig, fmt.Sprintf("Bike24 does not support setting %q", key), nil)
	}
	return nil
}

func (implementation) Help(_ context.Context, request provider.HelpRequest) (provider.HelpResult, error) {
	if err := validatePricing(request.Pricing); err != nil {
		return provider.HelpResult{}, err
	}
	help := provider.Help{
		Name: Name, DisplayName: "Bike24", Description: "Find products on the Bike24 website.",
		Capabilities: []provider.CapabilityHelp{
			{Name: provider.CapabilitySearch, Supported: true, Description: "Search public Bike24 product listings."},
			{Name: provider.CapabilityCategories, Supported: false},
			{Name: provider.CapabilityCategorySearch, Supported: false},
			{Name: provider.CapabilityCategoryItems, Supported: false},
			{Name: provider.CapabilityBrands, Supported: false},
			{Name: provider.CapabilityBrandSearch, Supported: false},
			{Name: provider.CapabilityBrandItems, Supported: false},
			{Name: provider.CapabilityDeals, Supported: false},
			{Name: provider.CapabilityFilters, Supported: false},
			{Name: provider.CapabilityItem, Supported: false},
			{Name: provider.CapabilityVariantSelection, Supported: false},
		},
		Search: &provider.SearchHelp{QueryRequired: true, Syntax: "plain product text", Examples: []string{"Garmin Edge"}},
		Pagination: &provider.PaginationHelp{
			Mode: provider.PaginationPageNumber, FirstPage: 1, DefaultPageSize: defaultPageSize,
			SupportedPageSizes: []int{defaultPageSize}, ReportsTotalItems: true, ReportsTotalPages: true,
		},
		Markets: &provider.MarketRestrictions{
			Countries: []string{"DE"}, Languages: []string{"en"}, Currencies: []string{"EUR"},
			Notes: []string{
				"Prices contain the displayed item price only. Shipping costs are excluded.",
				"A browser session must have Germany as its selected delivery country.",
			},
		},
		Access: &provider.AccessRequirements{
			Authentication: provider.AuthenticationNone, Browser: provider.BrowserFallback,
			SupportsCDP: true, SupportsInteractive: true,
			Notes: []string{"Bike24 can reject direct HTTP requests. The Core can use browser or CDP transport."},
		},
		Transport: []provider.TransportNote{
			{Mode: provider.TransportHTTP, UseWhen: "The public search page accepts direct HTTP."},
			{Mode: provider.TransportBrowser, UseWhen: "Direct HTTP is rejected."},
			{Mode: provider.TransportCDP, UseWhen: "An existing Chrome session is configured and isolated browser access is rejected."},
		},
		Warnings:     []provider.HelpWarning{{Code: "site_challenge", Message: "Bike24 can reject automated website access."}},
		ProviderData: provider.Data{Name: json.RawMessage(`{"base_url":"https://www.bike24.com","shipping_costs_included":false}`)},
	}
	if err := help.Validate(); err != nil {
		return provider.HelpResult{}, fmt.Errorf("validate Bike24 help: %w", err)
	}
	return provider.HelpResult{Help: help}, nil
}

func (implementation) Search(ctx context.Context, request provider.SearchRequest) (provider.ProductPage, error) {
	if err := ctx.Err(); err != nil {
		return provider.ProductPage{}, err
	}
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidFilter, "the Bike24 search query is required", nil)
	}
	if len(request.Filters) != 0 || request.Sort != nil {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidFilter, "Bike24 search filters and sort values are not supported", nil)
	}
	page := request.Page.Number
	if page == 0 {
		page = 1
	}
	if page < 1 || request.Page.Size != 0 && request.Page.Size != defaultPageSize {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidFilter, "Bike24 supports positive page numbers with page size 30", nil)
	}
	if err := validateRequest(request.Request); err != nil {
		return provider.ProductPage{}, err
	}
	if request.Resources == nil {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the Bike24 resource service is required", nil)
	}

	values := []provider.RequestValue{{Name: "searchTerm", Values: []string{query}}}
	if page > 1 {
		values = append(values, provider.RequestValue{Name: "page", Values: []string{strconv.Itoa(page)}})
	}
	response, err := request.Resources.Fetch(ctx, provider.ResourceRequest{
		Method: http.MethodGet, URL: baseURL + "/search", Query: values,
		Transport: provider.TransportPolicy{Preferred: []provider.TransportMode{
			provider.TransportHTTP,
			provider.TransportBrowser,
			provider.TransportCDP,
		}},
		Market: request.Market,
		Cache:  request.Cache, Interactive: request.Interactive,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return provider.ProductPage{}, err
		}
		if _, ok := provider.ErrorCodeOf(err); ok {
			return provider.ProductPage{}, err
		}
		code := provider.ErrorCodeHTTPFailure
		if response.Transport == provider.TransportBrowser || response.Transport == provider.TransportCDP {
			code = provider.ErrorCodeBrowserFailure
		}
		return provider.ProductPage{}, provider.NewError(code, "the Bike24 search request failed", err)
	}
	document := response.Body
	if response.Page != nil {
		document = response.Page.HTML
	}
	pageInfo, ok := searchPageInfo(document)
	if !ok {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the Bike24 search metadata could not be parsed", nil)
	}
	if pageInfo.Number != page || pageInfo.Size != defaultPageSize {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeHTTPFailure, "Bike24 returned a different search page", nil)
	}
	if !bytes.Contains(document, []byte("selected delivery country Germany")) {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeHTTPFailure, "Bike24 did not return the Germany delivery market", nil)
	}
	if *pageInfo.TotalItems == 0 {
		return provider.ProductPage{Items: []provider.ProductSummary{}, Page: pageInfo}, nil
	}
	items, warnings, err := extractProducts(document, response.RetrievedAt)
	if err != nil {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the Bike24 search page could not be parsed", err)
	}
	return provider.ProductPage{
		Items: items, Page: pageInfo, Warnings: warnings,
	}, nil
}

func validateRequest(request provider.Request) error {
	if err := validatePricing(request.Pricing); err != nil {
		return err
	}
	if err := request.Market.Validate(); err != nil {
		return provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the Bike24 market is invalid", err)
	}
	if !strings.EqualFold(request.Market.Country, "DE") || !strings.EqualFold(request.Market.Language, "en") || !strings.EqualFold(request.Market.Currency, "EUR") {
		return provider.NewError(provider.ErrorCodeInvalidResourceRequest, "Bike24 supports only the DE/en/EUR market", nil)
	}
	return nil
}

func searchPageInfo(document []byte) (provider.PageInfo, bool) {
	start := bytes.Index(document, []byte("initialResults"))
	if start < 0 {
		return provider.PageInfo{}, false
	}
	metadata := document[start:]
	if len(metadata) > 4096 {
		metadata = metadata[:4096]
	}
	values := [4]int{}
	for index, pattern := range searchMetadataPatterns {
		match := pattern.FindSubmatch(metadata)
		if match == nil {
			return provider.PageInfo{}, false
		}
		parsed, err := strconv.Atoi(string(match[1]))
		if err != nil {
			return provider.PageInfo{}, false
		}
		values[index] = parsed
	}
	pageSize, totalItems, totalPages, sitePage := values[0], values[1], values[2], values[3]
	if pageSize < 1 || (totalItems == 0) != (totalPages == 0) || totalPages > 0 && sitePage >= totalPages {
		return provider.PageInfo{}, false
	}
	hasNext := sitePage+1 < totalPages
	return provider.PageInfo{
		Number: sitePage + 1, Size: pageSize, TotalItems: new(totalItems), TotalPages: new(totalPages), HasNext: new(hasNext),
	}, true
}

func validatePricing(pricing provider.PricingPolicy) error {
	if pricing.IncludeShipping {
		return provider.NewError(provider.ErrorCodeInvalidProviderConfig, "Bike24 does not support prices that include shipping", nil)
	}
	return nil
}

func extractProducts(document []byte, retrievedAt time.Time) ([]provider.ProductSummary, []provider.Warning, error) {
	root, err := html.Parse(bytes.NewReader(document))
	if err != nil {
		return nil, nil, err
	}
	foundIDs := make(map[string]bool)
	seen := make(map[string]bool)
	var items []provider.ProductSummary
	for node := range root.Descendants() {
		if node.Type != html.ElementNode || node.Data != "a" {
			continue
		}
		id, productURL, ok := productLink(attribute(node, "href"))
		if !ok || seen[id] {
			continue
		}
		foundIDs[id] = true
		text, alt, imageSource, lazyImageSource := productContent(node)
		name := strings.TrimSpace(attribute(node, "title"))
		if name == "" {
			name = alt
		}
		if name == "" {
			name = text
		}
		if name == "" {
			continue
		}
		seen[id] = true
		item := provider.ProductSummary{
			ID: id, URL: productURL, Name: name, ImageURL: imageURL(imageSource, lazyImageSource),
			Availability: provider.AvailabilityUnknown, RetrievedAt: retrievedAt, DetailLevel: provider.DetailLevelSummary,
		}
		item.Price, item.OriginalPrice = nearbyPrices(text)
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, nil, errors.New("no Bike24 products were found")
	}
	var warnings []provider.Warning
	if len(items) != len(foundIDs) {
		parsed := len(items)
		found := len(foundIDs)
		warning := provider.NewWarning(provider.WarningCodePartialParsing, "Some Bike24 product entries could not be parsed.", nil)
		warning.FoundCount, warning.ParsedCount = new(found), new(parsed)
		warnings = append(warnings, warning)
	}
	return items, warnings, nil
}

func productLink(value string) (string, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host != "" && !strings.EqualFold(parsed.Host, "www.bike24.com") {
		return "", "", false
	}
	match := productPathPattern.FindStringSubmatch(parsed.Path)
	if match == nil || parsed.Query().Get("origin") != "SRP" {
		return "", "", false
	}
	return match[1], baseURL + parsed.Path, true
}

func nearbyPrices(text string) (*provider.Money, *provider.Money) {
	type indexedPrice struct {
		value      *provider.Money
		start, end int
	}
	matches := pricePattern.FindAllStringIndex(text, -1)
	prices := make([]indexedPrice, 0, len(matches))
	for _, match := range matches {
		if price := money(text[match[0]:match[1]]); price != nil {
			prices = append(prices, indexedPrice{value: price, start: match[0], end: match[1]})
		}
	}
	if len(prices) == 0 {
		return nil, nil
	}
	if len(prices) == 1 {
		return prices[0].value, nil
	}

	original := -1
	if rrp := strings.Index(strings.ToUpper(text), "RRP"); rrp >= 0 {
		for index, price := range prices {
			if price.end <= rrp {
				original = index
			}
		}
		if original < 0 {
			original = slices.IndexFunc(prices, func(price indexedPrice) bool { return price.start >= rrp })
		}
	}
	current := len(prices) - 1
	if current == original {
		current--
	}
	if original >= 0 {
		return prices[current].value, prices[original].value
	}
	return prices[current].value, nil
}

func money(display string) *provider.Money {
	amount := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(display, "EUR"), "€"))
	amount = strings.NewReplacer(" ", "", "\u00a0", "").Replace(amount)
	before, after, found := strings.CutLast(amount, ",")
	if dotBefore, dotAfter, dotFound := strings.CutLast(amount, "."); dotFound && (!found || len(dotAfter) < len(after)) {
		before, after, found = dotBefore, dotAfter, true
	}
	if found && len(after) == 2 {
		whole := strings.NewReplacer(",", "", ".", "").Replace(before)
		amount = whole + "." + after
	} else {
		amount = strings.NewReplacer(",", "", ".", "").Replace(amount)
	}
	price := &provider.Money{Amount: amount, Currency: "EUR", Display: strings.TrimSpace(display)}
	if price.Validate() != nil {
		return nil
	}
	return price
}

func imageURL(source, lazySource string) string {
	value := source
	if value == "" {
		value = lazySource
	}
	parsed, err := url.Parse(value)
	if err != nil || value == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Path == "" {
		return ""
	}
	if parsed.IsAbs() {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return ""
		}
		return parsed.String()
	}
	base, _ := url.Parse(baseURL)
	return base.ResolveReference(parsed).String()
}

func productContent(node *html.Node) (text, alt, source, lazySource string) {
	var content strings.Builder
	for descendant := range node.Descendants() {
		if descendant.Type == html.TextNode {
			content.WriteByte(' ')
			content.WriteString(descendant.Data)
			continue
		}
		if descendant.Type != html.ElementNode || descendant.Data != "img" {
			continue
		}
		if alt == "" {
			alt = strings.TrimSpace(attribute(descendant, "alt"))
		}
		if source == "" {
			source = strings.TrimSpace(attribute(descendant, "src"))
		}
		if lazySource == "" {
			lazySource = strings.TrimSpace(attribute(descendant, "data-src"))
		}
	}
	return strings.Join(strings.Fields(content.String()), " "), alt, source, lazySource
}

func attribute(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, name) {
			return attribute.Val
		}
	}
	return ""
}

var (
	_ provider.ConfigValidator = implementation{}
	_ provider.HelpProvider    = implementation{}
	_ provider.SearchProvider  = implementation{}
)
