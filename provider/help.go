package provider

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// CapabilityName identifies a provider operation in machine-readable help.
type CapabilityName string

// Supported capability names.
const (
	CapabilitySearch           CapabilityName = "search"
	CapabilityCategories       CapabilityName = "categories"
	CapabilityCategorySearch   CapabilityName = "category_search"
	CapabilityCategoryItems    CapabilityName = "category_items"
	CapabilityBrands           CapabilityName = "brands"
	CapabilityBrandSearch      CapabilityName = "brand_search"
	CapabilityBrandItems       CapabilityName = "brand_items"
	CapabilityDeals            CapabilityName = "deals"
	CapabilityFilters          CapabilityName = "filters"
	CapabilityItem             CapabilityName = "item"
	CapabilityVariantSelection CapabilityName = "variant_selection"
)

// CapabilityHelp states whether a provider supports one operation.
type CapabilityHelp struct {
	Name        CapabilityName `json:"name"`
	Supported   bool           `json:"supported"`
	Description string         `json:"description,omitempty"`
	Notes       []string       `json:"notes,omitempty"`
}

// SearchHelp describes the text syntax accepted by product search.
type SearchHelp struct {
	QueryRequired bool     `json:"query_required"`
	Syntax        string   `json:"syntax,omitempty"`
	Examples      []string `json:"examples,omitempty"`
	Notes         []string `json:"notes,omitempty"`
}

// FilterType identifies the value format of a provider filter.
// Filter values enter the CLI as text and providers parse them by this type.
type FilterType string

// Supported filter value types.
const (
	FilterTypeString  FilterType = "string"
	FilterTypeBoolean FilterType = "boolean"
	FilterTypeInteger FilterType = "integer"
	FilterTypeDecimal FilterType = "decimal"
	FilterTypeEnum    FilterType = "enum"
)

