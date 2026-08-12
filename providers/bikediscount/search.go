package bikediscount

import (
	"context"
	"strings"

	"github.com/kostyay/ecom/provider"
)

// Search uses the only verified Bike-Discount search request shape. The
// evidence does not prove that the current website honors sSearch, so every
// successful result includes a machine-readable warning about this limit.
func (implementation) Search(ctx context.Context, request provider.SearchRequest) (provider.ProductPage, error) {
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidFilter, "the Bike-Discount search query is required", nil)
	}
	page, values, err := productListingQuery(request.Page, request.Sort, request.Filters)
	if err != nil {
		return provider.ProductPage{}, err
	}
	values = append([]provider.RequestValue{{Name: "sSearch", Values: []string{query}}}, values...)
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
	warnings = append(warnings, provider.NewWarning(
		provider.WarningCodeSearchSemanticsUnverified,
		"Bike-Discount can ignore the verified legacy search query. Check that the returned products match the query.",
		nil,
	))
	warnings = append(warnings, currencyWarnings(request.Market.Currency, extraction.Products...)...)
	return provider.ProductPage{Items: extraction.Products, Page: page, Warnings: warnings}, nil
}

var _ provider.SearchProvider = implementation{}
