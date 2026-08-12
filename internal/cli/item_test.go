package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	coreapp "github.com/kostyay/ecom/internal/app"
	"github.com/kostyay/ecom/internal/output"
	"github.com/kostyay/ecom/provider"
)

type itemFixtureProvider struct {
	help            provider.Help
	result          provider.ItemResult
	itemErr         error
	returnUnmatched bool
	requests        []provider.ItemRequest
}

func (fixture *itemFixtureProvider) Help(context.Context, provider.HelpRequest) (provider.HelpResult, error) {
	return provider.HelpResult{Help: fixture.help}, nil
}

func (fixture *itemFixtureProvider) Item(_ context.Context, request provider.ItemRequest) (provider.ItemResult, error) {
	fixture.requests = append(fixture.requests, request)
	if fixture.itemErr != nil {
		return provider.ItemResult{}, fixture.itemErr
	}
	if parsed, err := url.Parse(request.IDOrURL); err == nil && parsed.IsAbs() && parsed.Host != "shop.example" {
		return provider.ItemResult{}, provider.NewError(provider.ErrorCodeInvalidFilter, "item URL is not owned by provider; use https://shop.example/items/PRODUCT-1", nil)
	}
	result := fixture.result
	if len(request.Variants) == 0 {
		return result, nil
	}
	want := []provider.VariantSelection{{Key: "color", Value: "black"}, {Key: "size", Value: "M"}}
	if !reflect.DeepEqual(request.Variants, want) {
		return provider.ItemResult{}, provider.NewError(
			provider.ErrorCodeVariantNotFound,
			"variant was not found; valid choices: color=black, color=blue, size=M, size=L",
			nil,
		)
	}
	selected := result.Item.Variants[0]
	selected.Attributes = append([]provider.Attribute(nil), selected.Attributes...)
	selected.Selected = true
	if fixture.returnUnmatched {
		selected.Attributes[1].Value = "L"
	}
	result.Item.SelectedVariant = &selected
	result.Item.Price = selected.Price
	return result, nil
}

