package bikediscount

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/kostyay/ecom/provider"
)

var (
	brandGroupPattern = regexp.MustCompile(`^(?:#|[A-Z])$`)
	brandSlugPattern  = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type brandExtraction struct {
	items    []provider.Brand
	warnings []provider.Warning
}

// Brands returns the complete Bike-Discount alphabetical brand index. The
// complete result lets Core provide local, case-insensitive text search because
// the website has no verified native brand search request.
func (implementation) Brands(ctx context.Context, request provider.BrandListRequest) (provider.BrandPage, error) {
	page, err := brandIndexPageInfo(request.Page)
	if err != nil {
		return provider.BrandPage{}, err
	}
	response, err := fetchResource(ctx, request.Request, resourceTarget{Path: "/brands"})
	if err != nil {
		return provider.BrandPage{}, err
	}
	pageURL := bikeDiscountBaseURL + "/en/brands"
	if response.FinalURL != "" {
		pageURL = response.FinalURL
	}
	extraction, err := extractBrands(responseDocument(response), pageURL)
	if err != nil {
		return provider.BrandPage{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the Bike-Discount brand index could not be parsed", err)
	}
	if len(extraction.items) == 0 {
		return provider.BrandPage{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the Bike-Discount brand index does not contain useful brands", nil)
	}
	return provider.BrandPage{Items: extraction.items, Page: page, Warnings: extraction.warnings}, nil
}

// BrandItems returns one product page for a stable brand slug from Brands.
func (implementation) BrandItems(ctx context.Context, request provider.BrandItemsRequest) (provider.ProductPage, error) {
	slug := strings.ToLower(strings.TrimSpace(request.BrandID))
	if !brandSlugPattern.MatchString(slug) {
		return provider.ProductPage{}, provider.NewError(
			provider.ErrorCodeInvalidFilter,
			"the brand ID must be a canonical slug returned by the brand index",
			nil,
		)
	}
	page, query, err := productListingQuery(request.Page, request.Sort, request.Filters)
	if err != nil {
		return provider.ProductPage{}, err
	}
	target := resourceTarget{Path: "/" + slug, Query: query}
	requestURL, err := bikeDiscountRequestURL(request.Market, target)
	if err != nil {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the brand URL is invalid", err)
	}
	response, err := fetchResource(ctx, request.Request, target)
	if err != nil {
		return provider.ProductPage{}, err
	}
	if response.FinalURL != "" {
		requestURL = response.FinalURL
	}
	document := responseDocument(response)
	extraction, hasNext, err := extractListing(document, requestURL)
	if err != nil {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the brand product page could not be parsed", err)
	}
	page.HasNext = hasNext
	warnings := append([]provider.Warning(nil), extraction.Warnings...)
	warnings = append(warnings, currencyWarnings(request.Market.Currency, extraction.Products...)...)
	return provider.ProductPage{Items: extraction.Products, Page: page, Warnings: warnings}, nil
}

func extractBrands(document []byte, pageURL string) (brandExtraction, error) {
	root, err := parseHTML(document)
	if err != nil {
		return brandExtraction{}, err
	}
	groups := descendants(root, func(node *htmlNode) bool {
		if node.tag != "section" {
			return false
		}
		heading := firstDirectChild(node, "h2")
		return heading != nil && brandGroupPattern.MatchString(nodeText(heading))
	})
	found, parsed := 0, 0
	items := make([]provider.Brand, 0)
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, link := range descendants(group, func(node *htmlNode) bool { return node.tag == "a" }) {
			found++
			name := nodeText(link)
			id, brandURL := canonicalBrandIdentity(link.attrs["href"], pageURL)
			if name == "" || id == "" {
				continue
			}
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			seen[id] = struct{}{}
			parsed++
			items = append(items, provider.Brand{ID: id, Name: name, URL: brandURL})
		}
	}
	result := brandExtraction{items: items}
	if found > parsed {
		result.warnings = append(result.warnings, partialCategoryWarning(
			"Some brand entries could not be parsed.", found, parsed,
			errors.New("one or more brand links are malformed or duplicated"),
		))
	}
	return result, nil
}

func canonicalBrandIdentity(reference, pageURL string) (string, string) {
	absolute := absoluteURL(reference, pageURL)
	parsed, err := url.Parse(absolute)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "www.bike-discount.de") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ""
	}
	slug, ok := strings.CutPrefix(parsed.Path, "/en/")
	if !ok {
		return "", ""
	}
	slug = strings.ToLower(slug)
	if !brandSlugPattern.MatchString(slug) {
		return "", ""
	}
	return slug, bikeDiscountBaseURL + "/en/" + slug
}

func brandIndexPageInfo(request provider.PageRequest) (provider.PageInfo, error) {
	page, err := categoryPageInfo(request)
	if err != nil {
		return provider.PageInfo{}, err
	}
	if page.Number != 1 {
		return provider.PageInfo{}, provider.NewError(
			provider.ErrorCodeInvalidFilter,
			"the complete Bike-Discount brand index is available only as page 1",
			nil,
		)
	}
	hasNext := false
	page.HasNext = &hasNext
	return page, nil
}

var _ provider.BrandListProvider = implementation{}
var _ provider.BrandItemsProvider = implementation{}
