// Package testprovider supplies a deterministic provider for integration tests.
//
// The package does not register itself. Tests must add Registration to their
// own provider.Registry. This prevents collisions with providers in a built
// CLI distribution.
package testprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kostyay/ecom/provider"
)

const (
	// Name is the stable provider name used by integration tests.
	Name              = "test-fixture"
	defaultPageSize   = 2
	searchResourceURL = "https://fixture.invalid/catalog-version"
)

var capabilities = []provider.CapabilityName{
	provider.CapabilitySearch,
	provider.CapabilityCategories,
	provider.CapabilityCategorySearch,
	provider.CapabilityCategoryItems,
	provider.CapabilityBrands,
	provider.CapabilityBrandSearch,
	provider.CapabilityBrandItems,
	provider.CapabilityDeals,
	provider.CapabilityFilters,
	provider.CapabilityItem,
	provider.CapabilityVariantSelection,
}

var fixtureTime = time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)

// Registration returns a new, full-capability fixture registration.
func Registration() provider.Registration {
	return provider.Registration{
		Name: Name, SDKAPIVersion: provider.APIVersion, Implementation: &fixtureProvider{},
		Capabilities: append([]provider.CapabilityName(nil), capabilities...),
	}
}

type fixtureProvider struct{}

func (*fixtureProvider) ValidateConfig(configuration map[string]any) error {
	for key, value := range configuration {
		if key != "page_size" {
			return provider.NewError(provider.ErrorCodeInvalidProviderConfig, fmt.Sprintf("unknown test fixture setting %q", key), nil)
		}
		size, ok := numericInt(value)
		if !ok || !slices.Contains([]int{1, 2}, size) {
			return provider.NewError(provider.ErrorCodeInvalidProviderConfig, "test fixture page_size must be 1 or 2", nil)
		}
	}
	return nil
}

func (*fixtureProvider) Help(context.Context, provider.HelpRequest) (provider.HelpResult, error) {
	capabilityHelp := make([]provider.CapabilityHelp, 0, len(capabilities))
	for _, capability := range capabilities {
		capabilityHelp = append(capabilityHelp, provider.CapabilityHelp{
			Name: capability, Supported: true, Description: "Deterministic test implementation.",
		})
	}
	return provider.HelpResult{Help: provider.Help{
		Name: Name, DisplayName: "Test Fixture", Description: "Offline provider for CLI and Core integration tests.",
		Capabilities: capabilityHelp,
		Search: &provider.SearchHelp{
			QueryRequired: true, Syntax: "case-insensitive product name, brand, or attribute text",
			Examples: []string{"helmet", "acme", "partial-warning"},
			Notes:    []string{"The reserved query partial-warning returns a valid partial-result warning."},
		},
		Filters: filterDefinitions(), SortModes: sortModes(),
		Pagination: &provider.PaginationHelp{
			Mode: provider.PaginationPageNumber, FirstPage: 1, DefaultPageSize: defaultPageSize,
			SupportedPageSizes: []int{1, 2}, ReportsTotalItems: true, ReportsTotalPages: true,
		},
		Markets: &provider.MarketRestrictions{
			Countries: []string{"DE"}, Languages: []string{"en"}, Currencies: []string{"EUR"},
			Notes: []string{"A different requested currency returns EUR with a currency_unavailable warning."},
		},
		Access:       &provider.AccessRequirements{Authentication: provider.AuthenticationNone, Browser: provider.BrowserNone},
		Warnings:     []provider.HelpWarning{{Code: string(provider.WarningCodePartialParsing), Message: "Tests can request a deterministic partial result."}},
		ProviderData: data(map[string]any{"fixture_version": 1}),
	}}, nil
}

