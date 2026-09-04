package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/kostyay/ecom/internal/output"
	"github.com/kostyay/ecom/provider"
)

var decimalFilterPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)

// SearchInput contains provider-neutral product search arguments.
type SearchInput struct {
	Query        string
	Filters      []string
	Sort         string
	Page         int
	PageSet      bool
	PageSize     int
	PageSizeSet  bool
	Refresh      bool
	StaleIfError bool
	Interactive  bool
}

// SearchResult contains a validated provider result and its output context.
type SearchResult struct {
	ProviderName string
	Market       provider.Market
	Page         provider.ProductPage
	Metadata     output.Metadata
}

// Search validates common search arguments against offline provider help and
// calls the selected provider. The provider remains responsible for its
// site-specific value translation and validation.
func (services *Services) Search(ctx context.Context, input SearchInput) (SearchResult, error) {
	if services == nil || services.Provider == nil {
		return SearchResult{}, errors.New("application provider services are required")
	}
	if input.Query == "" {
		return SearchResult{}, provider.NewError(provider.ErrorCodeInvalidFilter, "search query is required", nil)
	}

	helpResult, err := services.Provider.Help(ctx, provider.HelpRequest{
		Market: services.Market, Pricing: PricingFromConfig(services.Settings.Pricing)})
	if err != nil {
		return SearchResult{}, err
	}
	if err := validateSelectedHelp(services.Provider, helpResult.Help); err != nil {
		return SearchResult{}, err
	}
	if !services.Provider.Supports(provider.CapabilitySearch) || !helpSupports(helpResult.Help, provider.CapabilitySearch) {
		return SearchResult{}, provider.NewError(
			provider.ErrorCodeCapabilityUnavailable,
			fmt.Sprintf("provider %q does not support capability %q", services.Provider.Name(), provider.CapabilitySearch),
			nil,
		)
	}

	filters, err := validateFilters(input.Filters, helpResult.Help.Filters, provider.CapabilitySearch)
	if err != nil {
		return SearchResult{}, err
	}
	sort, err := validateSort(input.Sort, helpResult.Help.SortModes, provider.CapabilitySearch)
	if err != nil {
		return SearchResult{}, err
	}
	page, err := validatePageRequest(input.Page, input.PageSet, input.PageSize, input.PageSizeSet, helpResult.Help.Pagination)
	if err != nil {
		return SearchResult{}, err
	}

	collector := newMetadataResourceService(services.Resources, services.Market, provider.CachePolicy{
		Refresh: input.Refresh, StaleIfError: input.StaleIfError,
	}, input.Interactive)
	request := provider.SearchRequest{
		Market: services.Market, Pricing: PricingFromConfig(services.Settings.Pricing),
		Cache: collector.cache, Interactive: input.Interactive, Resources: collector,
		Query: input.Query, Filters: filters, Sort: sort, Page: page,
	}
	result, err := services.Provider.Search(ctx, request)
	if err != nil {
		return SearchResult{}, err
	}
	if err := validateProductPage(result, helpResult.Help.Pagination); err != nil {
		return SearchResult{}, provider.NewError(
			provider.ErrorCodeInvalidProviderResult,
			"provider returned invalid product search data",
			err,
		)
	}

	return SearchResult{
		ProviderName: services.Provider.Name(), Market: services.Market, Page: result, Metadata: collector.Metadata(),
	}, nil
}

func validateSelectedHelp(selected provider.Provider, help provider.Help) error {
	if err := help.Validate(); err != nil {
		return provider.NewError(provider.ErrorCodeInvalidProviderResult, "provider returned invalid help data", err)
	}
	if help.Name != selected.Name() {
		return provider.NewError(provider.ErrorCodeInvalidProviderResult, "provider help name does not match the selected provider", nil)
	}
	for _, capability := range help.Capabilities {
		if capability.Supported != selected.Supports(capability.Name) {
			return provider.NewError(provider.ErrorCodeInvalidProviderResult, "provider help capabilities do not match registration", nil)
		}
	}
	return nil
}

func helpSupports(help provider.Help, capability provider.CapabilityName) bool {
	for _, value := range help.Capabilities {
		if value.Name == capability {
			return value.Supported
		}
	}
	return false
}

func validateFilters(values []string, definitions []provider.FilterDefinition, capability provider.CapabilityName) ([]provider.Filter, error) {
	available := make(map[string]provider.FilterDefinition, len(definitions))
	for _, definition := range definitions {
		if appliesTo(definition.AppliesTo, capability) {
			available[definition.Key] = definition
		}
	}

	seen := make(map[string]struct{}, len(values))
	result := make([]provider.Filter, 0, len(values))
	for _, raw := range values {
		key, value, found := strings.Cut(raw, "=")
		if !found || key == "" || value == "" {
			return nil, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("filter %q must use key=value", raw), nil)
		}
		definition, ok := available[key]
		if !ok {
			return nil, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("filter %q is not supported for %s", key, capability), nil)
		}
		if _, duplicate := seen[key]; duplicate && !definition.Repeatable {
			return nil, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("filter %q cannot be repeated", key), nil)
		}
		if err := validateFilterValue(definition, value); err != nil {
			return nil, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("filter %q has invalid value %q", key, value), err)
		}
		seen[key] = struct{}{}
		result = append(result, provider.Filter{Key: key, Value: value})
	}
	return result, nil
}

