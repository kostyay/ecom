package cli

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	coreapp "github.com/kostyay/ecom/internal/app"
	"github.com/kostyay/ecom/internal/output"
	"github.com/kostyay/ecom/provider"
)

type searchFixtureProvider struct {
	help          provider.Help
	result        provider.ProductPage
	helpErr       error
	searchErr     error
	helpRequests  []provider.HelpRequest
	searchRequest provider.SearchRequest
}

func (fixture *searchFixtureProvider) Help(_ context.Context, request provider.HelpRequest) (provider.HelpResult, error) {
	fixture.helpRequests = append(fixture.helpRequests, request)
	return provider.HelpResult{Help: fixture.help}, fixture.helpErr
}

func (fixture *searchFixtureProvider) Search(ctx context.Context, request provider.SearchRequest) (provider.ProductPage, error) {
	fixture.searchRequest = request
	_ = ctx
	return fixture.result, fixture.searchErr
}

func TestSearchPropagatesExactArgumentsAndWritesJSON(t *testing.T) {
	fixture, factory := searchFixture(t, true)

	result := runSearch(t, factory,
		"search", "trail bike",
		"--filter", "brand=acme", "--filter", "brand=other", "--filter", "discount=true",
		"--sort", "price-asc", "--page", "2", "--page-size", "24",
		"--refresh", "--stale-if-error", "--interactive",
	)
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("status = %d; stderr = %q", result.status, result.stderr)
	}

	request := fixture.searchRequest
	if request.Query != "trail bike" {
		t.Errorf("query = %q", request.Query)
	}
	wantFilters := []provider.Filter{{Key: "brand", Value: "acme"}, {Key: "brand", Value: "other"}, {Key: "discount", Value: "true"}}
	if !reflect.DeepEqual(request.Filters, wantFilters) || request.Sort == nil || request.Sort.Value != "price-asc" || request.Page != (provider.PageRequest{Number: 2, Size: 24}) {
		t.Errorf("search request = %#v", request)
	}
	if !request.Cache.Refresh || !request.Cache.StaleIfError || !request.Interactive || request.Resources == nil {
		t.Errorf("common request flags = %#v", request.Request)
	}
	wantMarket := provider.Market{Country: "CA", Language: "fr", Currency: "CAD"}
	if request.Market != wantMarket || len(fixture.helpRequests) != 1 || fixture.helpRequests[0].Market != wantMarket {
		t.Errorf("markets = search %#v, help %#v", request.Market, fixture.helpRequests)
	}

	var envelope output.Envelope
	if err := json.Unmarshal([]byte(result.stdout), &envelope); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if envelope.Provider != "fixture" || envelope.Page == nil || envelope.Page.Number != 2 || envelope.Cache != nil {
		t.Errorf("envelope = %#v", envelope)
	}
}

func TestSearchOutputModesAndEmptyResults(t *testing.T) {
	_, factory := searchFixture(t, true)
	table := runSearch(t, factory, "search", "helmet", "-o", "table")
	if table.status != 0 || table.stderr != "" || !strings.Contains(table.stdout, "Fixture helmet") || !strings.Contains(table.stdout, "PRODUCT-1") {
		t.Errorf("table = status %d, stdout %q, stderr %q", table.status, table.stdout, table.stderr)
	}
	jsonPath := runSearch(t, factory, "search", "helmet", "-o", `jsonpath={.data.items[*].name}`)
	if jsonPath.status != 0 || jsonPath.stdout != "Fixture helmet" || jsonPath.stderr != "" {
		t.Errorf("JSONPath = status %d, stdout %q, stderr %q", jsonPath.status, jsonPath.stdout, jsonPath.stderr)
	}

	fixture, emptyFactory := searchFixture(t, true)
	fixture.result.Items = nil
	empty := runSearch(t, emptyFactory, "search", "nothing", "-o", "table")
	if empty.status != 0 || !strings.Contains(empty.stdout, "(no products)") {
		t.Errorf("empty table = status %d, stdout %q, stderr %q", empty.status, empty.stdout, empty.stderr)
	}
}

func TestSearchRejectsArgumentsAgainstProviderHelp(t *testing.T) {
	fixture, factory := searchFixture(t, true)
	tests := []struct {
		name string
		args []string
		code provider.ErrorCode
	}{
		{name: "missing query", args: []string{"search"}, code: codeCommand},
		{name: "extra query", args: []string{"search", "one", "two"}, code: codeCommand},
		{name: "all flag", args: []string{"search", "one", "--all"}, code: codeCommand},
		{name: "filter format", args: []string{"search", "one", "--filter", "brand"}, code: provider.ErrorCodeInvalidFilter},
		{name: "unknown filter", args: []string{"search", "one", "--filter", "size=M"}, code: provider.ErrorCodeInvalidFilter},
		{name: "wrong type", args: []string{"search", "one", "--filter", "discount=yes"}, code: provider.ErrorCodeInvalidFilter},
		{name: "wrong enum", args: []string{"search", "one", "--filter", "brand=missing"}, code: provider.ErrorCodeInvalidFilter},
		{name: "not repeatable", args: []string{"search", "one", "--filter", "discount=true", "--filter", "discount=false"}, code: provider.ErrorCodeInvalidFilter},
		{name: "wrong sort", args: []string{"search", "one", "--sort", "popular"}, code: provider.ErrorCodeInvalidFilter},
		{name: "page before first", args: []string{"search", "one", "--page", "0"}, code: provider.ErrorCodeInvalidFilter},
		{name: "page size", args: []string{"search", "one", "--page-size", "25"}, code: provider.ErrorCodeInvalidFilter},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := fixture.searchRequest.Query
			assertErrorCode(t, runSearch(t, factory, test.args...), test.code)
			if fixture.searchRequest.Query != before {
				t.Error("invalid input called provider Search")
			}
		})
	}
}