// FilterValue describes one value that a provider accepts for an enum filter.
type FilterValue struct {
	Value       string `json:"value"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

// FilterDefinition describes one provider filter without exposing its wire format.
type FilterDefinition struct {
	Key           string           `json:"key"`
	Type          FilterType       `json:"type"`
	Description   string           `json:"description,omitempty"`
	Repeatable    bool             `json:"repeatable,omitempty"`
	AllowedValues []FilterValue    `json:"allowed_values,omitempty"`
	Examples      []string         `json:"examples,omitempty"`
	AppliesTo     []CapabilityName `json:"applies_to,omitempty"`
	Notes         []string         `json:"notes,omitempty"`
}

// SortMode describes one value accepted by the common sort option.
type SortMode struct {
	Value       string           `json:"value"`
	Label       string           `json:"label,omitempty"`
	Description string           `json:"description,omitempty"`
	AppliesTo   []CapabilityName `json:"applies_to,omitempty"`
}

// PaginationMode identifies the pagination model exposed by a provider.
type PaginationMode string

// Supported pagination modes.
const (
	PaginationNone       PaginationMode = "none"
	PaginationPageNumber PaginationMode = "page_number"
	PaginationCursor     PaginationMode = "cursor"
)

// PaginationHelp describes provider paging rules.
type PaginationHelp struct {
	Mode               PaginationMode `json:"mode"`
	FirstPage          int            `json:"first_page,omitempty"`
	DefaultPageSize    int            `json:"default_page_size,omitempty"`
	SupportedPageSizes []int          `json:"supported_page_sizes,omitempty"`
	ReportsTotalItems  bool           `json:"reports_total_items,omitempty"`
	ReportsTotalPages  bool           `json:"reports_total_pages,omitempty"`
	Notes              []string       `json:"notes,omitempty"`
}

// MarketRestrictions states which market settings a provider accepts.
// An empty allowed list means that the provider does not publish a restriction.
type MarketRestrictions struct {
	Countries  []string `json:"countries,omitempty"`
	Languages  []string `json:"languages,omitempty"`
	Currencies []string `json:"currencies,omitempty"`
	Notes      []string `json:"notes,omitempty"`
}

// AuthenticationRequirement identifies whether a provider needs authentication.
type AuthenticationRequirement string

// Supported authentication requirements.
const (
	AuthenticationNone     AuthenticationRequirement = "none"
	AuthenticationOptional AuthenticationRequirement = "optional"
	AuthenticationRequired AuthenticationRequirement = "required"
)

// BrowserRequirement identifies when a provider needs browser transport.
type BrowserRequirement string

// Supported browser requirements.
const (
	BrowserNone     BrowserRequirement = "none"
	BrowserFallback BrowserRequirement = "fallback"
	BrowserRequired BrowserRequirement = "required"
)

// AccessRequirements describes authentication and browser needs.
type AccessRequirements struct {
	Authentication      AuthenticationRequirement `json:"authentication"`
	Browser             BrowserRequirement        `json:"browser"`
	SupportsCDP         bool                      `json:"supports_cdp,omitempty"`
	SupportsInteractive bool                      `json:"supports_interactive,omitempty"`
	Notes               []string                  `json:"notes,omitempty"`
}

// TransportMode identifies a Core-owned resource transport.
type TransportMode string

// Supported Core-owned transport modes.
const (
	TransportHTTP    TransportMode = "http"
	TransportBrowser TransportMode = "browser"
	TransportCDP     TransportMode = "cdp"
)

// TransportNote explains when a provider uses one Core-owned transport.
type TransportNote struct {
	Mode    TransportMode `json:"mode"`
	UseWhen string        `json:"use_when,omitempty"`
	Notes   []string      `json:"notes,omitempty"`
}

// HelpWarning describes a known provider restriction or operational risk.
type HelpWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Help contains all provider discovery metadata used by agents and
// generic output renderers. Site-specific fields belong only in ProviderData.
type Help struct {
	Name         string              `json:"name"`
	DisplayName  string              `json:"display_name,omitempty"`
	Description  string              `json:"description,omitempty"`
	Capabilities []CapabilityHelp    `json:"capabilities,omitempty"`
	Search       *SearchHelp         `json:"search,omitempty"`
	Filters      []FilterDefinition  `json:"filters,omitempty"`
	SortModes    []SortMode          `json:"sort_modes,omitempty"`
	Pagination   *PaginationHelp     `json:"pagination,omitempty"`
	Markets      *MarketRestrictions `json:"markets,omitempty"`
	Access       *AccessRequirements `json:"access,omitempty"`
	Transport    []TransportNote     `json:"transport,omitempty"`
	Warnings     []HelpWarning       `json:"warnings,omitempty"`
	ProviderData Data                `json:"provider_data,omitempty"`
}

// Validate checks the provider-neutral help contract.
func (h Help) Validate() error {
	if !validProviderName(h.Name) {
		return errors.New("provider help name must be a valid provider identifier")
	}

	if err := validateCapabilities(h.Capabilities); err != nil {
		return err
	}
	if err := validateFilters(h.Filters); err != nil {
		return err
	}
	if err := validateSortModes(h.SortModes); err != nil {
		return err
	}
	if h.Pagination != nil {
		if err := h.Pagination.Validate(); err != nil {
			return fmt.Errorf("pagination: %w", err)
		}
	}
	if h.Access != nil {
		if err := h.Access.Validate(); err != nil {
			return fmt.Errorf("access: %w", err)
		}
	}
	for index, note := range h.Transport {
		if !oneOf(note.Mode, TransportHTTP, TransportBrowser, TransportCDP) {
			return fmt.Errorf("transport note %d has unknown mode %q", index, note.Mode)
		}
	}
	for index, warning := range h.Warnings {
		if strings.TrimSpace(warning.Code) == "" || strings.TrimSpace(warning.Message) == "" {
			return fmt.Errorf("warning %d requires a code and message", index)
		}
	}
	if err := validateProviderData(h.ProviderData); err != nil {
		return err
	}
	return nil
}

// Validate checks pagination values and their relationships.
func (p PaginationHelp) Validate() error {
	if !oneOf(p.Mode, PaginationNone, PaginationPageNumber, PaginationCursor) {
		return fmt.Errorf("unknown mode %q", p.Mode)
	}
	if p.FirstPage < 0 || p.DefaultPageSize < 0 {
		return errors.New("first page and default page size must not be negative")
	}
	seen := make(map[int]struct{}, len(p.SupportedPageSizes))
	for _, size := range p.SupportedPageSizes {
		if size <= 0 {
			return errors.New("supported page sizes must be positive")
		}
		if _, exists := seen[size]; exists {
			return fmt.Errorf("duplicate supported page size %d", size)
		}
		seen[size] = struct{}{}
	}
	if p.DefaultPageSize > 0 && len(seen) > 0 {
		if _, exists := seen[p.DefaultPageSize]; !exists {
			return errors.New("default page size must be a supported page size")
		}
	}
	return nil
}

// Validate checks the authentication and browser requirement values.
func (a AccessRequirements) Validate() error {
	if !oneOf(a.Authentication, AuthenticationNone, AuthenticationOptional, AuthenticationRequired) {
		return fmt.Errorf("unknown authentication requirement %q", a.Authentication)
	}
	if !oneOf(a.Browser, BrowserNone, BrowserFallback, BrowserRequired) {
		return fmt.Errorf("unknown browser requirement %q", a.Browser)
	}
	return nil
}

func validateCapabilities(capabilities []CapabilityHelp) error {
	seen := make(map[CapabilityName]struct{}, len(capabilities))
	for index, capability := range capabilities {
		if strings.TrimSpace(string(capability.Name)) == "" {
			return fmt.Errorf("capability %d requires a name", index)
		}
		if _, exists := seen[capability.Name]; exists {
			return fmt.Errorf("duplicate capability %q", capability.Name)
		}
		seen[capability.Name] = struct{}{}
	}
	return nil
}

func validateFilters(filters []FilterDefinition) error {
	seen := make(map[string]struct{}, len(filters))
	for index, filter := range filters {
		if strings.TrimSpace(filter.Key) == "" {
			return fmt.Errorf("filter %d requires a key", index)
		}
		if _, exists := seen[filter.Key]; exists {
			return fmt.Errorf("duplicate filter %q", filter.Key)
		}
		seen[filter.Key] = struct{}{}
		if !oneOf(filter.Type, FilterTypeString, FilterTypeBoolean, FilterTypeInteger, FilterTypeDecimal, FilterTypeEnum) {
			return fmt.Errorf("filter %q has unknown type %q", filter.Key, filter.Type)
		}
		values := make(map[string]struct{}, len(filter.AllowedValues))
		for _, allowed := range filter.AllowedValues {
			if allowed.Value == "" {
				return fmt.Errorf("filter %q has an empty allowed value", filter.Key)
			}
			if _, exists := values[allowed.Value]; exists {
				return fmt.Errorf("filter %q has duplicate allowed value %q", filter.Key, allowed.Value)
			}
			values[allowed.Value] = struct{}{}
		}
	}
	return nil
}

func validateSortModes(modes []SortMode) error {
	seen := make(map[string]struct{}, len(modes))
	for index, mode := range modes {
		if strings.TrimSpace(mode.Value) == "" {
			return fmt.Errorf("sort mode %d requires a value", index)
		}
		if _, exists := seen[mode.Value]; exists {
			return fmt.Errorf("duplicate sort mode %q", mode.Value)
		}
		seen[mode.Value] = struct{}{}
	}
	return nil
}

func oneOf[T comparable](value T, allowed ...T) bool {
	return slices.Contains(allowed, value)
}
