package conformance_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/provider/conformance"
)

type fixtureProvider struct {
	resources provider.ResourceService
	helpName  string
	invalid   bool
}

func (p *fixtureProvider) Help(context.Context, provider.HelpRequest) (provider.HelpResult, error) {
	return provider.HelpResult{Help: provider.Help{
		Name: p.helpName,
		Capabilities: []provider.CapabilityHelp{
			{Name: provider.CapabilitySearch, Supported: true},
			{Name: provider.CapabilityFilters, Supported: true},
		},
		Filters:   []provider.FilterDefinition{{Key: "discount", Type: provider.FilterTypeBoolean}},
		SortModes: []provider.SortMode{{Value: "price-asc"}},
		Pagination: &provider.PaginationHelp{
			Mode: provider.PaginationPageNumber, FirstPage: 1,
			DefaultPageSize: 24, SupportedPageSizes: []int{24, 48},
		},
	}}, nil
}

func (p *fixtureProvider) Search(ctx context.Context, request provider.SearchRequest) (provider.ProductPage, error) {
	if request.Query == "bad-filter" {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidFilter, "invalid filter", nil)
	}
	resources := request.Resources
	if resources == nil {
		resources = p.resources
	}
	response, err := resources.Fetch(ctx, provider.ResourceRequest{
		Method: "GET", URL: "https://shop.example/search",
		Query:  []provider.RequestValue{{Name: "q", Values: []string{request.Query}}},
		Market: request.Market, Cache: request.Cache, Interactive: request.Interactive,
	})
	if err != nil {
		return provider.ProductPage{}, err
	}
	found, parsed := 2, 1
	productURL := "https://shop.example/items/helmet"
	if p.invalid {
		productURL = "/items/helmet"
	}
	return provider.ProductPage{
		Items: []provider.ProductSummary{{
			ID: "helmet", URL: productURL, Name: string(response.Body),
			Price:       &provider.Money{Amount: "79.95", Currency: "EUR", Display: "€79.95"},
			DetailLevel: provider.DetailLevelSummary, RetrievedAt: response.RetrievedAt,
		}},
		Page: provider.PageInfo{Number: 1, Size: 24},
		Warnings: []provider.Warning{{
			Code: provider.WarningCodePartialParsing, Message: "one result could not be parsed",
			FoundCount: &found, ParsedCount: &parsed,
		}},
	}, nil
}

func (*fixtureProvider) Filters(context.Context, provider.FiltersRequest) (provider.FiltersResult, error) {
	return provider.FiltersResult{
		Filters:   []provider.FilterDefinition{{Key: "discount", Type: provider.FilterTypeBoolean}},
		SortModes: []provider.SortMode{{Value: "price-asc"}},
	}, nil
}

func TestConformingProvider(t *testing.T) {
	retrievedAt := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	resources := conformance.NewFixtureService(conformance.ResourceFixture{
		Response: provider.ResourceResponse{Body: []byte("Trail Helmet"), StatusCode: 200, RetrievedAt: retrievedAt},
		CheckRequest: func(request provider.ResourceRequest) error {
			if request.Method != "GET" || request.URL != "https://shop.example/search" {
				return fmt.Errorf("request = %s %s", request.Method, request.URL)
			}
			if !request.Cache.Refresh || !request.Cache.StaleIfError || !request.Interactive {
				return fmt.Errorf("common request policy was not propagated")
			}
			return nil
		},
	})
	implementation := &fixtureProvider{resources: resources, helpName: "fixture"}

	conformance.Run(t, conformance.Suite{
		Registration: provider.Registration{
			Name: "fixture", SDKAPIVersion: provider.APIVersion, Implementation: implementation,
			Capabilities: []provider.CapabilityName{provider.CapabilitySearch, provider.CapabilityFilters},
		},
		Resources: resources,
		Cases: []conformance.OperationCase{
			{
				Name: "saved search response", Capability: provider.CapabilitySearch,
				Invoke: func(ctx context.Context, p provider.Provider) (any, error) {
					return p.Search(ctx, provider.SearchRequest{
						Request: provider.Request{
							Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"},
							Cache:  provider.CachePolicy{Refresh: true, StaleIfError: true}, Interactive: true, Resources: resources,
						},
						Query: "helmet", Page: provider.PageRequest{Number: 1, Size: 24},
					})
				},
				WantPartialWarning: true,
				Check: func(value any) error {
					page := value.(provider.ProductPage)
					if page.Items[0].Name != "Trail Helmet" {
						return fmt.Errorf("product name = %q", page.Items[0].Name)
					}
					return nil
				},
			},
			{
				Name: "structured invalid filter", Capability: provider.CapabilitySearch,
				Invoke: func(ctx context.Context, p provider.Provider) (any, error) {
					return p.Search(ctx, provider.SearchRequest{Query: "bad-filter"})
				},
				WantErrorCode: provider.ErrorCodeInvalidFilter,
			},
			{
				Name: "filter definitions", Capability: provider.CapabilityFilters,
				Invoke: func(ctx context.Context, p provider.Provider) (any, error) {
					return p.Filters(ctx, provider.FiltersRequest{Capability: provider.CapabilitySearch})
				},
			},
		},
	})
}