func TestItemGetsIDsAndURLsWithVisibleVariantsAndPrices(t *testing.T) {
	fixture, factory := itemFixture(t, true)
	byID := runItem(t, factory, "item", "PRODUCT-1")
	if byID.status != 0 || byID.stderr != "" {
		t.Fatalf("ID result = status %d, stderr %q", byID.status, byID.stderr)
	}
	var envelope output.Envelope
	if err := json.Unmarshal([]byte(byID.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	encoded := byID.stdout
	for _, value := range []string{`"variants"`, `"price_range"`, `"amount":"49.95"`, `"amount":"59.95"`} {
		if !strings.Contains(encoded, value) {
			t.Errorf("default JSON does not contain %s: %s", value, encoded)
		}
	}
	if envelope.Provider != "fixture" || fixture.requests[0].IDOrURL != "PRODUCT-1" {
		t.Errorf("envelope = %#v; request = %#v", envelope, fixture.requests[0])
	}

	byURL := runItem(t, factory, "item", "https://shop.example/items/PRODUCT-1?language=en", "-o", "table")
	if byURL.status != 0 || byURL.stderr != "" || !strings.Contains(byURL.stdout, "Variants") || !strings.Contains(byURL.stdout, "$49.95 – $59.95") {
		t.Errorf("URL table = status %d, stdout %q, stderr %q", byURL.status, byURL.stdout, byURL.stderr)
	}
	if fixture.requests[1].IDOrURL != "https://shop.example/items/PRODUCT-1?language=en" {
		t.Errorf("URL request = %#v", fixture.requests[1])
	}
}

func TestItemSelectsAnExactVariantAndPassesPolicy(t *testing.T) {
	fixture, factory := itemFixture(t, true)
	result := runItem(t, factory, "item", "PRODUCT-1",
		"--variant", "color=black", "--variant", "size=M",
		"--refresh", "--stale-if-error", "--interactive",
		"-o", `jsonpath={.data.item.selected_variant.price.display}`,
	)
	if result.status != 0 || result.stderr != "" || result.stdout != "$49.95" {
		t.Fatalf("selection = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	request := fixture.requests[0]
	want := []provider.VariantSelection{{Key: "color", Value: "black"}, {Key: "size", Value: "M"}}
	if !reflect.DeepEqual(request.Variants, want) || !request.Cache.Refresh || !request.Cache.StaleIfError || !request.Interactive || request.Resources == nil {
		t.Errorf("item request = %#v", request)
	}
}

func TestItemRejectsInvalidURLsVariantsAndProviderResults(t *testing.T) {
	fixture, factory := itemFixture(t, true)
	tests := []struct {
		name    string
		args    []string
		code    provider.ErrorCode
		message string
	}{
		{name: "missing argument", args: []string{"item"}, code: codeCommand},
		{name: "malformed URL", args: []string{"item", "https://"}, code: provider.ErrorCodeInvalidFilter},
		{name: "unsupported URL scheme", args: []string{"item", "ftp://shop.example/item"}, code: provider.ErrorCodeInvalidFilter},
		{name: "foreign URL", args: []string{"item", "https://other.example/item"}, code: provider.ErrorCodeInvalidFilter, message: "not owned"},
		{name: "bad variant format", args: []string{"item", "PRODUCT-1", "--variant", "size"}, code: provider.ErrorCodeInvalidFilter},
		{name: "duplicate variant key", args: []string{"item", "PRODUCT-1", "--variant", "size=M", "--variant", "size=L"}, code: provider.ErrorCodeInvalidFilter},
		{name: "variant not found", args: []string{"item", "PRODUCT-1", "--variant", "size=XS"}, code: provider.ErrorCodeVariantNotFound, message: "valid choices: color=black, color=blue, size=M, size=L"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := runItem(t, factory, test.args...)
			assertErrorCode(t, result, test.code)
			if test.message != "" && !strings.Contains(result.stderr, test.message) {
				t.Errorf("stderr = %q, want %q", result.stderr, test.message)
			}
		})
	}

	fixture.result.Item.DetailLevel = provider.DetailLevelSummary
	assertErrorCode(t, runItem(t, factory, "item", "PRODUCT-1"), provider.ErrorCodeInvalidProviderResult)
}

func TestItemChecksCapabilitiesAndKeepsSafeProviderErrors(t *testing.T) {
	fixture, factory := itemFixture(t, false)
	assertErrorCode(t, runItem(t, factory, "item", "PRODUCT-1", "--variant", "size=M"), provider.ErrorCodeCapabilityUnavailable)
	if len(fixture.requests) != 0 {
		t.Error("unsupported variant selection called Item")
	}

	fixture, factory = itemFixture(t, true)
	fixture.itemErr = provider.NewError(provider.ErrorCodeAccessBlocked, "item access is blocked", errors.New("private cause"))
	result := runItem(t, factory, "item", "PRODUCT-1")
	assertErrorCode(t, result, provider.ErrorCodeAccessBlocked)
	if strings.Contains(result.stderr, "private cause") {
		t.Errorf("stderr exposed private cause: %s", result.stderr)
	}
}

func TestItemListsVisibleChoicesWhenProviderDoesNotMatchExactSelection(t *testing.T) {
	fixture, factory := itemFixture(t, true)
	fixture.returnUnmatched = true
	result := runItem(t, factory, "item", "PRODUCT-1", "--variant", "color=black", "--variant", "size=M")
	assertErrorCode(t, result, provider.ErrorCodeVariantNotFound)
	if !strings.Contains(result.stderr, "valid choices: color=black, color=blue, size=L, size=M") {
		t.Errorf("stderr does not list visible choices: %s", result.stderr)
	}
}

func itemFixture(t *testing.T, variantSelection bool) (*itemFixtureProvider, *coreapp.Factory) {
	t.Helper()
	minimum := provider.Money{Amount: "49.95", Currency: "CAD", Display: "$49.95"}
	maximum := provider.Money{Amount: "59.95", Currency: "CAD", Display: "$59.95"}
	capabilityHelp := []provider.CapabilityHelp{{Name: provider.CapabilityItem, Supported: true}}
	capabilities := []provider.CapabilityName{provider.CapabilityItem}
	if variantSelection {
		capabilityHelp = append(capabilityHelp, provider.CapabilityHelp{Name: provider.CapabilityVariantSelection, Supported: true})
		capabilities = append(capabilities, provider.CapabilityVariantSelection)
	}
	fixture := &itemFixtureProvider{
		help: provider.Help{Name: "fixture", Capabilities: capabilityHelp},
		result: provider.ItemResult{Item: provider.ItemDetail{
			ProductSummary: provider.ProductSummary{
				ID: "PRODUCT-1", URL: "https://shop.example/items/PRODUCT-1", Name: "Fixture helmet", Brand: "Acme",
				PriceRange: &provider.PriceRange{Minimum: minimum, Maximum: maximum},
				Variants: []provider.Variant{
					{ID: "black-m", Attributes: []provider.Attribute{{Name: "color", Value: "black"}, {Name: "size", Value: "M"}}, Price: &minimum, Availability: provider.AvailabilityInStock},
					{ID: "blue-l", Attributes: []provider.Attribute{{Name: "color", Value: "blue"}, {Name: "size", Value: "L"}}, Price: &maximum, Availability: provider.AvailabilityInStock},
				},
				DetailLevel: provider.DetailLevelFull,
			},
			Description: "A test helmet.",
		}},
	}
	registry := provider.NewRegistry()
	if err := registry.Register(provider.Registration{
		Name: "fixture", SDKAPIVersion: provider.APIVersion, Implementation: fixture, Capabilities: capabilities,
	}); err != nil {
		t.Fatal(err)
	}
	return fixture, coreapp.NewFactory(registry.Resolve)
}

func runItem(t *testing.T, factory *coreapp.Factory, args ...string) commandResult {
	t.Helper()
	args = append(args, "--provider", "fixture")
	return runProviderHelpWithCache(t, factory, filepath.Join(t.TempDir(), "cache.db"), args...)
}

var _ provider.HelpProvider = (*itemFixtureProvider)(nil)
var _ provider.ItemProvider = (*itemFixtureProvider)(nil)
