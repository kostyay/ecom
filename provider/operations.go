package provider

import (
	"context"
	"fmt"
)

// Request contains values that apply to all provider operations.
type Request struct {
	Market      Market          `json:"market"`
	Pricing     PricingPolicy   `json:"pricing"`
	Cache       CachePolicy     `json:"cache,omitempty"`
	Interactive bool            `json:"interactive,omitempty"`
	Resources   ResourceService `json:"-"`
}

// PricingPolicy selects the price components that a provider must return.
// A provider must reject an unsupported policy instead of ignoring it.
type PricingPolicy struct {
	IncludeShipping bool `json:"include_shipping"`
}

// Filter is one provider-neutral key and value supplied by a caller.
// The provider converts it to its site-specific wire format.
type Filter struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Sort is one provider-defined sort value supplied through the common API.
type Sort struct {
	Value string `json:"value"`
}

// PageRequest selects one provider result page. Zero values ask the provider
// to use its documented defaults.
type PageRequest struct {
	Number int `json:"number,omitempty"`
	Size   int `json:"size,omitempty"`
}

// PageInfo describes the page that a provider returned. ProviderData contains
// paging details that cannot be represented by the common page-number model.
type PageInfo struct {
	Number       int   `json:"number,omitempty"`
	Size         int   `json:"size,omitempty"`
	TotalItems   *int  `json:"total_items,omitempty"`
	TotalPages   *int  `json:"total_pages,omitempty"`
	HasNext      *bool `json:"has_next,omitempty"`
	ProviderData Data  `json:"provider_data,omitempty"`
}

// VariantSelection selects one variant attribute, such as size or color.
type VariantSelection struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// HelpRequest contains the context for provider help.
type HelpRequest struct {
	Request
}

// HelpResult contains provider discovery metadata.
type HelpResult struct {
	Help Help `json:"help"`
}

// SearchRequest searches only for products.
type SearchRequest struct {
	Request
	Query   string      `json:"query"`
	Filters []Filter    `json:"filters,omitempty"`
	Sort    *Sort       `json:"sort,omitempty"`
	Page    PageRequest `json:"page,omitempty"`
}

// CategoryListRequest lists top-level categories, children, or a recursive
// category tree. An empty ParentID selects top-level categories.
type CategoryListRequest struct {
	Request
	ParentID  string      `json:"parent_id,omitempty"`
	Recursive bool        `json:"recursive,omitempty"`
	Page      PageRequest `json:"page,omitempty"`
}

// CategorySearchRequest finds categories by text.
type CategorySearchRequest struct {
	Request
	Query string      `json:"query"`
	Page  PageRequest `json:"page,omitempty"`
}

// CategoryItemsRequest lists products in one provider category.
type CategoryItemsRequest struct {
	Request
	CategoryID string      `json:"category_id"`
	Filters    []Filter    `json:"filters,omitempty"`
	Sort       *Sort       `json:"sort,omitempty"`
	Page       PageRequest `json:"page,omitempty"`
}

// BrandListRequest lists provider brands.
type BrandListRequest struct {
	Request
	Page PageRequest `json:"page,omitempty"`
}

// BrandSearchRequest finds provider brands by text.
type BrandSearchRequest struct {
	Request
	Query string      `json:"query"`
	Page  PageRequest `json:"page,omitempty"`
}

// BrandItemsRequest lists products for one provider brand.
type BrandItemsRequest struct {
	Request
	BrandID string      `json:"brand_id"`
	Filters []Filter    `json:"filters,omitempty"`
	Sort    *Sort       `json:"sort,omitempty"`
	Page    PageRequest `json:"page,omitempty"`
}

// DealsRequest lists products for which the provider shows a reduction.
type DealsRequest struct {
	Request
	Filters []Filter    `json:"filters,omitempty"`
	Sort    *Sort       `json:"sort,omitempty"`
	Page    PageRequest `json:"page,omitempty"`
}

// FiltersRequest asks for filters and sort modes that apply to an operation.
// CategoryID and BrandID can narrow the definitions when the provider supports
// context-sensitive filters.
type FiltersRequest struct {
	Request
	Capability CapabilityName `json:"capability,omitempty"`
	CategoryID string         `json:"category_id,omitempty"`
	BrandID    string         `json:"brand_id,omitempty"`
}

// ItemRequest gets item details by a provider item ID or provider-owned URL.
// An empty Variants list asks for all visible variants.
type ItemRequest struct {
	Request
	IDOrURL  string             `json:"id_or_url"`
	Variants []VariantSelection `json:"variants,omitempty"`
}

// ProductPage is one page of product summaries.
type ProductPage struct {
	Items        []ProductSummary `json:"items"`
	Page         PageInfo         `json:"page"`
	Warnings     []Warning        `json:"warnings,omitempty"`
	ProviderData Data             `json:"provider_data,omitempty"`
}