func (*fixtureProvider) Search(ctx context.Context, request provider.SearchRequest) (provider.ProductPage, error) {
	if strings.TrimSpace(request.Query) == "" {
		return provider.ProductPage{}, invalid("search query is required")
	}
	if request.Resources != nil {
		response, err := request.Resources.Fetch(ctx, provider.ResourceRequest{
			Method: "GET", URL: searchResourceURL,
			Transport: provider.TransportPolicy{Required: provider.TransportHTTP},
		})
		if err != nil {
			return provider.ProductPage{}, err
		}
		if string(response.Body) != "fixture-catalog-v1" {
			return provider.ProductPage{}, invalid("fixture search resource is invalid")
		}
	}
	items, err := selectProducts(request.Filters, request.Sort, func(product record) bool {
		query := strings.ToLower(request.Query)
		if query == "partial-warning" {
			return product.ID == "trail-helmet"
		}
		return strings.Contains(strings.ToLower(product.Name+" "+product.Brand+" "+product.Color), query)
	})
	if err != nil {
		return provider.ProductPage{}, err
	}
	page, err := productPage(items, request.Page)
	if err != nil {
		return provider.ProductPage{}, err
	}
	page.Warnings = currencyWarnings(request.Market)
	if request.Query == "partial-warning" {
		found, parsed := 2, 1
		page.Warnings = append(page.Warnings, provider.Warning{
			Code: provider.WarningCodePartialParsing, Message: "one fixture result could not be parsed",
			FoundCount: &found, ParsedCount: &parsed, URL: "https://fixture.invalid/broken-result",
		})
	}
	return page, nil
}

func (*fixtureProvider) Categories(_ context.Context, request provider.CategoryListRequest) (provider.CategoryPage, error) {
	items := categories()
	selected := make([]provider.Category, 0, len(items))
	for _, category := range items {
		if request.Recursive {
			if request.ParentID == "" || categoryDescendsFrom(category, request.ParentID, items) {
				selected = append(selected, category)
			}
		} else if category.ParentID == request.ParentID {
			selected = append(selected, category)
		}
	}
	if request.ParentID != "" && !hasCategory(request.ParentID) {
		return provider.CategoryPage{}, invalid("category ID was not found")
	}
	return categoryPage(selected, request.Page)
}

func (*fixtureProvider) SearchCategories(_ context.Context, request provider.CategorySearchRequest) (provider.CategoryPage, error) {
	if strings.TrimSpace(request.Query) == "" {
		return provider.CategoryPage{}, invalid("category query is required")
	}
	query := strings.ToLower(request.Query)
	var selected []provider.Category
	for _, category := range categories() {
		if strings.Contains(strings.ToLower(category.Name+" "+category.Path), query) {
			selected = append(selected, category)
		}
	}
	return categoryPage(selected, request.Page)
}

func (*fixtureProvider) CategoryItems(_ context.Context, request provider.CategoryItemsRequest) (provider.ProductPage, error) {
	if !hasCategory(request.CategoryID) {
		return provider.ProductPage{}, invalid("category ID was not found")
	}
	items, err := selectProducts(request.Filters, request.Sort, func(product record) bool {
		return product.CategoryID == request.CategoryID || request.CategoryID == "cycling"
	})
	if err != nil {
		return provider.ProductPage{}, err
	}
	page, err := productPage(items, request.Page)
	if err == nil {
		page.Warnings = currencyWarnings(request.Market)
	}
	return page, err
}

func (*fixtureProvider) Brands(_ context.Context, request provider.BrandListRequest) (provider.BrandPage, error) {
	return brandPage(brands(), request.Page)
}

func (*fixtureProvider) SearchBrands(_ context.Context, request provider.BrandSearchRequest) (provider.BrandPage, error) {
	if strings.TrimSpace(request.Query) == "" {
		return provider.BrandPage{}, invalid("brand query is required")
	}
	query := strings.ToLower(request.Query)
	var selected []provider.Brand
	for _, brand := range brands() {
		if strings.Contains(strings.ToLower(brand.Name), query) {
			selected = append(selected, brand)
		}
	}
	return brandPage(selected, request.Page)
}

func (*fixtureProvider) BrandItems(_ context.Context, request provider.BrandItemsRequest) (provider.ProductPage, error) {
	if !hasBrand(request.BrandID) {
		return provider.ProductPage{}, invalid("brand ID was not found")
	}
	items, err := selectProducts(request.Filters, request.Sort, func(product record) bool { return product.BrandID == request.BrandID })
	if err != nil {
		return provider.ProductPage{}, err
	}
	page, err := productPage(items, request.Page)
	if err == nil {
		page.Warnings = currencyWarnings(request.Market)
	}
	return page, err
}

