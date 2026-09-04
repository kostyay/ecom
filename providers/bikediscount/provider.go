// Package bikediscount implements the Bike-Discount commerce provider.
// Import this package for its registration side effect.
package bikediscount

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/kostyay/ecom/provider"
)

const (
	// Name is the stable provider identifier.
	Name            = "bike-discount"
	defaultPageSize = 48
)

type implementation struct{}

func init() {
	provider.MustRegister(registration())
}

// registration is the single definition of the compiled provider contract.
// The offline conformance suite uses it directly so that its capability cases
// cannot drift from the provider that a blank import registers.
func registration() provider.Registration {
	return provider.Registration{
		Name:           Name,
		SDKAPIVersion:  provider.APIVersion,
		Implementation: implementation{},
		Capabilities: []provider.CapabilityName{
			provider.CapabilitySearch,
			provider.CapabilityCategories,
			provider.CapabilityCategoryItems,
			provider.CapabilityBrands,
			provider.CapabilityBrandItems,
			provider.CapabilityDeals,
			provider.CapabilityFilters,
			provider.CapabilityItem,
			provider.CapabilityVariantSelection,
		},
	}
}

// ValidateConfig validates the Bike-Discount configuration block.
func (implementation) ValidateConfig(configuration map[string]any) error {
	for key := range configuration {
		if key != "page_size" {
			return fmt.Errorf("unknown setting %q", key)
		}
	}
	if value, exists := configuration["page_size"]; exists {
		pageSize, ok := integer(value)
		if !ok || pageSize != defaultPageSize {
			return fmt.Errorf("page_size must be %d", defaultPageSize)
		}
	}
	return nil
}

func integer(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int8:
		return int64(number), true
	case int16:
		return int64(number), true
	case int32:
		return int64(number), true
	case int64:
		return number, true
	case uint:
		if uint64(number) <= math.MaxInt64 {
			return int64(number), true
		}
	case uint8:
		return int64(number), true
	case uint16:
		return int64(number), true
	case uint32:
		return int64(number), true
	case uint64:
		if number <= math.MaxInt64 {
			return int64(number), true
		}
	case float64:
		if number == math.Trunc(number) && number >= math.MinInt64 && number <= math.MaxInt64 {
			return int64(number), true
		}
	case json.Number:
		parsed, err := number.Int64()
		return parsed, err == nil
	}
	return 0, false
}

func (implementation) Help(_ context.Context, request provider.HelpRequest) (provider.HelpResult, error) {
	if err := validatePricingPolicy(request.Pricing); err != nil {
		return provider.HelpResult{}, err
	}
	if request.Market != (provider.Market{}) {
		if err := request.Market.Validate(); err != nil {
			return provider.HelpResult{}, fmt.Errorf("validate market: %w", err)
		}
	}
	help := provider.Help{
		Name:        Name,
		DisplayName: "Bike-Discount",
		Description: "Find products on the Bike-Discount website.",
		Capabilities: []provider.CapabilityHelp{
			{
				Name: provider.CapabilitySearch, Supported: true,
				Description: "Search products with the current verified storefront request.",
				Notes:       []string{"The search form uses the search query parameter. An exact category term can redirect to its canonical category page."},
			},
			supported(provider.CapabilityCategories, "List roots from llms.txt and traverse verified canonical category links."),
			unsupported(provider.CapabilityCategorySearch, "Category text search is planned over the cached category tree."),
			supported(provider.CapabilityCategoryItems, "List product summaries from one category page."),
			supported(provider.CapabilityBrands, "List the complete alphabetical brand index for navigation and local text search."),
			unsupported(provider.CapabilityBrandSearch, "No native brand text-search request was verified. The CLI searches the complete brand index locally without case sensitivity."),
			supported(provider.CapabilityBrandItems, "List product summaries from one canonical brand slug."),
			{
				Name: provider.CapabilityDeals, Supported: true,
				Description: "List products with provider-shown reductions from the stable bike sale page.",
				Notes: []string{
					"The source is https://www.bike-discount.de/en/bike/sale. Temporary campaign pages are not used.",
					"A result is a deal only when its listing card shows an RRP, discount amount, or discount percent. The provider does not estimate discounts or deal scores.",
					"This operation covers the principal bike sale listing. It does not combine the separate outdoor, running, e-bike, or special-deal listings.",
				},
			},
			supported(provider.CapabilityFilters, "List the verified filter keys and sort modes for product listings."),
			supported(provider.CapabilityItem, "Get item details by a complete English Bike-Discount URL or a displayed numeric item number."),
			{
				Name: provider.CapabilityVariantSelection, Supported: true,
				Description: "Select an exact visible variant with the displayed label and value.",
				Notes:       []string{"Use --variant Label=Value exactly as shown by the item result. Internal website variant IDs are not public contracts."},
			},
		},
		Filters:   filterDefinitions(),
		SortModes: sortDefinitions(),
		Pagination: &provider.PaginationHelp{
			Mode: provider.PaginationPageNumber, FirstPage: 1,
			DefaultPageSize: defaultPageSize, SupportedPageSizes: []int{defaultPageSize},
			Notes: []string{"Page size 48 is the only verified value."},
		},
		Search: &provider.SearchHelp{
			QueryRequired: true,
			Syntax:        "plain product text",
			Examples:      []string{"powertube"},
			Notes:         []string{"Search uses the current verified search query parameter. An exact category term can redirect to its canonical category page."},
		},
		Markets: &provider.MarketRestrictions{
			Countries: []string{"DE"}, Languages: []string{"en"}, Currencies: []string{"EUR"},
			Notes: []string{
				"Other market selections need a captured working session. No currency conversion is done.",
				"Prices contain the displayed item price only. Shipping costs are excluded and cannot be requested from this provider.",
			},
		},
		Access: &provider.AccessRequirements{
			Authentication: provider.AuthenticationNone, Browser: provider.BrowserFallback,
			SupportsCDP: true, SupportsInteractive: true,
			Notes: []string{"Cloudflare can block catalog pages. Direct HTTP is tried before browser and CDP transport."},
		},
		Transport: []provider.TransportNote{
			{Mode: provider.TransportHTTP, UseWhen: "The public page is available without a browser."},
			{Mode: provider.TransportBrowser, UseWhen: "Direct HTTP is blocked or the page needs JavaScript."},
			{Mode: provider.TransportCDP, UseWhen: "An existing Chrome session is configured and isolated browser access is blocked."},
		},
		Warnings: []provider.HelpWarning{
			{Code: "site_challenge", Message: "Cloudflare can require manual browser action."},
			{Code: "market_unverified", Message: "Only the DE/en/EUR market is verified."},
		},
		ProviderData: provider.Data{
			Name: json.RawMessage(`{"base_url":"https://www.bike-discount.de","shipping_costs_included":false}`),
		},
	}
	if err := help.Validate(); err != nil {
		return provider.HelpResult{}, fmt.Errorf("validate compiled help: %w", err)
	}
	return provider.HelpResult{Help: help}, nil
}

