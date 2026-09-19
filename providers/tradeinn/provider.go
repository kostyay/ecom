// Package tradeinn implements public search across the Tradeinn shops.
// Import this package for its registration side effect.
package tradeinn

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/kostyay/ecom/provider"
)

// Name is the stable Provider identifier.
const Name = "tradeinn"

const (
	searchURL = "https://www.tradeinn.com/listado.php"
	pageSize  = 45
	maxPage   = 10
	// The public website uses this anonymous fallback when no analytics cookie exists.
	visitorID = "1556872684.1715855695"
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
	if len(configuration) != 0 {
		return provider.NewError(provider.ErrorCodeInvalidProviderConfig, "Tradeinn does not support Provider settings", nil)
	}
	return nil
}

func (implementation) Help(context.Context, provider.HelpRequest) (provider.HelpResult, error) {
	help := provider.Help{
		Name: Name, DisplayName: "Tradeinn", Description: "Search products across the Tradeinn shops, including Bikeinn.",
		Capabilities: []provider.CapabilityHelp{
			{Name: provider.CapabilitySearch, Supported: true},
			{Name: provider.CapabilityCategories}, {Name: provider.CapabilityCategorySearch},
			{Name: provider.CapabilityCategoryItems}, {Name: provider.CapabilityBrands},
			{Name: provider.CapabilityBrandSearch}, {Name: provider.CapabilityBrandItems},
			{Name: provider.CapabilityDeals}, {Name: provider.CapabilityFilters},
			{Name: provider.CapabilityItem}, {Name: provider.CapabilityVariantSelection},
		},
		Search: &provider.SearchHelp{
			QueryRequired: true, Syntax: "plain product text with at least three characters", Examples: []string{"powertube", "Garmin Edge"},
			Notes: []string{
				"Results can come from any Tradeinn shop. Filters and custom sorts are not supported.",
				"Tradeinn can correct or expand the query. Such results include a search_semantics_unverified warning.",
			},
		},
		Pagination: &provider.PaginationHelp{
			Mode: provider.PaginationPageNumber, FirstPage: 1, DefaultPageSize: pageSize,
			SupportedPageSizes: []int{pageSize}, ReportsTotalItems: true,
			Notes: []string{
				"Pages 1 to 10 are supported. Later pages follow temporary tokens from page one through the Core cache.",
				"The site controls the actual item count. A page after the last token returns no items. Total pages are not inferred.",
			},
		},
		Markets: &provider.MarketRestrictions{
			Countries: []string{"AD", "DE"}, Languages: []string{"en"}, Currencies: []string{"EUR"},
			Notes: []string{
				"Prices use the selected country's catalog values and exclude shipping. Other currencies produce a currency_unavailable warning.",
				"IN_STOCK is the search catalog status. It does not establish immediate warehouse stock or a delivery date.",
			},
		},
		Access:    &provider.AccessRequirements{Authentication: provider.AuthenticationNone, Browser: provider.BrowserNone},
		Transport: []provider.TransportNote{{Mode: provider.TransportHTTP, UseWhen: "Read public search JSON through the Core."}},
		Warnings:  []provider.HelpWarning{{Code: "public_endpoint", Message: "Tradeinn can change or limit its website search endpoint."}},
	}
	return provider.HelpResult{Help: help}, help.Validate()
}