func (*fixtureProvider) Deals(_ context.Context, request provider.DealsRequest) (provider.DealPage, error) {
	items, err := selectProducts(request.Filters, request.Sort, func(product record) bool { return product.OriginalAmount != "" })
	if err != nil {
		return provider.DealPage{}, err
	}
	products, info, err := paginate(items, request.Page)
	if err != nil {
		return provider.DealPage{}, err
	}
	deals := make([]provider.Deal, 0, len(products))
	for _, product := range products {
		deals = append(deals, provider.Deal{Product: summary(product)})
	}
	return provider.DealPage{Items: deals, Page: info, Warnings: currencyWarnings(request.Market), ProviderData: fixtureData("deals")}, nil
}

func (*fixtureProvider) Filters(_ context.Context, request provider.FiltersRequest) (provider.FiltersResult, error) {
	if request.Capability != "" && !slices.Contains([]provider.CapabilityName{
		provider.CapabilitySearch, provider.CapabilityCategoryItems, provider.CapabilityBrandItems, provider.CapabilityDeals,
	}, request.Capability) {
		return provider.FiltersResult{}, invalid("filters are not available for the requested capability")
	}
	filters := filterDefinitions()
	sorts := sortModes()
	if request.Capability != "" {
		filters = slices.DeleteFunc(filters, func(filter provider.FilterDefinition) bool {
			return !slices.Contains(filter.AppliesTo, request.Capability)
		})
		sorts = slices.DeleteFunc(sorts, func(sort provider.SortMode) bool { return !slices.Contains(sort.AppliesTo, request.Capability) })
	}
	return provider.FiltersResult{Filters: filters, SortModes: sorts, ProviderData: fixtureData("filters")}, nil
}

func (*fixtureProvider) Item(_ context.Context, request provider.ItemRequest) (provider.ItemResult, error) {
	identifier := itemID(request.IDOrURL)
	product, found := findRecord(identifier)
	if !found {
		return provider.ItemResult{}, invalid("item was not found")
	}
	item := detail(product)
	if len(request.Variants) > 0 {
		index := slices.IndexFunc(item.Variants, func(variant provider.Variant) bool { return matches(variant, request.Variants) })
		if index < 0 {
			return provider.ItemResult{}, provider.NewError(provider.ErrorCodeVariantNotFound, "variant was not found; valid size and color choices are in the item result", nil)
		}
		item.Variants[index].Selected = true
		selected := item.Variants[index]
		item.SelectedVariant = &selected
		item.Price = selected.Price
	}
	return provider.ItemResult{Item: item, Warnings: currencyWarnings(request.Market), ProviderData: fixtureData("item")}, nil
}

func filterDefinitions() []provider.FilterDefinition {
	listings := []provider.CapabilityName{provider.CapabilitySearch, provider.CapabilityCategoryItems, provider.CapabilityBrandItems, provider.CapabilityDeals}
	return []provider.FilterDefinition{
		{Key: "brand", Type: provider.FilterTypeEnum, Repeatable: true, Description: "Keep one or more brand IDs.", AllowedValues: []provider.FilterValue{{Value: "acme", Label: "Acme"}, {Value: "velo", Label: "Velo Works"}}, AppliesTo: listings},
		{Key: "in-stock", Type: provider.FilterTypeBoolean, Description: "Keep or exclude products that are in stock.", Examples: []string{"true"}, AppliesTo: listings},
		{Key: "max-price", Type: provider.FilterTypeDecimal, Description: "Maximum displayed EUR item price.", Examples: []string{"100.00"}, AppliesTo: listings},
		{Key: "min-discount", Type: provider.FilterTypeInteger, Description: "Minimum shown discount percentage.", Examples: []string{"10"}, AppliesTo: listings},
		{Key: "color", Type: provider.FilterTypeString, Repeatable: true, Description: "Case-insensitive exact color.", Examples: []string{"black"}, AppliesTo: listings},
	}
}

func sortModes() []provider.SortMode {
	listings := []provider.CapabilityName{provider.CapabilitySearch, provider.CapabilityCategoryItems, provider.CapabilityBrandItems, provider.CapabilityDeals}
	return []provider.SortMode{
		{Value: "price-asc", Label: "Lowest price", AppliesTo: listings},
		{Value: "price-desc", Label: "Highest price", AppliesTo: listings},
		{Value: "name-asc", Label: "Name", AppliesTo: listings},
	}
}

type record struct {
	ID, Name, BrandID, Brand, CategoryID, Color, Amount, OriginalAmount string
	InStock                                                             bool
	Discount                                                            int
}