func validatePricingPolicy(policy provider.PricingPolicy) error {
	if policy.IncludeShipping {
		return provider.NewError(
			provider.ErrorCodeInvalidProviderConfig,
			"Bike-Discount does not support prices that include shipping",
			nil,
		)
	}
	return nil
}

func (implementation) Filters(_ context.Context, request provider.FiltersRequest) (provider.FiltersResult, error) {
	if err := validatePricingPolicy(request.Pricing); err != nil {
		return provider.FiltersResult{}, err
	}
	filters := filterDefinitions()
	sorts := sortDefinitions()
	if request.Capability != "" {
		filters = slices.DeleteFunc(filters, func(definition provider.FilterDefinition) bool {
			return !slices.Contains(definition.AppliesTo, request.Capability)
		})
		sorts = slices.DeleteFunc(sorts, func(definition provider.SortMode) bool {
			return !slices.Contains(definition.AppliesTo, request.Capability)
		})
	}
	return provider.FiltersResult{Filters: filters, SortModes: sorts}, nil
}

func filterDefinitions() []provider.FilterDefinition {
	return []provider.FilterDefinition{
		{
			Key: "manufacturer", Type: provider.FilterTypeString,
			Description: "Use one manufacturer ID exposed by the target product listing.",
			AppliesTo:   []provider.CapabilityName{provider.CapabilitySearch, provider.CapabilityCategoryItems, provider.CapabilityBrandItems, provider.CapabilityDeals},
			Notes:       []string{"The value must be a 32-character website ID from the current target listing. Do not reuse an ID from another listing."},
		},
		{
			Key: "properties", Type: provider.FilterTypeString, Repeatable: true,
			Description: "Use one or more property IDs exposed by the selected category page.",
			AppliesTo:   []provider.CapabilityName{provider.CapabilitySearch, provider.CapabilityCategoryItems, provider.CapabilityBrandItems, provider.CapabilityDeals},
			Notes:       []string{"Repeat this filter for each 32-character website ID from the current target listing. The provider joins values with |."},
		},
	}
}

func sortDefinitions() []provider.SortMode {
	return []provider.SortMode{{
		Value: "standard", Label: "Standard",
		Description: "Use the verified current website order.",
		AppliesTo:   []provider.CapabilityName{provider.CapabilitySearch, provider.CapabilityCategoryItems, provider.CapabilityBrandItems, provider.CapabilityDeals},
	}}
}

func supported(name provider.CapabilityName, description string) provider.CapabilityHelp {
	return provider.CapabilityHelp{Name: name, Supported: true, Description: strings.TrimSpace(description)}
}

func unsupported(name provider.CapabilityName, note string) provider.CapabilityHelp {
	return provider.CapabilityHelp{
		Name: name, Supported: false, Description: "Not implemented in this provider scaffold.",
		Notes: []string{strings.TrimSpace(note)},
	}
}