// CategoryPage is one page of categories.
type CategoryPage struct {
	Items        []Category `json:"items"`
	Page         PageInfo   `json:"page"`
	Warnings     []Warning  `json:"warnings,omitempty"`
	ProviderData Data       `json:"provider_data,omitempty"`
}

// BrandPage is one page of brands.
type BrandPage struct {
	Items        []Brand   `json:"items"`
	Page         PageInfo  `json:"page"`
	Warnings     []Warning `json:"warnings,omitempty"`
	ProviderData Data      `json:"provider_data,omitempty"`
}

// DealPage is one page of provider-shown deals.
type DealPage struct {
	Items        []Deal    `json:"items"`
	Page         PageInfo  `json:"page"`
	Warnings     []Warning `json:"warnings,omitempty"`
	ProviderData Data      `json:"provider_data,omitempty"`
}

// FiltersResult contains provider filter definitions and sort modes.
type FiltersResult struct {
	Filters      []FilterDefinition `json:"filters,omitempty"`
	SortModes    []SortMode         `json:"sort_modes,omitempty"`
	Warnings     []Warning          `json:"warnings,omitempty"`
	ProviderData Data               `json:"provider_data,omitempty"`
}

// ItemResult contains one full item and operation warnings.
type ItemResult struct {
	Item         ItemDetail `json:"item"`
	Warnings     []Warning  `json:"warnings,omitempty"`
	ProviderData Data       `json:"provider_data,omitempty"`
}

// HelpProvider supplies provider discovery metadata. Every registered
// implementation must implement this interface.
type HelpProvider interface {
	Help(context.Context, HelpRequest) (HelpResult, error)
}

// ConfigValidator validates the provider-specific configuration block.
// Implementations must accept a nil or empty configuration as their defaults.
type ConfigValidator interface {
	ValidateConfig(map[string]interface{}) error
}

// SearchProvider supports product search.
type SearchProvider interface {
	Search(context.Context, SearchRequest) (ProductPage, error)
}

// CategoryListProvider supports category tree navigation.
type CategoryListProvider interface {
	Categories(context.Context, CategoryListRequest) (CategoryPage, error)
}

// CategorySearchProvider supports provider-native category text search.
type CategorySearchProvider interface {
	SearchCategories(context.Context, CategorySearchRequest) (CategoryPage, error)
}

// CategoryItemsProvider supports product listing in a category.
type CategoryItemsProvider interface {
	CategoryItems(context.Context, CategoryItemsRequest) (ProductPage, error)
}

// BrandListProvider supports brand navigation.
type BrandListProvider interface {
	Brands(context.Context, BrandListRequest) (BrandPage, error)
}

// BrandSearchProvider supports provider-native brand text search.
type BrandSearchProvider interface {
	SearchBrands(context.Context, BrandSearchRequest) (BrandPage, error)
}

// BrandItemsProvider supports product listing for a brand.
type BrandItemsProvider interface {
	BrandItems(context.Context, BrandItemsRequest) (ProductPage, error)
}

// DealsProvider supports native deal discovery.
type DealsProvider interface {
	Deals(context.Context, DealsRequest) (DealPage, error)
}

// FiltersProvider supplies available filters and sort modes.
type FiltersProvider interface {
	Filters(context.Context, FiltersRequest) (FiltersResult, error)
}

// ItemProvider supports full item details and optional variant selection.
type ItemProvider interface {
	Item(context.Context, ItemRequest) (ItemResult, error)
}

// Provider is the typed facade used by Core callers. It exposes all common
// operations and returns capability_unavailable for operations that the
// registered implementation did not declare.
type Provider interface {
	Name() string
	Capabilities() []CapabilityName
	Supports(CapabilityName) bool
	ValidateConfig(map[string]interface{}) error
	Help(context.Context, HelpRequest) (HelpResult, error)
	Search(context.Context, SearchRequest) (ProductPage, error)
	Categories(context.Context, CategoryListRequest) (CategoryPage, error)
	SearchCategories(context.Context, CategorySearchRequest) (CategoryPage, error)
	CategoryItems(context.Context, CategoryItemsRequest) (ProductPage, error)
	Brands(context.Context, BrandListRequest) (BrandPage, error)
	SearchBrands(context.Context, BrandSearchRequest) (BrandPage, error)
	BrandItems(context.Context, BrandItemsRequest) (ProductPage, error)
	Deals(context.Context, DealsRequest) (DealPage, error)
	Filters(context.Context, FiltersRequest) (FiltersResult, error)
	Item(context.Context, ItemRequest) (ItemResult, error)
}