func records() []record {
	return []record{
		{ID: "trail-helmet", Name: "Trail Helmet", BrandID: "acme", Brand: "Acme", CategoryID: "helmets", Color: "black", Amount: "79.95", OriginalAmount: "99.95", InStock: true, Discount: 20},
		{ID: "road-helmet", Name: "Road Helmet", BrandID: "velo", Brand: "Velo Works", CategoryID: "helmets", Color: "white", Amount: "119.00", InStock: true},
		{ID: "winter-gloves", Name: "Winter Gloves", BrandID: "acme", Brand: "Acme", CategoryID: "clothing", Color: "red", Amount: "34.50", OriginalAmount: "49.50", InStock: false, Discount: 30},
	}
}

func categories() []provider.Category {
	return []provider.Category{
		{ID: "cycling", Name: "Cycling", Path: "Cycling", URL: "https://fixture.invalid/categories/cycling", HasChildren: true, ProviderData: fixtureData("category")},
		{ID: "helmets", Name: "Helmets", Path: "Cycling / Helmets", ParentID: "cycling", URL: "https://fixture.invalid/categories/helmets", ProviderData: fixtureData("category")},
		{ID: "clothing", Name: "Clothing", Path: "Cycling / Clothing", ParentID: "cycling", URL: "https://fixture.invalid/categories/clothing", ProviderData: fixtureData("category")},
	}
}

func brands() []provider.Brand {
	return []provider.Brand{
		{ID: "acme", Name: "Acme", URL: "https://fixture.invalid/brands/acme", ProviderData: fixtureData("brand")},
		{ID: "velo", Name: "Velo Works", URL: "https://fixture.invalid/brands/velo", ProviderData: fixtureData("brand")},
	}
}

func selectProducts(filters []provider.Filter, sort *provider.Sort, include func(record) bool) ([]record, error) {
	if err := validateFixtureFilters(filters); err != nil {
		return nil, err
	}
	var result []record
	for _, product := range records() {
		if !include(product) {
			continue
		}
		keep, err := matchesFilters(product, filters)
		if err != nil {
			return nil, err
		}
		if keep {
			result = append(result, product)
		}
	}
	if sort != nil {
		switch sort.Value {
		case "price-asc":
			slices.SortFunc(result, func(a, b record) int { return compareDecimal(a.Amount, b.Amount) })
		case "price-desc":
			slices.SortFunc(result, func(a, b record) int { return compareDecimal(b.Amount, a.Amount) })
		case "name-asc":
			slices.SortFunc(result, func(a, b record) int { return strings.Compare(a.Name, b.Name) })
		default:
			return nil, invalid(fmt.Sprintf("sort mode %q is not supported", sort.Value))
		}
	}
	return result, nil
}

func validateFixtureFilters(filters []provider.Filter) error {
	definitions := make(map[string]provider.FilterDefinition)
	for _, definition := range filterDefinitions() {
		definitions[definition.Key] = definition
	}
	seen := make(map[string]bool)
	for _, filter := range filters {
		definition, found := definitions[filter.Key]
		if !found {
			return invalid(fmt.Sprintf("filter %q is not supported", filter.Key))
		}
		if seen[filter.Key] && !definition.Repeatable {
			return invalid(fmt.Sprintf("filter %q cannot be repeated", filter.Key))
		}
		seen[filter.Key] = true
		switch filter.Key {
		case "brand":
			if !slices.Contains([]string{"acme", "velo"}, filter.Value) {
				return invalid("brand must be acme or velo")
			}
		case "color":
			if strings.TrimSpace(filter.Value) == "" {
				return invalid("color must not be empty")
			}
		case "in-stock":
			if filter.Value != "true" && filter.Value != "false" {
				return invalid("in-stock must be true or false")
			}
		case "max-price":
			if err := (provider.Money{Amount: filter.Value, Currency: "EUR", Display: filter.Value}).Validate(); err != nil {
				return invalid("max-price must be a non-negative decimal")
			}
		case "min-discount":
			minimum, err := strconv.Atoi(filter.Value)
			if err != nil || minimum < 0 || minimum > 100 {
				return invalid("min-discount must be an integer from 0 to 100")
			}
		}
	}
	return nil
}