func validateFilterValue(definition provider.FilterDefinition, value string) error {
	switch definition.Type {
	case provider.FilterTypeString:
		return nil
	case provider.FilterTypeBoolean:
		if value != "true" && value != "false" {
			return errors.New("value must be true or false")
		}
	case provider.FilterTypeInteger:
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return errors.New("value must be an integer")
		}
	case provider.FilterTypeDecimal:
		if !decimalFilterPattern.MatchString(value) {
			return errors.New("value must be a decimal number")
		}
	case provider.FilterTypeEnum:
		for _, allowed := range definition.AllowedValues {
			if value == allowed.Value {
				return nil
			}
		}
		return errors.New("value is not in the allowed set")
	default:
		return errors.New("filter type is unknown")
	}
	return nil
}

func validateSort(value string, modes []provider.SortMode, capability provider.CapabilityName) (*provider.Sort, error) {
	if value == "" {
		return nil, nil
	}
	for _, mode := range modes {
		if mode.Value == value && appliesTo(mode.AppliesTo, capability) {
			return &provider.Sort{Value: value}, nil
		}
	}
	return nil, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("sort mode %q is not supported for %s", value, capability), nil)
}

func validatePageRequest(number int, numberSet bool, size int, sizeSet bool, help *provider.PaginationHelp) (provider.PageRequest, error) {
	page := provider.PageRequest{Number: number, Size: size}
	if !numberSet && !sizeSet {
		return page, nil
	}
	if help == nil || help.Mode != provider.PaginationPageNumber {
		return provider.PageRequest{}, provider.NewError(provider.ErrorCodeInvalidFilter, "provider does not support page-number pagination", nil)
	}
	if numberSet && number < help.FirstPage {
		return provider.PageRequest{}, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("page must be at least %d", help.FirstPage), nil)
	}
	if sizeSet && !slices.Contains(help.SupportedPageSizes, size) {
		return provider.PageRequest{}, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("page size %d is not supported", size), nil)
	}
	return page, nil
}

func appliesTo(values []provider.CapabilityName, capability provider.CapabilityName) bool {
	return len(values) == 0 || slices.Contains(values, capability)
}

func validateProductPage(result provider.ProductPage, pagination *provider.PaginationHelp) error {
	for index, item := range result.Items {
		if err := item.Validate(); err != nil {
			return fmt.Errorf("item %d: %w", index, err)
		}
	}
	if err := validatePageInfo(result.Page, pagination); err != nil {
		return fmt.Errorf("page: %w", err)
	}
	if err := validateProviderData(result.ProviderData); err != nil {
		return err
	}
	return validateWarnings(result.Warnings)
}

func validatePageInfo(page provider.PageInfo, help *provider.PaginationHelp) error {
	if page.Number < 0 || page.Size < 0 {
		return errors.New("number and size must not be negative")
	}
	if page.TotalItems != nil && *page.TotalItems < 0 || page.TotalPages != nil && *page.TotalPages < 0 {
		return errors.New("totals must not be negative")
	}
	if err := validateProviderData(page.ProviderData); err != nil {
		return err
	}
	if help == nil || help.Mode != provider.PaginationPageNumber {
		return nil
	}
	if page.Number < help.FirstPage || page.Size <= 0 {
		return errors.New("number or size is outside provider pagination rules")
	}
	if len(help.SupportedPageSizes) > 0 && !slices.Contains(help.SupportedPageSizes, page.Size) {
		return errors.New("size is not supported")
	}
	if help.ReportsTotalItems && page.TotalItems == nil || help.ReportsTotalPages && page.TotalPages == nil {
		return errors.New("provider omitted a declared page total")
	}
	if page.TotalPages != nil && *page.TotalPages != 0 && page.Number > *page.TotalPages {
		return errors.New("number exceeds total pages")
	}
	return nil
}

func validateProviderData(data provider.Data) error {
	for namespace, value := range data {
		if strings.TrimSpace(namespace) == "" {
			return errors.New("provider data namespace is required")
		}
		if !json.Valid(value) {
			return fmt.Errorf("provider data %q must contain valid JSON", namespace)
		}
	}
	return nil
}

type metadataResourceService struct {
	next        provider.ResourceService
	market      provider.Market
	cache       provider.CachePolicy
	interactive bool
	mu          sync.Mutex
	resources   []output.ResourceMetadata
}

func newMetadataResourceService(next provider.ResourceService, market provider.Market, cache provider.CachePolicy, interactive bool) *metadataResourceService {
	return &metadataResourceService{next: next, market: market, cache: cache, interactive: interactive}
}

func (service *metadataResourceService) Fetch(ctx context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
	if service.next == nil {
		return provider.ResourceResponse{}, errors.New("application resource service is required")
	}
	request.Market = service.market
	request.Cache = service.cache
	request.Interactive = service.interactive
	response, err := service.next.Fetch(ctx, request)
	service.mu.Lock()
	service.resources = append(service.resources, output.ResourceMetadata{Cache: response.Cache, Attempts: response.Attempts})
	service.mu.Unlock()
	return response, err
}

func (service *metadataResourceService) Metadata() output.Metadata {
	service.mu.Lock()
	defer service.mu.Unlock()
	return output.Metadata{Resources: append([]output.ResourceMetadata(nil), service.resources...)}
}

var _ provider.ResourceService = (*metadataResourceService)(nil)
