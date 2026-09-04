package bikediscount_test

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/kostyay/ecom/provider"
	_ "github.com/kostyay/ecom/providers/bikediscount"
)

const providerName = "bike-discount"

type helpExpectation struct {
	Name                string                    `json:"name"`
	ActiveCapabilities  []provider.CapabilityName `json:"active_capabilities"`
	PlannedCapabilities []provider.CapabilityName `json:"planned_capabilities"`
	PageSizes           []int                     `json:"page_sizes"`
	Countries           []string                  `json:"countries"`
	Languages           []string                  `json:"languages"`
	Currencies          []string                  `json:"currencies"`
}

func TestBlankImportRegistersProvider(t *testing.T) {
	registered, found := provider.Lookup(providerName)
	if !found {
		t.Fatal("blank import did not register bike-discount")
	}
	if registered.Name() != providerName {
		t.Errorf("provider name = %q, want %q", registered.Name(), providerName)
	}
	wantCapabilities := []provider.CapabilityName{
		provider.CapabilitySearch, provider.CapabilityCategories, provider.CapabilityCategoryItems,
		provider.CapabilityBrands, provider.CapabilityBrandItems, provider.CapabilityDeals,
		provider.CapabilityFilters, provider.CapabilityItem, provider.CapabilityVariantSelection,
	}
	if capabilities := registered.Capabilities(); !reflect.DeepEqual(capabilities, wantCapabilities) {
		t.Errorf("active capabilities = %v, want %v", capabilities, wantCapabilities)
	}

	_, err := registered.Search(t.Context(), provider.SearchRequest{})
	if !errors.Is(err, provider.ErrorCodeInvalidFilter) {
		t.Errorf("Search() error = %v, want invalid_filter", err)
	}
}

func TestOfflineHelpMatchesFixture(t *testing.T) {
	registered, found := provider.Lookup(providerName)
	if !found {
		t.Fatal("provider is not registered")
	}
	result, err := registered.Help(t.Context(), provider.HelpRequest{
		Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"}})
	if err != nil {
		t.Fatalf("Help() error = %v", err)
	}
	if err := result.Help.Validate(); err != nil {
		t.Fatalf("Help.Validate() error = %v", err)
	}

	fixtureData, err := os.ReadFile("testdata/help_expectation.json")
	if err != nil {
		t.Fatal(err)
	}
	var want helpExpectation
	if err := json.Unmarshal(fixtureData, &want); err != nil {
		t.Fatal(err)
	}

	active := make([]provider.CapabilityName, 0)
	planned := make([]provider.CapabilityName, 0, len(result.Help.Capabilities))
	for _, capability := range result.Help.Capabilities {
		if capability.Supported {
			active = append(active, capability.Name)
		} else {
			planned = append(planned, capability.Name)
		}
	}
	got := helpExpectation{
		Name: result.Help.Name, ActiveCapabilities: active, PlannedCapabilities: planned,
		PageSizes: result.Help.Pagination.SupportedPageSizes,
		Countries: result.Help.Markets.Countries, Languages: result.Help.Markets.Languages,
		Currencies: result.Help.Markets.Currencies,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("help summary = %#v, want %#v", got, want)
	}
	if result.Help.Search == nil || !result.Help.Search.QueryRequired || len(result.Help.Search.Notes) == 0 ||
		len(result.Help.Filters) != 2 || len(result.Help.SortModes) != 1 || result.Help.SortModes[0].Value != "standard" {
		t.Error("help does not match the implemented search and product listing syntax")
	}
	for _, definition := range result.Help.Filters {
		if !slices.Contains(definition.AppliesTo, provider.CapabilityDeals) {
			t.Errorf("filter %q does not apply to deals", definition.Key)
		}
	}
	if !slices.Contains(result.Help.SortModes[0].AppliesTo, provider.CapabilityDeals) {
		t.Error("standard sort does not apply to deals")
	}
}

func TestProviderConfigurationValidation(t *testing.T) {
	registered, found := provider.Lookup(providerName)
	if !found {
		t.Fatal("provider is not registered")
	}
	for _, configuration := range []map[string]any{
		nil,
		{},
		{"page_size": 48},
		{"page_size": float64(48)},
		{"page_size": json.Number("48")},
	} {
		if err := registered.ValidateConfig(configuration); err != nil {
			t.Errorf("ValidateConfig(%v) error = %v", configuration, err)
		}
	}
	for _, configuration := range []map[string]any{
		{"page_size": 24},
		{"page_size": "48"},
		{"unknown": true},
	} {
		if err := registered.ValidateConfig(configuration); err == nil {
			t.Errorf("ValidateConfig(%v) did not return an error", configuration)
		}
	}
}

func TestProviderRejectsShippingPricePolicy(t *testing.T) {
	registered, found := provider.Lookup(providerName)
	if !found {
		t.Fatal("provider is not registered")
	}

	_, err := registered.Help(t.Context(), provider.HelpRequest{
		Market:  provider.Market{Country: "DE", Language: "en", Currency: "EUR"},
		Pricing: provider.PricingPolicy{IncludeShipping: true}})
	if !errors.Is(err, provider.ErrorCodeInvalidProviderConfig) {
		t.Fatalf("Help() error = %v, want invalid_provider_config", err)
	}
}
