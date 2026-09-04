package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kostyay/ecom/internal/output"
	"github.com/kostyay/ecom/provider"
)

const (
	categorySearchProvider = "provider"
	categorySearchLocal    = "local"
)

// CategoriesInput contains category tree and text search arguments.
type CategoriesInput struct {
	Query        string
	ParentID     string
	Recursive    bool
	Page         int
	PageSet      bool
	PageSize     int
	PageSizeSet  bool
	Refresh      bool
	StaleIfError bool
	Interactive  bool
}

// CategoryItemsInput contains arguments for one category product page.
type CategoryItemsInput struct {
	CategoryID   string
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

// CategoriesResult contains a validated category page and output context.
type CategoriesResult struct {
	ProviderName string
	Market       provider.Market
	Page         provider.CategoryPage
	SearchMethod string
	Metadata     output.Metadata
}

// CategoryItemsResult contains a validated product page and output context.
type CategoryItemsResult struct {
	ProviderName string
	Market       provider.Market
	Page         provider.ProductPage
	Metadata     output.Metadata
}

// Categories lists or searches the selected provider category tree.
func (services *Services) Categories(ctx context.Context, input CategoriesInput) (CategoriesResult, error) {
	help, err := services.selectedProviderHelp(ctx)
	if err != nil {
		return CategoriesResult{}, err
	}
	if input.Query != "" && (input.ParentID != "" || input.Recursive) {
		return CategoriesResult{}, provider.NewError(provider.ErrorCodeInvalidFilter, "category text search cannot use --parent or --recursive", nil)
	}
	page, err := validatePageRequest(input.Page, input.PageSet, input.PageSize, input.PageSizeSet, help.Pagination)
	if err != nil {
		return CategoriesResult{}, err
	}
	collector := newMetadataResourceService(services.Resources, services.Market, provider.CachePolicy{
		Refresh: input.Refresh, StaleIfError: input.StaleIfError,
	}, input.Interactive)
	request := provider.Request{
		Market: services.Market, Pricing: PricingFromConfig(services.Settings.Pricing),
		Cache: collector.cache, Interactive: input.Interactive, Resources: collector,
	}

	var result provider.CategoryPage
	method := ""
	switch {
	case input.Query == "":
		if err := requireCapability(services.Provider, help, provider.CapabilityCategories); err != nil {
			return CategoriesResult{}, err
		}
		result, err = services.Provider.Categories(ctx, provider.CategoryListRequest{
			Request: request, ParentID: input.ParentID, Recursive: input.Recursive, Page: page,
		})
	case services.Provider.Supports(provider.CapabilityCategorySearch) && helpSupports(help, provider.CapabilityCategorySearch):
		method = categorySearchProvider
		result, err = services.Provider.SearchCategories(ctx, provider.CategorySearchRequest{Request: request, Query: input.Query, Page: page})
	default:
		if err := requireCapability(services.Provider, help, provider.CapabilityCategories); err != nil {
			return CategoriesResult{}, err
		}
		method = categorySearchLocal
		result, err = services.Provider.Categories(ctx, provider.CategoryListRequest{Request: request, Recursive: true})
		if err == nil {
			result = filterCategoryPage(result, input.Query, page, help.Pagination)
		}
	}
	if err != nil {
		return CategoriesResult{}, err
	}
	if err := validateCategoryPage(result, help.Pagination); err != nil {
		return CategoriesResult{}, provider.NewError(provider.ErrorCodeInvalidProviderResult, "provider returned invalid category data", err)
	}
	return CategoriesResult{ProviderName: services.Provider.Name(), Market: services.Market, Page: result, SearchMethod: method, Metadata: collector.Metadata()}, nil
}

// CategoryItems lists products in one opaque provider category ID.
func (services *Services) CategoryItems(ctx context.Context, input CategoryItemsInput) (CategoryItemsResult, error) {
	if strings.TrimSpace(input.CategoryID) == "" {
		return CategoryItemsResult{}, provider.NewError(provider.ErrorCodeInvalidFilter, "category ID is required", nil)
	}
	help, err := services.selectedProviderHelp(ctx)
	if err != nil {
		return CategoryItemsResult{}, err
	}
	if err := requireCapability(services.Provider, help, provider.CapabilityCategoryItems); err != nil {
		return CategoryItemsResult{}, err
	}
	filters, sortMode, page, err := validateProductListingArguments(
		input.Filters, input.Sort, input.Page, input.PageSet, input.PageSize, input.PageSizeSet,
		provider.CapabilityCategoryItems, help,
	)
	if err != nil {
		return CategoryItemsResult{}, err
	}
	collector := newMetadataResourceService(services.Resources, services.Market, provider.CachePolicy{
		Refresh: input.Refresh, StaleIfError: input.StaleIfError,
	}, input.Interactive)
	result, err := services.Provider.CategoryItems(ctx, provider.CategoryItemsRequest{
		Market: services.Market, Pricing: PricingFromConfig(services.Settings.Pricing),
		Cache: collector.cache, Interactive: input.Interactive, Resources: collector,
		CategoryID: input.CategoryID, Filters: filters, Sort: sortMode, Page: page,
	})
	if err != nil {
		return CategoryItemsResult{}, err
	}
	if err := validateProductPage(result, help.Pagination); err != nil {
		return CategoryItemsResult{}, provider.NewError(provider.ErrorCodeInvalidProviderResult, "provider returned invalid category product data", err)
	}
	return CategoryItemsResult{ProviderName: services.Provider.Name(), Market: services.Market, Page: result, Metadata: collector.Metadata()}, nil
}

func (services *Services) selectedProviderHelp(ctx context.Context) (provider.Help, error) {
	if services == nil || services.Provider == nil {
		return provider.Help{}, errors.New("application provider services are required")
	}
	result, err := services.Provider.Help(ctx, provider.HelpRequest{
		Market: services.Market, Pricing: PricingFromConfig(services.Settings.Pricing)})
	if err != nil {
		return provider.Help{}, err
	}
	if err := validateSelectedHelp(services.Provider, result.Help); err != nil {
		return provider.Help{}, err
	}
	return result.Help, nil
}

func requireCapability(selected provider.Provider, help provider.Help, capability provider.CapabilityName) error {
	if selected.Supports(capability) && helpSupports(help, capability) {
		return nil
	}
	return provider.NewError(provider.ErrorCodeCapabilityUnavailable, fmt.Sprintf("provider %q does not support capability %q", selected.Name(), capability), nil)
}

func filterCategoryPage(source provider.CategoryPage, query string, requested provider.PageRequest, help *provider.PaginationHelp) provider.CategoryPage {
	query = strings.ToLower(strings.TrimSpace(query))
	items := make([]provider.Category, 0, len(source.Items))
	for _, category := range source.Items {
		if strings.Contains(strings.ToLower(category.Name), query) || strings.Contains(strings.ToLower(category.Path), query) {
			items = append(items, category)
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

func validateCategoryPage(result provider.CategoryPage, pagination *provider.PaginationHelp) error {
	parents := make(map[string]string, len(result.Items))
	for index, category := range result.Items {
		if strings.TrimSpace(category.ID) == "" || strings.TrimSpace(category.Name) == "" {
			return fmt.Errorf("category %d requires an opaque ID and name", index)
		}
		if category.ParentID == category.ID {
			return fmt.Errorf("category %q cannot be its own parent", category.ID)
		}
		if _, exists := parents[category.ID]; exists {
			return fmt.Errorf("category ID %q is duplicated", category.ID)
		}
		parents[category.ID] = category.ParentID
		if err := category.Validate(); err != nil {
			return fmt.Errorf("category %d: %w", index, err)
		}
	}
	states := make(map[string]uint8, len(parents))
	var visit func(string) error
	visit = func(id string) error {
		switch states[id] {
		case 1:
			return fmt.Errorf("category tree contains a cycle at %q", id)
		case 2:
			return nil
		}
		states[id] = 1
		if parent := parents[id]; parent != "" {
			if _, exists := parents[parent]; exists {
				if err := visit(parent); err != nil {
					return err
				}
			}
		}
		states[id] = 2
		return nil
	}
	for id := range parents {
		if err := visit(id); err != nil {
			return err
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

func validateWarnings(warnings []provider.Warning) error {
	for index, warning := range warnings {
		if warning.Code == "" || strings.TrimSpace(warning.Message) == "" {
			return fmt.Errorf("warning %d requires a code and message", index)
		}
		if warning.Code == provider.WarningCodePartialParsing && (warning.FoundCount == nil || warning.ParsedCount == nil || *warning.FoundCount <= *warning.ParsedCount || *warning.ParsedCount < 0) {
			return fmt.Errorf("warning %d has invalid partial parsing counts", index)
		}
	}
	return nil
}
