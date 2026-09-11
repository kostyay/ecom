package buscocotxe

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/kostyay/ecom/provider"
)

// Name is the stable Provider identifier.
const Name = "buscocotxe"

const searchURL = "https://" + websiteHost + "/ca/search"

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
		return provider.NewError(provider.ErrorCodeInvalidProviderConfig, "BuscoCotxe does not support Provider settings", nil)
	}
	return nil
}

func (implementation) Help(_ context.Context, _ provider.HelpRequest) (provider.HelpResult, error) {
	help := provider.Help{
		Name: Name, DisplayName: "BuscoCotxe", Description: "Search public car listings in Andorra.",
		Capabilities: []provider.CapabilityHelp{
			{Name: provider.CapabilitySearch, Supported: true},
			{Name: provider.CapabilityCategories},
			{Name: provider.CapabilityCategorySearch},
			{Name: provider.CapabilityCategoryItems},
			{Name: provider.CapabilityBrands},
			{Name: provider.CapabilityBrandSearch},
			{Name: provider.CapabilityBrandItems},
			{Name: provider.CapabilityDeals},
			{Name: provider.CapabilityFilters},
			{Name: provider.CapabilityItem},
			{Name: provider.CapabilityVariantSelection},
		},
		Search: &provider.SearchHelp{
			QueryRequired: true, Syntax: "plain car search text", Examples: []string{"BMW X3", "elèctric"},
			Notes: []string{
				"Matches can come from titles or descriptions. Results keep the website order.",
				"Filters and custom sorts are not supported. Results can include sold cars and cars with no stated price.",
			},
		},
		Pagination: &provider.PaginationHelp{
			Mode: provider.PaginationPageNumber, FirstPage: 1, DefaultPageSize: pageSize,
			SupportedPageSizes: []int{pageSize}, ReportsTotalItems: true,
			Notes: []string{
				"One request returns one page. A different returned page number is an error.",
				"Nonempty results include total pages. Empty results use the requested page number and omit total pages because the website omits page metadata.",
			},
		},
		Markets: &provider.MarketRestrictions{
			Currencies: []string{"EUR"},
			Notes: []string{
				"The catalog is always Andorra in Catalan, regardless of requested country or language.",
				"Prices are displayed item prices in EUR. Other currencies produce a currency_unavailable warning. Shipping inclusion is not supported.",
			},
		},
		Access: &provider.AccessRequirements{
			Authentication: provider.AuthenticationNone, Browser: provider.BrowserNone,
			Notes: []string{"HTTP only. Browser, CDP, and manual challenge handling are not supported."},
		},
		Transport: []provider.TransportNote{{Mode: provider.TransportHTTP, UseWhen: "Read public search HTML through the Core."}},
	}
	return provider.HelpResult{Help: help}, help.Validate()
}

func (implementation) Search(ctx context.Context, request provider.SearchRequest) (provider.ProductPage, error) {
	if err := ctx.Err(); err != nil {
		return provider.ProductPage{}, err
	}
	query := strings.TrimSpace(request.Query)
	page := request.Page.Number
	if page == 0 {
		page = 1
	}
	if query == "" || len(request.Filters) != 0 || request.Sort != nil || page < 1 || request.Page.Size != 0 && request.Page.Size != pageSize {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidFilter, "BuscoCotxe requires search text, positive page numbers, and page size 30; filters and sorts are not supported", nil)
	}
	if request.Pricing.IncludeShipping {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidProviderConfig, "BuscoCotxe cannot include shipping in displayed prices", nil)
	}
	if err := request.Market.Validate(); err != nil {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the BuscoCotxe market is invalid", err)
	}
	if request.Resources == nil {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the resource service is required", nil)
	}
	response, err := request.Resources.Fetch(ctx, provider.ResourceRequest{
		Method: http.MethodGet, URL: searchURL,
		Query: []provider.RequestValue{
			{Name: "search", Values: []string{query}},
			{Name: "pn", Values: []string{strconv.Itoa(page)}},
		},
		Transport: provider.TransportPolicy{Required: provider.TransportHTTP},
		Market:    request.Market, Cache: request.Cache, Interactive: request.Interactive,
	})
	if ctx.Err() != nil {
		return provider.ProductPage{}, ctx.Err()
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return provider.ProductPage{}, err
		}
		if _, coded := provider.ErrorCodeOf(err); coded {
			return provider.ProductPage{}, err
		}
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the BuscoCotxe request failed", err)
	}
	if response.StatusCode != http.StatusOK {
		code := provider.ErrorCodeHTTPFailure
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
			code = provider.ErrorCodeAccessBlocked
		}
		return provider.ProductPage{}, provider.NewError(code, "BuscoCotxe returned an unsuccessful response", nil)
	}
	if response.FinalURL != "" && !matchesSearchURL(response.FinalURL, query, page) {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidProviderResult, "BuscoCotxe returned a different search URL", nil)
	}
	result, err := parseListing(response.Body, response.RetrievedAt)
	if err != nil {
		return provider.ProductPage{}, err
	}
	if result.Page.Number == 0 {
		result.Page.Number = page
	} else if result.Page.Number != page {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidProviderResult, "BuscoCotxe returned a different page number", nil)
	}
	if request.Market.Currency != "EUR" {
		warning := provider.NewWarning(provider.WarningCodeCurrencyUnavailable, "BuscoCotxe prices are available only in EUR.", nil)
		warning.RequestedCurrency, warning.ActualCurrency = request.Market.Currency, "EUR"
		result.Warnings = append(result.Warnings, warning)
	}
	if err := ctx.Err(); err != nil {
		return provider.ProductPage{}, err
	}
	return result, nil
}

func matchesSearchURL(value, query string, page int) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, websiteHost) || u.User != nil || u.Path != "/ca/search" || u.RawPath != "" || u.Fragment != "" {
		return false
	}
	if u.RawQuery == "" {
		return !u.ForceQuery
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(values["search"]) != 1 || values.Get("search") != query {
		return false
	}
	if len(values) == 1 {
		return page == 1
	}
	return len(values) == 2 && len(values["pn"]) == 1 && values.Get("pn") == strconv.Itoa(page)
}

var (
	_ provider.HelpProvider    = implementation{}
	_ provider.ConfigValidator = implementation{}
	_ provider.SearchProvider  = implementation{}
)
