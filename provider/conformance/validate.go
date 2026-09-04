package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/kostyay/ecom/provider"
)

func validateResult(capability provider.CapabilityName, value any, help provider.Help, wantPartial bool) error {
	var (
		page     *provider.PageInfo
		warnings []provider.Warning
		err      error
	)
	switch capability {
	case provider.CapabilitySearch, provider.CapabilityCategoryItems, provider.CapabilityBrandItems:
		result, ok := value.(provider.ProductPage)
		if !ok {
			return fmt.Errorf("result type = %T, want provider.ProductPage", value)
		}
		for index := range result.Items {
			if validateErr := result.Items[index].Validate(); validateErr != nil {
				return fmt.Errorf("item %d: %w", index, validateErr)
			}
		}
		page, warnings, err = &result.Page, result.Warnings, validateData(result.ProviderData)
	case provider.CapabilityCategories, provider.CapabilityCategorySearch:
		result, ok := value.(provider.CategoryPage)
		if !ok {
			return fmt.Errorf("result type = %T, want provider.CategoryPage", value)
		}
		for index := range result.Items {
			if validateErr := result.Items[index].Validate(); validateErr != nil {
				return fmt.Errorf("category %d: %w", index, validateErr)
			}
		}
		page, warnings, err = &result.Page, result.Warnings, validateData(result.ProviderData)
	case provider.CapabilityBrands, provider.CapabilityBrandSearch:
		result, ok := value.(provider.BrandPage)
		if !ok {
			return fmt.Errorf("result type = %T, want provider.BrandPage", value)
		}
		for index := range result.Items {
			if validateErr := result.Items[index].Validate(); validateErr != nil {
				return fmt.Errorf("brand %d: %w", index, validateErr)
			}
		}
		page, warnings, err = &result.Page, result.Warnings, validateData(result.ProviderData)
	case provider.CapabilityDeals:
		result, ok := value.(provider.DealPage)
		if !ok {
			return fmt.Errorf("result type = %T, want provider.DealPage", value)
		}
		for index := range result.Items {
			if validateErr := result.Items[index].Validate(); validateErr != nil {
				return fmt.Errorf("deal %d: %w", index, validateErr)
			}
		}
		page, warnings, err = &result.Page, result.Warnings, validateData(result.ProviderData)
	case provider.CapabilityFilters:
		result, ok := value.(provider.FiltersResult)
		if !ok {
			return fmt.Errorf("result type = %T, want provider.FiltersResult", value)
		}
		filterHelp := provider.Help{Name: help.Name, Filters: result.Filters, SortModes: result.SortModes}
		if validateErr := filterHelp.Validate(); validateErr != nil {
			return fmt.Errorf("filters: %w", validateErr)
		}
		if validateErr := validateFilterHelpConsistency(result, help); validateErr != nil {
			return validateErr
		}
		warnings, err = result.Warnings, validateData(result.ProviderData)
	case provider.CapabilityItem, provider.CapabilityVariantSelection:
		result, ok := value.(provider.ItemResult)
		if !ok {
			return fmt.Errorf("result type = %T, want provider.ItemResult", value)
		}
		if validateErr := result.Item.Validate(); validateErr != nil {
			return fmt.Errorf("item: %w", validateErr)
		}
		warnings, err = result.Warnings, validateData(result.ProviderData)
	default:
		return fmt.Errorf("unknown case capability %q", capability)
	}
	if err != nil {
		return err
	}
	if page != nil {
		if err := validatePage(*page, help.Pagination); err != nil {
			return err
		}
	}
	return validateWarnings(warnings, wantPartial)
}

func validatePage(page provider.PageInfo, help *provider.PaginationHelp) error {
	if page.Number < 0 || page.Size < 0 {
		return errors.New("page number and size must not be negative")
	}
	if page.TotalItems != nil && *page.TotalItems < 0 || page.TotalPages != nil && *page.TotalPages < 0 {
		return errors.New("page totals must not be negative")
	}
	if page.TotalPages != nil && page.Number > *page.TotalPages && *page.TotalPages != 0 {
		return errors.New("page number exceeds total pages")
	}
	if err := validateData(page.ProviderData); err != nil {
		return fmt.Errorf("page provider data: %w", err)
	}
	if help == nil || help.Mode != provider.PaginationPageNumber {
		return nil
	}
	if help.ReportsTotalItems && page.TotalItems == nil {
		return errors.New("provider help says that total items are reported, but the result omits total items")
	}
	if help.ReportsTotalPages && page.TotalPages == nil {
		return errors.New("provider help says that total pages are reported, but the result omits total pages")
	}
	if page.Number < help.FirstPage {
		return fmt.Errorf("page number = %d, before first page %d", page.Number, help.FirstPage)
	}
	if page.Size <= 0 {
		return errors.New("page size must be positive for page-number pagination")
	}
	if len(help.SupportedPageSizes) > 0 {
		if slices.Contains(help.SupportedPageSizes, page.Size) {
			return nil
		}
		return fmt.Errorf("page size %d is not listed in provider help", page.Size)
	}
	return nil
}

func validateFilterHelpConsistency(result provider.FiltersResult, help provider.Help) error {
	filters := make(map[string]struct{}, len(help.Filters))
	for _, filter := range help.Filters {
		filters[filter.Key] = struct{}{}
	}
	for _, filter := range result.Filters {
		if _, ok := filters[filter.Key]; !ok {
			return fmt.Errorf("filter %q is not documented in provider help", filter.Key)
		}
	}
	sorts := make(map[string]struct{}, len(help.SortModes))
	for _, sort := range help.SortModes {
		sorts[sort.Value] = struct{}{}
	}
	for _, sort := range result.SortModes {
		if _, ok := sorts[sort.Value]; !ok {
			return fmt.Errorf("sort mode %q is not documented in provider help", sort.Value)
		}
	}
	return nil
}

func validateWarnings(warnings []provider.Warning, wantPartial bool) error {
	foundPartial := false
	for index, warning := range warnings {
		if warning.Code == "" || warning.Message == "" {
			return fmt.Errorf("warning %d requires a code and message", index)
		}
		if warning.Code != provider.WarningCodePartialParsing {
			continue
		}
		foundPartial = true
		if warning.FoundCount == nil || warning.ParsedCount == nil {
			return fmt.Errorf("warning %d partial parsing counts are required", index)
		}
		if *warning.FoundCount <= *warning.ParsedCount || *warning.ParsedCount < 0 {
			return fmt.Errorf("warning %d partial parsing counts are invalid", index)
		}
	}
	if wantPartial && !foundPartial {
		return errors.New("partial_parsing warning is required")
	}
	return nil
}

func validateData(data provider.Data) error {
	for namespace, value := range data {
		if namespace == "" {
			return errors.New("provider data namespace is required")
		}
		if !json.Valid(value) {
			return fmt.Errorf("provider data %q must contain valid JSON", namespace)
		}
	}
	return nil
}
