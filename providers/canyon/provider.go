// Package canyon implements public Canyon bike search for the English/Spain storefront.
// Import this package for its registration side effect.
package canyon

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/providers/internal/shoputil"
)

// Name is the stable Provider identifier.
const Name = "canyon"
const baseURL = "https://www.canyon.com"
const pageSize = 24

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
		Name: Name, DisplayName: "Canyon", Description: "Search Canyon bikes on the English/Spain storefront.",
		Capabilities: []provider.CapabilityHelp{{Name: provider.CapabilitySearch, Supported: true}},
		Search:       &provider.SearchHelp{QueryRequired: true, Syntax: "plain bike model text", Examples: []string{"spectral", "grizl"}, Notes: []string{"Search uses the bikes tab, including outlet bikes. Gear and editorial results are excluded. Filters, custom sorts, item details, and variant selection are unsupported."}},
		Pagination:   &provider.PaginationHelp{Mode: provider.PaginationPageNumber, FirstPage: 1, DefaultPageSize: pageSize, SupportedPageSizes: []int{pageSize}, ReportsTotalItems: true, ReportsTotalPages: true, Notes: []string{"Pages 1 to 1000 map to the site's 24-product offsets. Read has_next before requesting another page."}},
		Markets:      &provider.MarketRestrictions{Countries: []string{"ES"}, Languages: []string{"en"}, Currencies: []string{"EUR"}, Notes: []string{"Use ECOM_MARKET_COUNTRY=ES. Only /en-es/ is supported; prices exclude shipping and optional fees. Other requested currencies produce a currency_unavailable warning.", "Only displayed item prices are returned, never monthly finance payments or net analytics prices. Listing availability does not guarantee a particular size or color."}},
		Access:       &provider.AccessRequirements{Authentication: provider.AuthenticationNone, Browser: provider.BrowserFallback, SupportsCDP: true, SupportsInteractive: true},
		Transport: []provider.TransportNote{
			{Mode: provider.TransportHTTP, UseWhen: "Read the public search HTML."},
			{Mode: provider.TransportBrowser, UseWhen: "Direct HTTP is blocked."},
			{Mode: provider.TransportCDP, UseWhen: "An existing Chrome session is configured and isolated browser access is blocked."},
		},
		Warnings: []provider.HelpWarning{{Code: "site_challenge", Message: "Canyon may restrict automated access; browser fallback must remain on the Spain storefront."}},
	}
	return provider.HelpResult{Help: help}, help.Validate()
}
func (implementation) Search(ctx context.Context, r provider.SearchRequest) (provider.ProductPage, error) {
	page, err := shoputil.ValidateSearch(ctx, r, "ES", pageSize)
	if err != nil {
		return provider.ProductPage{}, err
	}
	response, err := shoputil.Fetch(ctx, r.Request, provider.ResourceRequest{
		Method: http.MethodGet, URL: baseURL + "/en-es/search/",
		Query: []provider.RequestValue{
			{Name: "q", Values: []string{strings.TrimSpace(r.Query)}}, {Name: "searchType", Values: []string{"bikes"}},
			{Name: "start", Values: []string{strconv.Itoa((page - 1) * pageSize)}}, {Name: "sz", Values: []string{strconv.Itoa(pageSize)}},
			{Name: "pn", Values: []string{strconv.Itoa(page - 1)}}, {Name: "searchredirect", Values: []string{"false"}},
		},
		Transport: provider.TransportPolicy{Preferred: []provider.TransportMode{provider.TransportHTTP, provider.TransportBrowser, provider.TransportCDP}},
	})
	if err != nil {
		return provider.ProductPage{}, err
	}
	return parseSearch(response, page, r.Market.Currency)
}
