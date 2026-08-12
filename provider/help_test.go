package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderHelpJSONGolden(t *testing.T) {
	tests := []struct {
		name string
		help Help
	}{
		{
			name: "complete",
			help: Help{
				Name:        "bike-discount",
				DisplayName: "Bike-Discount",
				Description: "Find bicycle products and provider-shown deals.",
				Capabilities: []CapabilityHelp{
					{Name: CapabilitySearch, Supported: true, Description: "Search products."},
					{Name: CapabilityBrandSearch, Supported: false, Notes: []string{"Use local brand search."}},
				},
				Search: &SearchHelp{
					QueryRequired: true,
					Syntax:        "Plain product terms",
					Examples:      []string{"Shimano XT M8100"},
					Notes:         []string{"Search returns products only."},
				},
				Filters: []FilterDefinition{
					{
						Key:           "brand",
						Type:          FilterTypeEnum,
						Description:   "Limit results to one or more brands.",
						Repeatable:    true,
						AllowedValues: []FilterValue{{Value: "shimano", Label: "Shimano", Description: "Shimano products."}},
						Examples:      []string{"brand=shimano"},
						AppliesTo:     []CapabilityName{CapabilitySearch, CapabilityDeals},
						Notes:         []string{"Use the provider brand ID."},
					},
				},
				SortModes: []SortMode{
					{Value: "price-asc", Label: "Lowest price", Description: "Sort by displayed item price.", AppliesTo: []CapabilityName{CapabilitySearch, CapabilityDeals}},
				},
				Pagination: &PaginationHelp{
					Mode:               PaginationPageNumber,
					FirstPage:          1,
					DefaultPageSize:    48,
					SupportedPageSizes: []int{24, 48, 96},
					ReportsTotalItems:  true,
					ReportsTotalPages:  true,
					Notes:              []string{"Each command returns one page."},
				},
				Markets: &MarketRestrictions{
					Countries:  []string{"DE", "FR"},
					Languages:  []string{"de", "en"},
					Currencies: []string{"EUR"},
					Notes:      []string{"The site can return its actual currency."},
				},
				Access: &AccessRequirements{
					Authentication:      AuthenticationNone,
					Browser:             BrowserFallback,
					SupportsCDP:         true,
					SupportsInteractive: true,
					Notes:               []string{"A challenge can require manual action."},
				},
				Transport: []TransportNote{
					{Mode: TransportHTTP, UseWhen: "Use for normal requests."},
					{Mode: TransportBrowser, UseWhen: "Use when JavaScript is required.", Notes: []string{"The Core owns the browser."}},
				},
				Warnings: []HelpWarning{
					{Code: "site_changes", Message: "Site changes can affect extraction."},
				},
				ProviderData: Data{"bike-discount": json.RawMessage(`{"catalog_id":"de"}`)},
			},
		},
		{name: "minimal", help: Help{Name: "example"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.help.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}

			got, err := json.MarshalIndent(test.help, "", "  ")
			if err != nil {
				t.Fatalf("marshal help: %v", err)
			}
			got = append(got, '\n')

			goldenPath := filepath.Join("testdata", "provider_help_"+test.name+".golden.json")
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden file: %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("JSON mismatch\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestProviderHelpValidate(t *testing.T) {
	tests := []struct {
		name string
		help Help
	}{
		{name: "invalid provider name", help: Help{Name: "Bike Discount"}},
		{name: "duplicate capability", help: Help{Name: "example", Capabilities: []CapabilityHelp{{Name: CapabilitySearch}, {Name: CapabilitySearch}}}},
		{name: "unknown filter type", help: Help{Name: "example", Filters: []FilterDefinition{{Key: "brand", Type: FilterType("object")}}}},
		{name: "duplicate filter", help: Help{Name: "example", Filters: []FilterDefinition{{Key: "brand", Type: FilterTypeString}, {Key: "brand", Type: FilterTypeString}}}},
		{name: "unsupported default page size", help: Help{Name: "example", Pagination: &PaginationHelp{Mode: PaginationPageNumber, DefaultPageSize: 48, SupportedPageSizes: []int{24}}}},
		{name: "unknown browser requirement", help: Help{Name: "example", Access: &AccessRequirements{Authentication: AuthenticationNone, Browser: BrowserRequirement("sometimes")}}},
		{name: "unknown transport", help: Help{Name: "example", Transport: []TransportNote{{Mode: TransportMode("ftp")}}}},
		{name: "incomplete warning", help: Help{Name: "example", Warnings: []HelpWarning{{Code: "site_changes"}}}},
		{name: "invalid provider data", help: Help{Name: "example", ProviderData: Data{"example": json.RawMessage(`{`)}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.help.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want an error")
			}
		})
	}
}

func TestMinimalProviderHelpIsValid(t *testing.T) {
	if err := (Help{Name: "example"}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