func TestCheckReportsDeliberateContractFailures(t *testing.T) {
	resources := conformance.NewFixtureService(conformance.ResourceFixture{
		Response: provider.ResourceResponse{Body: []byte("Trail Helmet")},
	})
	implementation := &fixtureProvider{resources: resources, helpName: "wrong-name", invalid: true}
	report := conformance.Check(context.Background(), conformance.Suite{
		Registration: provider.Registration{
			Name: "fixture", SDKAPIVersion: provider.APIVersion, Implementation: implementation,
			Capabilities: []provider.CapabilityName{provider.CapabilitySearch, provider.CapabilityFilters},
		},
		Resources: resources,
		Cases: []conformance.OperationCase{{
			Name: "invalid product", Capability: provider.CapabilitySearch,
			Invoke: func(ctx context.Context, p provider.Provider) (any, error) {
				return p.Search(ctx, provider.SearchRequest{Query: "helmet"})
			},
		}},
	})
	if report.Passed() {
		t.Fatal("Check() passed an invalid provider")
	}
	wantFailures := []string{"help_name", "operation_search_invalid_product", "case_required_filters"}
	for _, name := range wantFailures {
		if !hasFailure(report, name) {
			t.Errorf("Check() did not report %q; checks = %#v", name, report.Checks)
		}
	}
}

func TestCheckReportsInvalidRegistrationWithoutCallingProvider(t *testing.T) {
	report := conformance.Check(context.Background(), conformance.Suite{Registration: provider.Registration{
		Name: "Invalid Name", SDKAPIVersion: provider.APIVersion, Implementation: &fixtureProvider{},
	}})
	if report.Passed() || !hasFailure(report, "registration") {
		t.Fatalf("Check() report = %#v, want registration failure", report)
	}
}

func TestFixtureServiceIsOfflineOrderedAndCancelable(t *testing.T) {
	service := conformance.NewFixtureService(conformance.ResourceFixture{Response: provider.ResourceResponse{Body: []byte("first")}})
	response, err := service.Fetch(context.Background(), provider.ResourceRequest{Method: "GET", URL: "https://shop.example/one"})
	if err != nil || string(response.Body) != "first" {
		t.Fatalf("first Fetch() = %q, %v", response.Body, err)
	}
	if _, err := service.Fetch(context.Background(), provider.ResourceRequest{Method: "GET", URL: "https://shop.example/two"}); err == nil || !strings.Contains(err.Error(), "no resource fixture") {
		t.Fatalf("second Fetch() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Fetch(ctx, provider.ResourceRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Fetch() error = %v", err)
	}
	if got := len(service.Requests()); got != 2 {
		t.Fatalf("request count = %d, want 2", got)
	}
}

func hasFailure(report conformance.Report, name string) bool {
	for _, check := range report.Checks {
		if check.Name == name && check.Err != nil {
			return true
		}
	}
	return false
}

var (
	_ provider.HelpProvider    = (*fixtureProvider)(nil)
	_ provider.SearchProvider  = (*fixtureProvider)(nil)
	_ provider.FiltersProvider = (*fixtureProvider)(nil)
)