func TestSearchCapabilityProviderErrorsAndInvalidResults(t *testing.T) {
	t.Run("capability unavailable", func(t *testing.T) {
		fixture, factory := searchFixture(t, false)
		assertErrorCode(t, runSearch(t, factory, "search", "helmet"), provider.ErrorCodeCapabilityUnavailable)
		if fixture.searchRequest.Query != "" {
			t.Error("unsupported provider received Search")
		}
	})
	t.Run("provider error", func(t *testing.T) {
		fixture, factory := searchFixture(t, true)
		fixture.searchErr = provider.NewError(provider.ErrorCodeAccessBlocked, "search is blocked", errors.New("private cause"))
		result := runSearch(t, factory, "search", "helmet")
		assertErrorCode(t, result, provider.ErrorCodeAccessBlocked)
		if strings.Contains(result.stderr, "private cause") {
			t.Errorf("stderr exposed private cause: %s", result.stderr)
		}
	})
	t.Run("invalid result", func(t *testing.T) {
		fixture, factory := searchFixture(t, true)
		fixture.result.Items[0].URL = "://bad"
		assertErrorCode(t, runSearch(t, factory, "search", "helmet"), provider.ErrorCodeInvalidProviderResult)
	})
	t.Run("partial result", func(t *testing.T) {
		fixture, factory := searchFixture(t, true)
		found, parsed := 2, 1
		fixture.result.Warnings = []provider.Warning{{Code: provider.WarningCodePartialParsing, Message: "one card failed", FoundCount: &found, ParsedCount: &parsed}}
		result := runSearch(t, factory, "search", "helmet")
		if result.status != 0 || !strings.Contains(result.stdout, `"partial_parsing"`) {
			t.Errorf("partial result = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
		}
	})
}

func searchFixture(t *testing.T, supportsSearch bool) (*searchFixtureProvider, *coreapp.Factory) {
	t.Helper()
	total, pages, next := 1, 3, false
	fixture := &searchFixtureProvider{
		help: searchFixtureHelp(supportsSearch),
		result: provider.ProductPage{
			Items: []provider.ProductSummary{{
				ID: "PRODUCT-1", URL: "https://shop.example/products/1", Name: "Fixture helmet", Brand: "Acme",
				Price: &provider.Money{Amount: "49.95", Currency: "CAD", Display: "$49.95"}, DetailLevel: provider.DetailLevelSummary,
			}},
			Page: provider.PageInfo{Number: 2, Size: 24, TotalItems: &total, TotalPages: &pages, HasNext: &next},
		},
	}
	registry := provider.NewRegistry()
	capabilities := []provider.CapabilityName(nil)
	if supportsSearch {
		capabilities = append(capabilities, provider.CapabilitySearch)
	}
	if err := registry.Register(provider.Registration{
		Name: "fixture", SDKAPIVersion: provider.APIVersion, Implementation: fixture, Capabilities: capabilities,
	}); err != nil {
		t.Fatal(err)
	}
	return fixture, coreapp.NewFactory(registry.Resolve)
}

func searchFixtureHelp(supportsSearch bool) provider.Help {
	return provider.Help{
		Name:         "fixture",
		Capabilities: []provider.CapabilityHelp{{Name: provider.CapabilitySearch, Supported: supportsSearch}},
		Search:       &provider.SearchHelp{QueryRequired: true},
		Filters: []provider.FilterDefinition{
			{Key: "brand", Type: provider.FilterTypeEnum, Repeatable: true, AllowedValues: []provider.FilterValue{{Value: "acme"}, {Value: "other"}}, AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}},
			{Key: "discount", Type: provider.FilterTypeBoolean, AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}},
			{Key: "category-only", Type: provider.FilterTypeString, AppliesTo: []provider.CapabilityName{provider.CapabilityCategoryItems}},
		},
		SortModes: []provider.SortMode{{Value: "price-asc", AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}}},
		Pagination: &provider.PaginationHelp{
			Mode: provider.PaginationPageNumber, FirstPage: 1, DefaultPageSize: 24,
			SupportedPageSizes: []int{24, 48}, ReportsTotalItems: true, ReportsTotalPages: true,
		},
	}
}

func runSearch(t *testing.T, factory *coreapp.Factory, args ...string) commandResult {
	t.Helper()
	args = append(args, "--provider", "fixture")
	return runProviderHelpWithCache(t, factory, filepath.Join(t.TempDir(), "cache.db"), args...)
}

var _ provider.HelpProvider = (*searchFixtureProvider)(nil)
var _ provider.SearchProvider = (*searchFixtureProvider)(nil)
