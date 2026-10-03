// Package ytindustries implements public YT Industries product search.
// Import this package for its registration side effect.
package ytindustries

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/providers/internal/shoputil"
)

// Name is the stable Provider identifier.
const Name = "yt-industries"
const baseURL = "https://www.yt-industries.com"
const pageSize = 24

var accessKeyPattern = regexp.MustCompile(`shopwareAccessToken\s*:\s*"([A-Za-z0-9]+)"`)

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
		Name: Name, DisplayName: "YT Industries", Description: "Search the public German YT Industries catalog in English.",
		Capabilities: []provider.CapabilityHelp{{Name: provider.CapabilitySearch, Supported: true}},
		Search:       &provider.SearchHelp{QueryRequired: true, Syntax: "plain product text", Examples: []string{"capra", "izzo"}, Notes: []string{"Search includes bikes, parts, and clothing. Filters, custom sorts, item details, and variant selection are unsupported."}},
		Pagination:   &provider.PaginationHelp{Mode: provider.PaginationPageNumber, FirstPage: 1, DefaultPageSize: pageSize, SupportedPageSizes: []int{pageSize}, ReportsTotalItems: true, ReportsTotalPages: true, Notes: []string{"Pages 1 to 1000. Read has_next before requesting another page."}},
		Markets:      &provider.MarketRestrictions{Countries: []string{"DE"}, Languages: []string{"en"}, Currencies: []string{"EUR"}, Notes: []string{"The anonymous Store API context must report Germany, English, and EUR. Prices exclude shipping; other requested currencies produce a currency_unavailable warning.", "API decimal prices are rendered with an EUR symbol. Starting prices are marked in provider_data; listing availability does not guarantee a particular size."}},
		Access:       &provider.AccessRequirements{Authentication: provider.AuthenticationNone, Browser: provider.BrowserNone},
		Transport:    []provider.TransportNote{{Mode: provider.TransportHTTP, UseWhen: "Read the public storefront key, validate its anonymous market context, then POST to the Shopware search endpoint."}},
		Warnings:     []provider.HelpWarning{{Code: "public_endpoint", Message: "YT can change or restrict its public Store API."}},
	}
	return provider.HelpResult{Help: help}, help.Validate()
}

func (implementation) Search(ctx context.Context, r provider.SearchRequest) (provider.ProductPage, error) {
	page, err := shoputil.ValidateSearch(ctx, r, "DE", pageSize)
	if err != nil {
		return provider.ProductPage{}, err
	}
	home, err := shoputil.Fetch(ctx, r.Request, provider.ResourceRequest{Method: http.MethodGet, URL: baseURL + "/", Transport: provider.TransportPolicy{Required: provider.TransportHTTP}})
	if err != nil {
		return provider.ProductPage{}, err
	}
	key := accessKeyPattern.FindSubmatch(home.Body)
	if key == nil {
		return provider.ProductPage{}, shoputil.InvalidResult("YT did not publish its storefront access key")
	}
	headers := []provider.RequestValue{
		{Name: "sw-access-key", Values: []string{string(key[1])}, Sensitive: true},
		{Name: "Content-Type", Values: []string{"application/json"}},
	}
	partition := fmt.Sprintf("%x", sha256.Sum256(key[1]))
	resource := provider.ResourceRequest{Method: http.MethodGet, URL: baseURL + "/store-api/context", Headers: headers, CachePartition: partition, Transport: provider.TransportPolicy{Required: provider.TransportHTTP}}
	response, err := shoputil.Fetch(ctx, r.Request, resource)
	if err != nil {
		return provider.ProductPage{}, err
	}
	if !validContext(response.Body) {
		return provider.ProductPage{}, shoputil.InvalidResult("YT did not return the Germany/English/EUR catalog context")
	}
	query := strings.TrimSpace(r.Query)
	body, err := json.Marshal(map[string]any{
		"search": query, "page": page, "limit": pageSize, "total-count-mode": 1,
		"associations": map[string]any{"seoUrls": struct{}{}, "cover": map[string]any{"associations": map[string]any{"media": struct{}{}}}},
	})
	if err != nil {
		return provider.ProductPage{}, err
	}
	resource.Method, resource.URL, resource.Body = http.MethodPost, baseURL+"/store-api/search", provider.RequestBody{Bytes: body}
	response, err = shoputil.Fetch(ctx, r.Request, resource)
	if err != nil {
		return provider.ProductPage{}, err
	}
	return parseSearch(response, query, page, r.Market.Currency)
}

func validContext(body []byte) bool {
	var value struct {
		Currency struct {
			ISOCode string `json:"isoCode"`
		} `json:"currency"`
		Language struct {
			Locale string `json:"localeCode"`
		} `json:"languageInfo"`
		Shipping struct {
			Country struct {
				ISO string `json:"iso"`
			} `json:"country"`
		} `json:"shippingLocation"`
	}
	return json.Unmarshal(body, &value) == nil && value.Currency.ISOCode == "EUR" && value.Language.Locale == "en-GB" && value.Shipping.Country.ISO == "DE"
}