func (implementation) Search(ctx context.Context, request provider.SearchRequest) (provider.ProductPage, error) {
	if err := ctx.Err(); err != nil {
		return provider.ProductPage{}, err
	}
	query := strings.TrimSpace(request.Query)
	page := cmp.Or(request.Page.Number, 1)
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) < 3 || len(request.Filters) != 0 || request.Sort != nil || page < 1 || page > maxPage || request.Page.Size != 0 && request.Page.Size != pageSize {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidFilter, "Tradeinn requires at least three search characters, pages 1 to 10, and page size 45; filters and sorts are not supported", nil)
	}
	if request.Pricing.IncludeShipping {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidProviderConfig, "Tradeinn cannot include shipping in displayed prices", nil)
	}
	if err := request.Market.Validate(); err != nil {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the Tradeinn market is invalid", err)
	}
	country := strings.ToUpper(request.Market.Country)
	countryID := map[string]string{"AD": "4", "DE": "75"}[country]
	if countryID == "" || !strings.EqualFold(request.Market.Language, "en") {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "Tradeinn supports AD and DE markets in English", nil)
	}
	if request.Resources == nil {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the resource service is required", nil)
	}

	// ponytail: page-number access walks cursors, capped at ten pages; add SDK cursor support if deeper access is needed.
	token := "null"
	seenTokens := map[string]bool{token: true}
	var result provider.ProductPage
	for current := 1; current <= page; current++ {
		response, err := fetchSearch(ctx, request.Request, query, country, countryID, token)
		if err != nil {
			return provider.ProductPage{}, err
		}
		document, err := decodeSearch(response.Body)
		if err != nil {
			return provider.ProductPage{}, err
		}
		if document.NextPageToken != "" && seenTokens[document.NextPageToken] {
			return provider.ProductPage{}, invalidResult("Tradeinn repeated a pagination token")
		}
		if current == page || document.NextPageToken == "" {
			if current < page {
				document.Results = nil
			}
			result, err = parseProducts(document, countryID, query, page, response.RetrievedAt)
			if err != nil {
				return provider.ProductPage{}, err
			}
			break
		}
		token = document.NextPageToken
		seenTokens[token] = true
	}
	if request.Market.Currency != "EUR" {
		warning := provider.NewWarning(provider.WarningCodeCurrencyUnavailable, "Tradeinn prices are available only in EUR for this market.", nil)
		warning.RequestedCurrency, warning.ActualCurrency = request.Market.Currency, "EUR"
		result.Warnings = append(result.Warnings, warning)
	}
	return result, ctx.Err()
}

func fetchSearch(ctx context.Context, request provider.Request, query, country, countryID, token string) (provider.ResourceResponse, error) {
	values := url.Values{
		"action": {"buscador_google"}, "palabras": {query}, "id_tienda": {"0"},
		"nextToken": {token}, "visitorid": {visitorID}, "idioma": {"eng"},
	}
	body := []byte(values.Encode())
	response, err := request.Resources.Fetch(ctx, provider.ResourceRequest{
		Method: http.MethodPost, URL: searchURL,
		Headers: []provider.RequestValue{
			{Name: "Content-Type", Values: []string{"application/x-www-form-urlencoded"}},
			{Name: "Accept", Values: []string{"application/json, text/plain, */*"}},
			{Name: "Origin", Values: []string{"https://www.tradeinn.com"}},
			{Name: "Referer", Values: []string{"https://www.tradeinn.com/en?country=" + strings.ToLower(country)}},
			{Name: "Cookie", Values: []string{"id_pais=" + countryID}},
			{Name: "User-Agent", Values: []string{"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36"}},
		},
		Body: provider.RequestBody{Bytes: body, Sensitive: true},
		// Partition by the complete form without exposing the query or cursor.
		CachePartition: fmt.Sprintf("%x", sha256.Sum256(body)),
		Transport:      provider.TransportPolicy{Required: provider.TransportHTTP},
		Market:         request.Market, Cache: request.Cache, Interactive: request.Interactive,
	})
	if ctx.Err() != nil {
		return response, ctx.Err()
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return response, err
		}
		if _, coded := provider.ErrorCodeOf(err); coded {
			return response, err
		}
		return response, provider.NewError(provider.ErrorCodeHTTPFailure, "the Tradeinn search request failed", err)
	}
	if response.StatusCode != http.StatusOK {
		code := provider.ErrorCodeHTTPFailure
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
			code = provider.ErrorCodeAccessBlocked
		}
		return response, provider.NewError(code, "Tradeinn returned an unsuccessful response", nil)
	}
	if response.FinalURL != "" && response.FinalURL != searchURL {
		return response, invalidResult("Tradeinn returned a different search URL")
	}
	return response, nil
}

func invalidResult(message string) error {
	return provider.NewError(provider.ErrorCodeInvalidProviderResult, message, nil)
}
