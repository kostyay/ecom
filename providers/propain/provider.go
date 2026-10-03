// Package propain implements public Propain storefront product search.
// Import this package for its registration side effect.
package propain

import (
	"context"
	"net/http"
	"strings"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/providers/internal/shoputil"
)

// Name is the stable Provider identifier.
const Name = "propain"
const baseURL = "https://www.propain-bikes.com"
const pageSize = 10

type implementation struct{}

func init() { provider.MustRegister(registration()) }
func registration() provider.Registration {
	return provider.Registration{Name: Name, SDKAPIVersion: provider.APIVersion, Implementation: implementation{}, Capabilities: []provider.CapabilityName{provider.CapabilitySearch}}
}
func (implementation) ValidateConfig(config map[string]any) error {
	return shoputil.ValidateConfig(config)
}
func (implementation) Help(context.Context, provider.HelpRequest) (provider.HelpResult, error) {
	help := provider.Help{
		Name: Name, DisplayName: "Propain", Description: "Search Propain's German storefront in English.",
		Capabilities: []provider.CapabilityHelp{{Name: provider.CapabilitySearch, Supported: true}},
		Search:       &provider.SearchHelp{QueryRequired: true, Syntax: "plain product text", Examples: []string{"tyee", "spindrift"}, Notes: []string{"Search includes bikes, framesets, parts, and clothing. Filters, custom sorts, item details, and variant selection are unsupported."}},
		Pagination:   &provider.PaginationHelp{Mode: provider.PaginationPageNumber, FirstPage: 1, DefaultPageSize: pageSize, SupportedPageSizes: []int{pageSize}, Notes: []string{"Pages 1 to 1000. Native WordPress pages return up to ten products. Totals are not reported; read has_next before requesting another page."}},
		Markets:      &provider.MarketRestrictions{Countries: []string{"DE"}, Languages: []string{"en"}, Currencies: []string{"EUR"}, Notes: []string{"Requests select Germany explicitly. Server-rendered EUR prices exclude shipping. Other requested currencies produce a currency_unavailable warning.", "Configurable bikes can show a starting price, marked in provider_data. It is not a quote for a selected configuration. Stock is the listing status, not size-specific availability."}},
		Access:       &provider.AccessRequirements{Authentication: provider.AuthenticationNone, Browser: provider.BrowserNone},
		Transport:    []provider.TransportNote{{Mode: provider.TransportHTTP, UseWhen: "Read public WooCommerce search HTML, not the Store API's different product and price catalog."}},
		Warnings:     []provider.HelpWarning{{Code: "catalog_price", Message: "Configured builds and other delivery countries may have different prices."}},
	}
	return provider.HelpResult{Help: help}, help.Validate()
}
func (implementation) Search(ctx context.Context, r provider.SearchRequest) (provider.ProductPage, error) {
	page, err := shoputil.ValidateSearch(ctx, r, "DE", pageSize)
	if err != nil {
		return provider.ProductPage{}, err
	}
	path := "/en/"
	if page > 1 {
		path = fmtPagePath(page)
	}
	response, err := shoputil.Fetch(ctx, r.Request, provider.ResourceRequest{
		Method: http.MethodGet, URL: baseURL + path,
		Query:     []provider.RequestValue{{Name: "s", Values: []string{strings.TrimSpace(r.Query)}}, {Name: "post_type", Values: []string{"product"}}, {Name: "wcpbc-manual-country", Values: []string{"DE"}}},
		Transport: provider.TransportPolicy{Required: provider.TransportHTTP},
	})
	if err != nil {
		return provider.ProductPage{}, err
	}
	return parseSearch(response, page, r.Market.Currency)
}
