package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kostyay/ecom/internal/output"
	"github.com/kostyay/ecom/provider"
)

// FiltersInput narrows provider filter discovery to one listing context.
type FiltersInput struct {
	Capability   provider.CapabilityName
	CategoryID   string
	BrandID      string
	Refresh      bool
	StaleIfError bool
	Interactive  bool
}

// FiltersResult contains validated filter discovery data and output context.
type FiltersResult struct {
	ProviderName string
	Market       provider.Market
	Result       provider.FiltersResult
	Metadata     output.Metadata
}

// Filters gets the filter and sort definitions for one provider context.
func (services *Services) Filters(ctx context.Context, input FiltersInput) (FiltersResult, error) {
	if services == nil || services.Provider == nil {
		return FiltersResult{}, errors.New("application provider services are required")
	}
	if input.CategoryID != "" && input.BrandID != "" {
		return FiltersResult{}, provider.NewError(provider.ErrorCodeInvalidFilter, "category and brand contexts cannot be combined", nil)
	}
	if input.Capability != "" && !slices.Contains([]provider.CapabilityName{
		provider.CapabilitySearch, provider.CapabilityCategoryItems,
		provider.CapabilityBrandItems, provider.CapabilityDeals,
	}, input.Capability) {
		return FiltersResult{}, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("capability %q does not use product filters", input.Capability), nil)
	}

	help, err := services.selectedProviderHelp(ctx)
	if err != nil {
		return FiltersResult{}, err
	}
	if err := requireCapability(services.Provider, help, provider.CapabilityFilters); err != nil {
		return FiltersResult{}, err
	}
	collector := newMetadataResourceService(services.Resources, services.Market, provider.CachePolicy{
		Refresh: input.Refresh, StaleIfError: input.StaleIfError,
	}, input.Interactive)
	result, err := services.Provider.Filters(ctx, provider.FiltersRequest{
		Market: services.Market, Pricing: PricingFromConfig(services.Settings.Pricing),
		Cache: collector.cache, Interactive: input.Interactive, Resources: collector,
		Capability: input.Capability, CategoryID: input.CategoryID, BrandID: input.BrandID,
	})
	if err != nil {
		return FiltersResult{}, err
	}
	resultHelp := provider.Help{Name: services.Provider.Name(), Filters: result.Filters, SortModes: result.SortModes}
	if err := resultHelp.Validate(); err != nil {
		return FiltersResult{}, provider.NewError(provider.ErrorCodeInvalidProviderResult, "provider returned invalid filter data", err)
	}
	if err := validateProviderData(result.ProviderData); err != nil {
		return FiltersResult{}, provider.NewError(provider.ErrorCodeInvalidProviderResult, "provider returned invalid filter data", err)
	}
	if err := validateWarnings(result.Warnings); err != nil {
		return FiltersResult{}, provider.NewError(provider.ErrorCodeInvalidProviderResult, "provider returned invalid filter data", err)
	}
	if err := filterDefinitionsMatchHelp(result, help); err != nil {
		return FiltersResult{}, provider.NewError(provider.ErrorCodeInvalidProviderResult, "provider returned undocumented filter data", err)
	}
	return FiltersResult{
		ProviderName: services.Provider.Name(), Market: services.Market,
		Result: result, Metadata: collector.Metadata(),
	}, nil
}

func filterDefinitionsMatchHelp(result provider.FiltersResult, help provider.Help) error {
	filters := make(map[string]struct{}, len(help.Filters))
	for _, definition := range help.Filters {
		filters[definition.Key] = struct{}{}
	}
	for _, definition := range result.Filters {
		if _, exists := filters[definition.Key]; !exists {
			return fmt.Errorf("filter %q is not in provider help", definition.Key)
		}
	}
	sorts := make(map[string]struct{}, len(help.SortModes))
	for _, mode := range help.SortModes {
		sorts[mode.Value] = struct{}{}
	}
	for _, mode := range result.SortModes {
		if _, exists := sorts[mode.Value]; !exists {
			return fmt.Errorf("sort mode %q is not in provider help", mode.Value)
		}
	}
	return nil
}
