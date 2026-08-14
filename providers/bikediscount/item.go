package bikediscount

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/kostyay/ecom/provider"
)

var displayedItemNumberPattern = regexp.MustCompile(`^[0-9]+$`)

// Item gets one Bike-Discount product page. A displayed numeric item number is
// first resolved through search because no direct numeric item route is known.
func (implementation) Item(ctx context.Context, request provider.ItemRequest) (provider.ItemResult, error) {
	target, warnings, err := resolveItemTarget(ctx, request)
	if err != nil {
		return provider.ItemResult{}, err
	}
	response, err := fetchResource(ctx, request.Request, resourceTarget{URL: target})
	if err != nil {
		return provider.ItemResult{}, err
	}
	pageURL := target
	if response.FinalURL != "" {
		pageURL = response.FinalURL
	}
	item, err := ExtractItemDetail(responseDocument(response), pageURL)
	if err != nil {
		return provider.ItemResult{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the Bike-Discount item page could not be parsed", err)
	}
	if displayedItemNumberPattern.MatchString(strings.TrimSpace(request.IDOrURL)) && item.ID != strings.TrimSpace(request.IDOrURL) {
		return provider.ItemResult{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the resolved Bike-Discount item number does not match the item page", nil)
	}
	if len(request.Variants) > 0 {
		selected, err := selectVisibleVariant(item.Variants, request.Variants)
		if err != nil {
			return provider.ItemResult{}, err
		}
		item.SelectedVariant = selected
	}
	warnings = append(warnings, currencyWarnings(request.Market.Currency, item.ProductSummary)...)
	return provider.ItemResult{Item: item, Warnings: warnings}, nil
}

func resolveItemTarget(ctx context.Context, request provider.ItemRequest) (string, []provider.Warning, error) {
	identifier := strings.TrimSpace(request.IDOrURL)
	if identifier == "" {
		return "", nil, provider.NewError(provider.ErrorCodeInvalidFilter, "a Bike-Discount item number or URL is required", nil)
	}
	if strings.Contains(identifier, "://") || strings.HasPrefix(identifier, "http:") || strings.HasPrefix(identifier, "https:") {
		validated, err := bikeDiscountRequestURL(request.Market, resourceTarget{URL: identifier})
		if err != nil {
			return "", nil, provider.NewError(provider.ErrorCodeInvalidFilter, "the item URL must be an English Bike-Discount HTTPS URL", err)
		}
		return validated, nil, nil
	}
	if !displayedItemNumberPattern.MatchString(identifier) {
		return "", nil, provider.NewError(provider.ErrorCodeInvalidFilter, "the Bike-Discount item ID must be the displayed numeric item number", nil)
	}

	page, err := (implementation{}).Search(ctx, provider.SearchRequest{Request: request.Request, Query: identifier})
	if err != nil {
		return "", nil, err
	}
	for _, product := range page.Items {
		if strings.TrimSpace(product.ID) != identifier || product.URL == "" {
			continue
		}
		validated, validationErr := bikeDiscountRequestURL(request.Market, resourceTarget{URL: product.URL})
		if validationErr != nil {
			return "", nil, provider.NewError(provider.ErrorCodeHTTPFailure, "Bike-Discount search returned an invalid item URL", validationErr)
		}
		return validated, page.Warnings, nil
	}
	return "", nil, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("Bike-Discount item number %q was not found in search results", identifier), nil)
}

// ExtractItemDetail parses one item document without making a request.
func ExtractItemDetail(document []byte, pageURL string) (provider.ItemDetail, error) {
	root, err := parseHTML(document)
	if err != nil {
		return provider.ItemDetail{}, err
	}
	article := firstDescendant(root, func(node *htmlNode) bool { return node.tag == "article" })
	if article == nil {
		return provider.ItemDetail{}, errors.New("item article is missing")
	}
	canonical := canonicalURL(root, pageURL)
	if canonical == "" {
		return provider.ItemDetail{}, errors.New("canonical item URL is missing")
	}
	parsedCanonical, err := url.Parse(canonical)
	if err != nil || parsedCanonical.Scheme != "https" || !strings.EqualFold(parsedCanonical.Host, "www.bike-discount.de") ||
		(parsedCanonical.Path != "/en" && !strings.HasPrefix(parsedCanonical.Path, "/en/")) {
		return provider.ItemDetail{}, errors.New("canonical item URL is not an English Bike-Discount URL")
	}

	product, ok := extractProduct(article, pageURL, canonical, true)
	if !ok || product.ID == "" || product.URL == "" {
		return provider.ItemDetail{}, errors.New("item name, number, or canonical URL is missing")
	}
	product.URL = canonical
	product.DetailLevel = provider.DetailLevelFull
	product.Attributes = extractItemAttributes(article)
	product.Variants = extractVisibleVariants(article, product.Price)
	product.PriceRange = visiblePriceRange(product.Variants)

	images := extractImageURLs(article, pageURL)
	if len(images) > 0 {
		product.ImageURL = images[0]
		encoded, marshalErr := json.Marshal(struct {
			Images []string `json:"images"`
		}{Images: images})
		if marshalErr != nil {
			return provider.ItemDetail{}, marshalErr
		}
		product.ProviderData = provider.Data{Name: encoded}
	}
	return provider.ItemDetail{ProductSummary: product, Description: extractDescription(article)}, nil
}

func extractDescription(article *htmlNode) string {
	for _, section := range descendants(article, func(node *htmlNode) bool { return node.tag == "section" }) {
		heading := firstDescendant(section, func(node *htmlNode) bool {
			return (node.tag == "h2" || node.tag == "h3") && strings.EqualFold(nodeText(node), "description")
		})
		if heading == nil {
			continue
		}
		parts := make([]string, 0)
		for _, node := range section.children {
			if node != heading {
				if text := nodeText(node); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func extractItemAttributes(article *htmlNode) []provider.Attribute {
	var attributes []provider.Attribute
	for _, list := range descendants(article, func(node *htmlNode) bool { return node.tag == "dl" }) {
		var name string
		for _, child := range list.children {
			switch child.tag {
			case "dt":
				name = nodeText(child)
			case "dd":
				if name != "" && nodeText(child) != "" {
					attributes = append(attributes, provider.Attribute{Name: name, Value: nodeText(child)})
				}
				name = ""
			}
		}
	}
	return attributes
}

func extractImageURLs(article *htmlNode, pageURL string) []string {
	seen := make(map[string]struct{})
	var images []string
	for _, image := range descendants(article, func(node *htmlNode) bool { return node.tag == "img" }) {
		for _, attribute := range productExtractionSelectors.imageAttributes {
			value := absoluteURL(image.attrs[attribute], pageURL)
			if value == "" {
				continue
			}
			if _, exists := seen[value]; !exists {
				seen[value] = struct{}{}
				images = append(images, value)
			}
			break
		}
	}
	return images
}

func extractVisibleVariants(article *htmlNode, itemPrice *provider.Money) []provider.Variant {
	var variants []provider.Variant
	for _, fieldset := range descendants(article, func(node *htmlNode) bool { return node.tag == "fieldset" }) {
		legend := firstDescendant(fieldset, func(node *htmlNode) bool { return node.tag == "legend" })
		key := ""
		if legend != nil {
			key = nodeText(legend)
		}
		if key == "" {
			continue
		}
		for _, label := range descendants(fieldset, func(node *htmlNode) bool { return node.tag == "label" }) {
			input := firstDescendant(label, func(node *htmlNode) bool { return node.tag == "input" })
			value := directText(label)
			if input == nil || value == "" {
				continue
			}
			availability, stockText := extractAvailability(label)
			if _, disabled := input.attrs["disabled"]; disabled {
				availability = provider.AvailabilityOutOfStock
				if stockText == "" {
					stockText = "Unavailable"
				}
			}
			price := firstMoney(label)
			if price == nil && itemPrice != nil && !strings.HasPrefix(strings.ToLower(itemPrice.Display), "from ") {
				itemPriceCopy := *itemPrice
				price = &itemPriceCopy
			}
			_, selected := input.attrs["checked"]
			variants = append(variants, provider.Variant{
				Attributes: []provider.Attribute{{Name: key, Value: value}}, Price: price,
				Availability: availability, StockText: stockText, Selected: selected,
			})
		}
	}
	return variants
}

func firstMoney(root *htmlNode) *provider.Money {
	for _, node := range descendants(root, func(node *htmlNode) bool { return nodeText(node) != "" }) {
		if money := parseMoney(nodeText(node)); money != nil {
			return money
		}
	}
	return nil
}

func visiblePriceRange(variants []provider.Variant) *provider.PriceRange {
	priced := make([]provider.Money, 0, len(variants))
	for _, variant := range variants {
		if variant.Price != nil {
			priced = append(priced, *variant.Price)
		}
	}
	if len(priced) < 2 {
		return nil
	}
	sort.SliceStable(priced, func(i, j int) bool {
		left, _ := new(big.Rat).SetString(priced[i].Amount)
		right, _ := new(big.Rat).SetString(priced[j].Amount)
		return left.Cmp(right) < 0
	})
	if priced[0].Currency != priced[len(priced)-1].Currency {
		return nil
	}
	return &provider.PriceRange{Minimum: priced[0], Maximum: priced[len(priced)-1]}
}

func selectVisibleVariant(variants []provider.Variant, selections []provider.VariantSelection) (*provider.Variant, error) {
	for index := range variants {
		matches := true
		for _, selection := range selections {
			found := false
			for _, attribute := range variants[index].Attributes {
				if attribute.Name == selection.Key && attribute.Value == selection.Value {
					found = true
					break
				}
			}
			if !found {
				matches = false
				break
			}
		}
		if matches {
			selected := variants[index]
			return &selected, nil
		}
	}
	choices := make([]string, 0, len(variants))
	for _, variant := range variants {
		for _, attribute := range variant.Attributes {
			choices = append(choices, attribute.Name+"="+attribute.Value)
		}
	}
	sort.Strings(choices)
	message := "the requested Bike-Discount variant was not found"
	if len(choices) > 0 {
		message += "; valid choices: " + strings.Join(choices, ", ")
	}
	return nil, provider.NewError(provider.ErrorCodeVariantNotFound, message, nil)
}

var _ provider.ItemProvider = implementation{}