func matchesFilters(product record, filters []provider.Filter) (bool, error) {
	grouped := make(map[string][]string)
	for _, filter := range filters {
		grouped[filter.Key] = append(grouped[filter.Key], filter.Value)
	}
	for key, values := range grouped {
		switch key {
		case "brand":
			if !slices.Contains(values, product.BrandID) {
				return false, nil
			}
		case "color":
			if !slices.ContainsFunc(values, func(value string) bool { return strings.EqualFold(value, product.Color) }) {
				return false, nil
			}
		case "in-stock":
			if len(values) != 1 || values[0] != "true" && values[0] != "false" {
				return false, invalid("in-stock must be true or false")
			}
			if product.InStock != (values[0] == "true") {
				return false, nil
			}
		case "max-price":
			limit, _ := new(big.Rat).SetString(values[0])
			amount, _ := new(big.Rat).SetString(product.Amount)
			if amount.Cmp(limit) > 0 {
				return false, nil
			}
		case "min-discount":
			minimum, _ := strconv.Atoi(values[0])
			if product.Discount < minimum {
				return false, nil
			}
		}
	}
	return true, nil
}

func productPage(items []record, request provider.PageRequest) (provider.ProductPage, error) {
	selected, info, err := paginate(items, request)
	if err != nil {
		return provider.ProductPage{}, err
	}
	products := make([]provider.ProductSummary, 0, len(selected))
	for _, product := range selected {
		products = append(products, summary(product))
	}
	return provider.ProductPage{Items: products, Page: info, ProviderData: fixtureData("products")}, nil
}

func categoryPage(items []provider.Category, request provider.PageRequest) (provider.CategoryPage, error) {
	selected, info, err := paginate(items, request)
	return provider.CategoryPage{Items: selected, Page: info, ProviderData: fixtureData("categories")}, err
}

func brandPage(items []provider.Brand, request provider.PageRequest) (provider.BrandPage, error) {
	selected, info, err := paginate(items, request)
	return provider.BrandPage{Items: selected, Page: info, ProviderData: fixtureData("brands")}, err
}

func paginate[T any](items []T, request provider.PageRequest) ([]T, provider.PageInfo, error) {
	number, size := request.Number, request.Size
	if number == 0 {
		number = 1
	}
	if size == 0 {
		size = defaultPageSize
	}
	if number < 1 || !slices.Contains([]int{1, 2}, size) {
		return nil, provider.PageInfo{}, invalid("page must start at 1 and page size must be 1 or 2")
	}
	total := len(items)
	totalPages := (total + size - 1) / size
	if totalPages > 0 && number > totalPages {
		return nil, provider.PageInfo{}, invalid("page exceeds the available page count")
	}
	start := min((number-1)*size, total)
	end := min(start+size, total)
	hasNext := number < totalPages
	return append([]T(nil), items[start:end]...), provider.PageInfo{
		Number: number, Size: size, TotalItems: &total, TotalPages: &totalPages, HasNext: &hasNext,
		ProviderData: fixtureData("page"),
	}, nil
}

func summary(product record) provider.ProductSummary {
	result := provider.ProductSummary{
		ID: product.ID, URL: "https://fixture.invalid/items/" + product.ID, Name: product.Name, Brand: product.Brand,
		Price: money(product.Amount), Availability: provider.AvailabilityOutOfStock, StockText: "Out of stock",
		ImageURL: "https://fixture.invalid/images/" + product.ID + ".jpg", RetrievedAt: fixtureTime,
		DetailLevel: provider.DetailLevelSummary, ProviderData: fixtureData("product"),
		Attributes: []provider.Attribute{{Name: "color", Value: product.Color}},
	}
	if product.InStock {
		result.Availability, result.StockText = provider.AvailabilityInStock, "In stock"
	}
	if product.OriginalAmount != "" {
		result.OriginalPrice = money(product.OriginalAmount)
		discount := product.Discount
		result.DiscountPercent = &discount
		result.DiscountAmount = money(subtractDecimal(product.OriginalAmount, product.Amount))
	}
	return result
}

func detail(product record) provider.ItemDetail {
	base := summary(product)
	base.DetailLevel = provider.DetailLevelFull
	base.Variants = variants(product)
	base.PriceRange = &provider.PriceRange{Minimum: *base.Variants[0].Price, Maximum: *base.Variants[len(base.Variants)-1].Price}
	return provider.ItemDetail{ProductSummary: base, Description: "Static fixture details for " + product.Name + "."}
}