type registeredProvider struct {
	name             string
	capabilities     []CapabilityName
	supported        map[CapabilityName]struct{}
	help             func(context.Context, HelpRequest) (HelpResult, error)
	validateConfig   func(map[string]interface{}) error
	search           func(context.Context, SearchRequest) (ProductPage, error)
	categories       func(context.Context, CategoryListRequest) (CategoryPage, error)
	searchCategories func(context.Context, CategorySearchRequest) (CategoryPage, error)
	categoryItems    func(context.Context, CategoryItemsRequest) (ProductPage, error)
	brands           func(context.Context, BrandListRequest) (BrandPage, error)
	searchBrands     func(context.Context, BrandSearchRequest) (BrandPage, error)
	brandItems       func(context.Context, BrandItemsRequest) (ProductPage, error)
	deals            func(context.Context, DealsRequest) (DealPage, error)
	filters          func(context.Context, FiltersRequest) (FiltersResult, error)
	item             func(context.Context, ItemRequest) (ItemResult, error)
}

func (p *registeredProvider) Name() string { return p.name }

func (p *registeredProvider) Capabilities() []CapabilityName {
	return append([]CapabilityName(nil), p.capabilities...)
}

func (p *registeredProvider) Supports(capability CapabilityName) bool {
	_, ok := p.supported[capability]
	return ok
}

func (p *registeredProvider) ValidateConfig(configuration map[string]interface{}) error {
	if p.validateConfig == nil {
		return nil
	}
	return p.validateConfig(configuration)
}

func (p *registeredProvider) Help(ctx context.Context, request HelpRequest) (HelpResult, error) {
	return p.help(ctx, request)
}

func (p *registeredProvider) Search(ctx context.Context, request SearchRequest) (ProductPage, error) {
	if p.search == nil {
		return ProductPage{}, capabilityUnavailable(p.name, CapabilitySearch)
	}
	return p.search(ctx, request)
}

func (p *registeredProvider) Categories(ctx context.Context, request CategoryListRequest) (CategoryPage, error) {
	if p.categories == nil {
		return CategoryPage{}, capabilityUnavailable(p.name, CapabilityCategories)
	}
	return p.categories(ctx, request)
}

func (p *registeredProvider) SearchCategories(ctx context.Context, request CategorySearchRequest) (CategoryPage, error) {
	if p.searchCategories == nil {
		return CategoryPage{}, capabilityUnavailable(p.name, CapabilityCategorySearch)
	}
	return p.searchCategories(ctx, request)
}

func (p *registeredProvider) CategoryItems(ctx context.Context, request CategoryItemsRequest) (ProductPage, error) {
	if p.categoryItems == nil {
		return ProductPage{}, capabilityUnavailable(p.name, CapabilityCategoryItems)
	}
	return p.categoryItems(ctx, request)
}

func (p *registeredProvider) Brands(ctx context.Context, request BrandListRequest) (BrandPage, error) {
	if p.brands == nil {
		return BrandPage{}, capabilityUnavailable(p.name, CapabilityBrands)
	}
	return p.brands(ctx, request)
}

func (p *registeredProvider) SearchBrands(ctx context.Context, request BrandSearchRequest) (BrandPage, error) {
	if p.searchBrands == nil {
		return BrandPage{}, capabilityUnavailable(p.name, CapabilityBrandSearch)
	}
	return p.searchBrands(ctx, request)
}

func (p *registeredProvider) BrandItems(ctx context.Context, request BrandItemsRequest) (ProductPage, error) {
	if p.brandItems == nil {
		return ProductPage{}, capabilityUnavailable(p.name, CapabilityBrandItems)
	}
	return p.brandItems(ctx, request)
}

func (p *registeredProvider) Deals(ctx context.Context, request DealsRequest) (DealPage, error) {
	if p.deals == nil {
		return DealPage{}, capabilityUnavailable(p.name, CapabilityDeals)
	}
	return p.deals(ctx, request)
}

func (p *registeredProvider) Filters(ctx context.Context, request FiltersRequest) (FiltersResult, error) {
	if p.filters == nil {
		return FiltersResult{}, capabilityUnavailable(p.name, CapabilityFilters)
	}
	return p.filters(ctx, request)
}

func (p *registeredProvider) Item(ctx context.Context, request ItemRequest) (ItemResult, error) {
	if p.item == nil {
		return ItemResult{}, capabilityUnavailable(p.name, CapabilityItem)
	}
	if len(request.Variants) > 0 && !p.Supports(CapabilityVariantSelection) {
		return ItemResult{}, capabilityUnavailable(p.name, CapabilityVariantSelection)
	}
	return p.item(ctx, request)
}

func capabilityUnavailable(providerName string, capability CapabilityName) error {
	return NewError(
		ErrorCodeCapabilityUnavailable,
		fmt.Sprintf("provider %q does not support capability %q", providerName, capability),
		nil,
	)
}
