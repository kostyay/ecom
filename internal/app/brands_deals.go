package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/kostyay/ecom/internal/output"
	"github.com/kostyay/ecom/provider"
)

const (
	brandSearchProvider = "provider"
	brandSearchLocal    = "local"
)

// BrandsInput contains brand list and text search arguments.
type BrandsInput struct {
	Query        string
	Page         int
	PageSet      bool
	PageSize     int
	PageSizeSet  bool
	Refresh      bool
	StaleIfError bool
	Interactive  bool
}

// BrandItemsInput contains arguments for one brand product page.
type BrandItemsInput struct {
	BrandID      string
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

// DealsInput contains arguments for one native deal page.
type DealsInput struct {
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

// BrandsResult contains a validated brand page and output context.
type BrandsResult struct {
	ProviderName string
	Market       provider.Market
	Page         provider.BrandPage
	SearchMethod string
	Metadata     output.Metadata
}

// BrandItemsResult contains a validated brand product page and output context.
type BrandItemsResult struct {
	ProviderName string
	Market       provider.Market
	Page         provider.ProductPage
	Metadata     output.Metadata
}

// DealsResult contains a validated native deal page and output context.
type DealsResult struct {
	ProviderName string
	Market       provider.Market
	Page         provider.DealPage
	Metadata     output.Metadata
}

// Brands lists or searches brands from the selected provider.
func (services *Services) Brands(ctx context.Context, input BrandsInput) (BrandsResult, error) {
	help, err := services.selectedProviderHelp(ctx)
	if err != nil {
		return BrandsResult{}, err
	}
	page, err := validatePageRequest(input.Page, input.PageSet, input.PageSize, input.PageSizeSet, help.Pagination)
	if err != nil {
		return BrandsResult{}, err
	}
	collector := newMetadataResourceService(services.Resources, services.Market, provider.CachePolicy{
		Refresh: input.Refresh, StaleIfError: input.StaleIfError,
	}, input.Interactive)
	request := provider.Request{
		Market: services.Market, Pricing: PricingFromConfig(services.Settings.Pricing),
		Cache: collector.cache, Interactive: input.Interactive, Resources: collector,
	}

	var result provider.BrandPage
	method := ""
	switch {
	case input.Query == "":
		if err := requireCapability(services.Provider, help, provider.CapabilityBrands); err != nil {
			return BrandsResult{}, err
		}
		result, err = services.Provider.Brands(ctx, provider.BrandListRequest{Request: request, Page: page})
	case services.Provider.Supports(provider.CapabilityBrandSearch) && helpSupports(help, provider.CapabilityBrandSearch):
		method = brandSearchProvider
		result, err = services.Provider.SearchBrands(ctx, provider.BrandSearchRequest{Request: request, Query: input.Query, Page: page})
	default:
		if err := requireCapability(services.Provider, help, provider.CapabilityBrands); err != nil {
			return BrandsResult{}, err
		}
		method = brandSearchLocal
		result, err = services.Provider.Brands(ctx, provider.BrandListRequest{Request: request})
		if err == nil {
			result = filterBrandPage(result, input.Query, page, help.Pagination)
		}
	}
	if err != nil {
		return BrandsResult{}, err
	}
	if err := validateBrandPage(result, help.Pagination); err != nil {
		return BrandsResult{}, provider.NewError(provider.ErrorCodeInvalidProviderResult, "provider returned invalid brand data", err)
	}
	return BrandsResult{ProviderName: services.Provider.Name(), Market: services.Market, Page: result, SearchMethod: method, Metadata: collector.Metadata()}, nil
}

// BrandItems lists products for one opaque provider brand ID.
func (services *Services) BrandItems(ctx context.Context, input BrandItemsInput) (BrandItemsResult, error) {
	if strings.TrimSpace(input.BrandID) == "" {
		return BrandItemsResult{}, provider.NewError(provider.ErrorCodeInvalidFilter, "brand ID is required", nil)
	}
	help, err := services.selectedProviderHelp(ctx)
	if err != nil {
		return BrandItemsResult{}, err
	}
	if err := requireCapability(services.Provider, help, provider.CapabilityBrandItems); err != nil {
		return BrandItemsResult{}, err
	}
	filters, sortMode, page, err := validateProductListingArguments(input.Filters, input.Sort, input.Page, input.PageSet, input.PageSize, input.PageSizeSet, provider.CapabilityBrandItems, help)
	if err != nil {
		return BrandItemsResult{}, err
	}
	collector := newMetadataResourceService(services.Resources, services.Market, provider.CachePolicy{Refresh: input.Refresh, StaleIfError: input.StaleIfError}, input.Interactive)
	result, err := services.Provider.BrandItems(ctx, provider.BrandItemsRequest{
		Market: services.Market, Pricing: PricingFromConfig(services.Settings.Pricing),
		Cache: collector.cache, Interactive: input.Interactive, Resources: collector,
		BrandID: input.BrandID, Filters: filters, Sort: sortMode, Page: page,
	})
	if err != nil {
		return BrandItemsResult{}, err
	}
	if err := validateProductPage(result, help.Pagination); err != nil {
		return BrandItemsResult{}, provider.NewError(provider.ErrorCodeInvalidProviderResult, "provider returned invalid brand product data", err)
	}
	return BrandItemsResult{ProviderName: services.Provider.Name(), Market: services.Market, Page: result, Metadata: collector.Metadata()}, nil
}

// Deals lists products for which the provider declares a shown reduction.
func (services *Services) Deals(ctx context.Context, input DealsInput) (DealsResult, error) {
	help, err := services.selectedProviderHelp(ctx)
	if err != nil {
		return DealsResult{}, err
	}
	if err := requireCapability(services.Provider, help, provider.CapabilityDeals); err != nil {
		return DealsResult{}, err
	}
	filters, sortMode, page, err := validateProductListingArguments(input.Filters, input.Sort, input.Page, input.PageSet, input.PageSize, input.PageSizeSet, provider.CapabilityDeals, help)
	if err != nil {
		return DealsResult{}, err
	}
	collector := newMetadataResourceService(services.Resources, services.Market, provider.CachePolicy{Refresh: input.Refresh, StaleIfError: input.StaleIfError}, input.Interactive)
	result, err := services.Provider.Deals(ctx, provider.DealsRequest{
		Market: services.Market, Pricing: PricingFromConfig(services.Settings.Pricing),
		Cache: collector.cache, Interactive: input.Interactive, Resources: collector,
		Filters: filters, Sort: sortMode, Page: page,
	})
	if err != nil {
		return DealsResult{}, err
	}
	if err := validateDealPage(result, help.Pagination); err != nil {
		return DealsResult{}, provider.NewError(provider.ErrorCodeInvalidProviderResult, "provider returned invalid deal data", err)
	}
	return DealsResult{ProviderName: services.Provider.Name(), Market: services.Market, Page: result, Metadata: collector.Metadata()}, nil
}

func validateProductListingArguments(filters []string, sortValue string, page int, pageSet bool, pageSize int, pageSizeSet bool, capability provider.CapabilityName, help provider.Help) ([]provider.Filter, *provider.Sort, provider.PageRequest, error) {
	parsedFilters, err := validateFilters(filters, help.Filters, capability)
	if err != nil {
		return nil, nil, provider.PageRequest{}, err
	}
	sortMode, err := validateSort(sortValue, help.SortModes, capability)
	if err != nil {
		return nil, nil, provider.PageRequest{}, err
	}
	pageRequest, err := validatePageRequest(page, pageSet, pageSize, pageSizeSet, help.Pagination)
	if err != nil {
		return nil, nil, provider.PageRequest{}, err
	}
	return parsedFilters, sortMode, pageRequest, nil
}

func filterBrandPage(source provider.BrandPage, query string, requested provider.PageRequest, help *provider.PaginationHelp) provider.BrandPage {
	query = strings.ToLower(strings.TrimSpace(query))
	items := make([]provider.Brand, 0, len(source.Items))
	for _, brand := range source.Items {
		if strings.Contains(strings.ToLower(brand.Name), query) {
			items = append(items, brand)
		}
	}
	number, size := localPage(requested, help, len(items))
	total := len(items)
	totalPages := (total + size - 1) / size
	start := (number - 1) * size
	end := min(start+size, total)
	if start >= total {
		items = nil
	} else {
		items = items[start:end]
	}
	hasNext := number < totalPages
	source.Items = items
	source.Page = provider.PageInfo{Number: number, Size: size, TotalItems: &total, TotalPages: &totalPages, HasNext: &hasNext}
	return source
}

func localPage(requested provider.PageRequest, help *provider.PaginationHelp, itemCount int) (int, int) {
	number, size := requested.Number, requested.Size
	if help != nil && help.Mode == provider.PaginationPageNumber {
		if number == 0 {
			number = help.FirstPage
		}
		if size == 0 {
			size = help.DefaultPageSize
		}
	}
	if number == 0 {
		number = 1
	}
	if size == 0 {
		size = max(itemCount, 1)
	}
	return number, size
}

func validateBrandPage(result provider.BrandPage, pagination *provider.PaginationHelp) error {
	seen := make(map[string]struct{}, len(result.Items))
	for index, brand := range result.Items {
		if strings.TrimSpace(brand.ID) == "" || strings.TrimSpace(brand.Name) == "" {
			return fmt.Errorf("brand %d requires an opaque ID and name", index)
		}
		if _, exists := seen[brand.ID]; exists {
			return fmt.Errorf("brand ID %q is duplicated", brand.ID)
		}
		seen[brand.ID] = struct{}{}
		if err := brand.Validate(); err != nil {
			return fmt.Errorf("brand %d: %w", index, err)
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

func validateDealPage(result provider.DealPage, pagination *provider.PaginationHelp) error {
	for index, deal := range result.Items {
		if err := deal.Validate(); err != nil {
			return fmt.Errorf("deal %d: %w", index, err)
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