func variants(product record) []provider.Variant {
	return []provider.Variant{
		{ID: product.ID + "-s", Attributes: []provider.Attribute{{Name: "size", Value: "S"}, {Name: "color", Value: product.Color}}, Price: money(product.Amount), Availability: provider.AvailabilityInStock, StockText: "In stock", ProviderData: fixtureData("variant")},
		{ID: product.ID + "-m", Attributes: []provider.Attribute{{Name: "size", Value: "M"}, {Name: "color", Value: product.Color}}, Price: money(addDecimal(product.Amount, "10.00")), Availability: provider.AvailabilityInStock, StockText: "In stock", ProviderData: fixtureData("variant")},
	}
}

func matches(variant provider.Variant, selections []provider.VariantSelection) bool {
	attributes := make(map[string]string, len(variant.Attributes))
	for _, attribute := range variant.Attributes {
		attributes[attribute.Name] = attribute.Value
	}
	for _, selection := range selections {
		if attributes[selection.Key] != selection.Value {
			return false
		}
	}
	return true
}

func findRecord(id string) (record, bool) {
	for _, product := range records() {
		if product.ID == id {
			return product, true
		}
	}
	return record{}, false
}

func itemID(identifier string) string {
	parsed, err := url.Parse(identifier)
	if err == nil && parsed.Host == "fixture.invalid" {
		return strings.TrimPrefix(parsed.Path, "/items/")
	}
	return identifier
}

func categoryDescendsFrom(category provider.Category, parentID string, all []provider.Category) bool {
	parents := make(map[string]string, len(all))
	for _, item := range all {
		parents[item.ID] = item.ParentID
	}
	for current := category.ParentID; current != ""; current = parents[current] {
		if current == parentID {
			return true
		}
	}
	return false
}

func hasCategory(id string) bool {
	return slices.ContainsFunc(categories(), func(item provider.Category) bool { return item.ID == id })
}
func hasBrand(id string) bool {
	return slices.ContainsFunc(brands(), func(item provider.Brand) bool { return item.ID == id })
}

func currencyWarnings(market provider.Market) []provider.Warning {
	if market.Currency == "" || market.Currency == "EUR" {
		return nil
	}
	return []provider.Warning{{
		Code: provider.WarningCodeCurrencyUnavailable, Message: "the fixture has prices only in EUR",
		RequestedCurrency: market.Currency, ActualCurrency: "EUR",
	}}
}

func money(amount string) *provider.Money {
	return &provider.Money{Amount: amount, Currency: "EUR", Display: "€" + amount}
}

func compareDecimal(a, b string) int {
	left, _ := new(big.Rat).SetString(a)
	right, _ := new(big.Rat).SetString(b)
	return left.Cmp(right)
}

func addDecimal(a, b string) string      { return decimalOperation(a, b, false) }
func subtractDecimal(a, b string) string { return decimalOperation(a, b, true) }

func decimalOperation(a, b string, subtract bool) string {
	left, _ := new(big.Rat).SetString(a)
	right, _ := new(big.Rat).SetString(b)
	if subtract {
		left.Sub(left, right)
	} else {
		left.Add(left, right)
	}
	return left.FloatString(2)
}

func data(value any) provider.Data {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return provider.Data{Name: encoded}
}

func fixtureData(kind string) provider.Data { return data(map[string]string{"kind": kind}) }

func invalid(message string) error {
	return provider.NewError(provider.ErrorCodeInvalidFilter, message, nil)
}

func numericInt(value any) (int, bool) {
	switch value := value.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), value == float64(int(value))
	default:
		return 0, false
	}
}

var (
	_ provider.HelpProvider           = (*fixtureProvider)(nil)
	_ provider.ConfigValidator        = (*fixtureProvider)(nil)
	_ provider.SearchProvider         = (*fixtureProvider)(nil)
	_ provider.CategoryListProvider   = (*fixtureProvider)(nil)
	_ provider.CategorySearchProvider = (*fixtureProvider)(nil)
	_ provider.CategoryItemsProvider  = (*fixtureProvider)(nil)
	_ provider.BrandListProvider      = (*fixtureProvider)(nil)
	_ provider.BrandSearchProvider    = (*fixtureProvider)(nil)
	_ provider.BrandItemsProvider     = (*fixtureProvider)(nil)
	_ provider.DealsProvider          = (*fixtureProvider)(nil)
	_ provider.FiltersProvider        = (*fixtureProvider)(nil)
	_ provider.ItemProvider           = (*fixtureProvider)(nil)
)
