package bikediscount

import (
	"context"

	"github.com/kostyay/ecom/provider"
)

const bikeSalePath = "/bike/sale"

// Deals returns one page from the stable Bike-Discount bike sale listing.
// It excludes products when the listing does not contain a reduction value.
func (implementation) Deals(ctx context.Context, request provider.DealsRequest) (provider.DealPage, error) {
	page, query, err := productListingQuery(request.Page, request.Sort, request.Filters)
	if err != nil {
		return provider.DealPage{}, err
	}
	target := resourceTarget{Path: bikeSalePath, Query: query}
	requestURL, err := bikeDiscountRequestURL(request.Market, target)
	if err != nil {
		return provider.DealPage{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the deal listing URL is invalid", err)
	}
	response, err := fetchResource(ctx, request.Request, target)
	if err != nil {
		return provider.DealPage{}, err
	}
	if response.FinalURL != "" {
		requestURL = response.FinalURL
	}
	document := responseDocument(response)
	extraction, hasNext, err := extractListing(document, requestURL)
	if err != nil {
		return provider.DealPage{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the deal product page could not be parsed", err)
	}

	deals := make([]provider.Deal, 0, len(extraction.Products))
	for _, product := range extraction.Products {
		if product.OriginalPrice == nil && product.DiscountAmount == nil && product.DiscountPercent == nil {
			continue
		}
		deals = append(deals, provider.Deal{Product: product})
	}
	page.HasNext = hasNext
	warnings := append([]provider.Warning(nil), extraction.Warnings...)
	warnings = append(warnings, currencyWarnings(request.Market.Currency, extraction.Products...)...)
	return provider.DealPage{Items: deals, Page: page, Warnings: warnings}, nil
}

var _ provider.DealsProvider = implementation{}
