package bikediscount

import (
	"context"
	"strings"

	"github.com/kostyay/ecom/provider"
)

// Search uses the current Bike-Discount storefront search request.
func (implementation) Search(ctx context.Context, request provider.SearchRequest) (provider.ProductPage, error) {
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidFilter, "the Bike-Discount search query is required", nil)
	}
	page, values, err := productListingQuery(request.Page, request.Sort, request.Filters)
	if err != nil {
		return provider.ProductPage{}, err
	}
	values = append([]provider.RequestValue{{Name: "search", Values: []string{query}}}, values...)
	target := resourceTarget{Path: "/search", Query: values}
	requestURL, err := bikeDiscountRequestURL(request.Market, target)
	if err != nil {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the search URL is invalid", err)
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
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the search product page could not be parsed", err)
	}
	page.HasNext = hasNext
	warnings := append([]provider.Warning(nil), extraction.Warnings...)
	warnings = append(warnings, currencyWarnings(request.Market.Currency, extraction.Products...)...)
	return provider.ProductPage{Items: extraction.Products, Page: page, Warnings: warnings}, nil
}

var _ provider.SearchProvider = implementation{}
