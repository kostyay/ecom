package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/kostyay/ecom/internal/app"
	"github.com/kostyay/ecom/internal/output"
	"github.com/kostyay/ecom/internal/version"
	"github.com/kostyay/ecom/provider"
)

type helpFixtureProvider struct {
	result  provider.HelpResult
	err     error
	request provider.HelpRequest
	calls   int
}

func (fixture *helpFixtureProvider) Help(_ context.Context, request provider.HelpRequest) (provider.HelpResult, error) {
	fixture.calls++
	fixture.request = request
	return fixture.result, fixture.err
}

func TestProviderHelpJSONGolden(t *testing.T) {
	fixture, factory := providerHelpFixture(t)
	result := runProviderHelp(t, factory, "provider", "help", "fixture")

	if result.status != 0 || result.stderr != "" {
		t.Fatalf("status = %d; stderr = %q", result.status, result.stderr)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "provider_help.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if result.stdout != string(want) {
		t.Errorf("JSON output does not match golden\ngot:  %s\nwant: %s", result.stdout, want)
	}

	var envelope output.Envelope
	if err := json.Unmarshal([]byte(result.stdout), &envelope); err != nil {
		t.Fatalf("decode output envelope: %v", err)
	}
	if envelope.SchemaVersion != output.SchemaVersion || envelope.Provider != "fixture" {
		t.Errorf("envelope identity = version %q, provider %q", envelope.SchemaVersion, envelope.Provider)
	}
	if fixture.calls != 1 {
		t.Errorf("Help calls = %d, want 1", fixture.calls)
	}
	wantMarket := (provider.Market{Country: "CA", Language: "fr", Currency: "CAD"})
	if fixture.request.Market != wantMarket || envelope.Market != wantMarket {
		t.Errorf("request market = %#v; envelope market = %#v; want %#v", fixture.request.Market, envelope.Market, wantMarket)
	}
}

func TestProviderHelpOutputModes(t *testing.T) {
	_, factory := providerHelpFixture(t)

	table := runProviderHelp(t, factory, "provider", "help", "fixture", "-o", "table")
	if table.status != 0 || table.stderr != "" {
		t.Fatalf("table status = %d; stderr = %q", table.status, table.stderr)
	}
	for _, text := range []string{"Provider Help", "Capabilities", "Filters", "Sort modes", "Page sizes:", "Markets", "Access", "site_changes"} {
		if !strings.Contains(table.stdout, text) {
			t.Errorf("table output does not contain %q:\n%s", text, table.stdout)
		}
	}

	jsonPath := runProviderHelp(t, factory, "provider", "help", "fixture", "-o", `jsonpath={.data.help.pagination.supported_page_sizes[*]}`)
	if jsonPath.status != 0 || jsonPath.stderr != "" || jsonPath.stdout != "24 48 96" {
		t.Errorf("JSONPath result = status %d, stdout %q, stderr %q", jsonPath.status, jsonPath.stdout, jsonPath.stderr)
	}
}

func TestProviderHelpPositionalSelectionAndConflict(t *testing.T) {
	fixture, factory := providerHelpFixture(t)
	result := runProviderHelp(t, factory, "provider", "help", "fixture")
	if result.status != 0 || fixture.calls != 1 {
		t.Fatalf("positional selection = status %d, calls %d, stderr %q", result.status, fixture.calls, result.stderr)
	}

	result = runProviderHelp(t, factory, "provider", "help", "fixture", "--provider", "other")
	assertErrorCode(t, result, provider.ErrorCodeProviderConflict)
	if fixture.calls != 1 {
		t.Errorf("conflicting selection called Help; calls = %d", fixture.calls)
	}

	result = runProviderHelp(t, factory, "provider", "help", "fixture", "--provider", "fixture")
	if result.status != 0 {
		t.Fatalf("matching selection status = %d; stderr = %q", result.status, result.stderr)
	}
}

func TestProviderHelpArgumentAndProviderErrors(t *testing.T) {
	_, factory := providerHelpFixture(t)
	tests := []struct {
		name string
		args []string
		code string
	}{
		{name: "missing provider", args: []string{"provider", "help"}, code: codeCommand},
		{name: "extra argument", args: []string{"provider", "help", "fixture", "extra"}, code: codeCommand},
		{name: "unknown provider", args: []string{"provider", "help", "unknown"}, code: string(provider.ErrorCodeProviderNotFound)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertErrorCode(t, runProviderHelp(t, factory, test.args...), provider.ErrorCode(test.code))
		})
	}
}

func TestProviderHelpErrorsAndValidation(t *testing.T) {
	t.Run("provider error", func(t *testing.T) {
		fixture, factory := providerHelpFixture(t)
		fixture.err = provider.NewError(provider.ErrorCodeAccessBlocked, "help is unavailable", errors.New("private cause"))
		result := runProviderHelp(t, factory, "provider", "help", "fixture")
		assertErrorCode(t, result, provider.ErrorCodeAccessBlocked)
		if strings.Contains(result.stderr, "private cause") {
			t.Errorf("stderr exposed private cause: %s", result.stderr)
		}
	})

	t.Run("invalid help", func(t *testing.T) {
		fixture, factory := providerHelpFixture(t)
		fixture.result.Help.Pagination.SupportedPageSizes = []int{0}
		result := runProviderHelp(t, factory, "provider", "help", "fixture")
		assertErrorCode(t, result, provider.ErrorCodeInvalidProviderResult)
	})

	t.Run("wrong help name", func(t *testing.T) {
		fixture, factory := providerHelpFixture(t)
		fixture.result.Help.Name = "other"
		result := runProviderHelp(t, factory, "provider", "help", "fixture")
		assertErrorCode(t, result, provider.ErrorCodeInvalidProviderResult)
	})
}

func TestProviderHelpDoesNotCreateDatabase(t *testing.T) {
	_, factory := providerHelpFixture(t)
	cachePath := filepath.Join(t.TempDir(), "state", "cache.db")
	result := runProviderHelpWithCache(t, factory, cachePath, "provider", "help", "fixture")
	if result.status != 0 {
		t.Fatalf("status = %d; stderr = %q", result.status, result.stderr)
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cache database exists or stat failed unexpectedly: %v", err)
	}
}

func TestProviderHelpKeepsStdoutAndStderrSeparate(t *testing.T) {
	_, factory := providerHelpFixture(t)
	success := runProviderHelp(t, factory, "provider", "help", "fixture")
	if success.stdout == "" || success.stderr != "" {
		t.Errorf("success streams = stdout %q, stderr %q", success.stdout, success.stderr)
	}

	failure := runProviderHelp(t, factory, "provider", "help", "unknown")
	if failure.stdout != "" || failure.stderr == "" {
		t.Errorf("failure streams = stdout %q, stderr %q", failure.stdout, failure.stderr)
	}
}

func TestProviderHelpAdditionDoesNotChangeVersionCommand(t *testing.T) {
	fixture, factory := providerHelpFixture(t)
	result := runProviderHelp(t, factory, "version", "--json")
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("version result = status %d, stderr %q", result.status, result.stderr)
	}
	var got version.Info
	if err := json.Unmarshal([]byte(result.stdout), &got); err != nil {
		t.Fatalf("decode version output: %v", err)
	}
	if got != version.Current() {
		t.Errorf("version output = %#v, want %#v", got, version.Current())
	}
	if fixture.calls != 0 {
		t.Errorf("version command called provider Help %d times", fixture.calls)
	}
}

type commandResult struct {
	status int
	stdout string
	stderr string
}

func runProviderHelp(t *testing.T, factory *coreapp.Factory, args ...string) commandResult {
	t.Helper()
	return runProviderHelpWithCache(t, factory, filepath.Join(t.TempDir(), "cache.db"), args...)
}

func runProviderHelpWithCache(t *testing.T, factory *coreapp.Factory, cachePath string, args ...string) commandResult {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configText := "provider: configured\nmarket:\n  country: CA\n  language: fr\n  currency: CAD\ncache:\n  path: " + cachePath + "\n"
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	args = append(args, "--config", configPath, "--log-file", filepath.Join(t.TempDir(), "ecom.log"))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	status := run(t.Context(), args, &stdout, &stderr, factory)
	return commandResult{status: status, stdout: stdout.String(), stderr: stderr.String()}
}

func assertErrorCode(t *testing.T, result commandResult, want provider.ErrorCode) {
	t.Helper()
	if result.status != 1 || result.stdout != "" {
		t.Fatalf("error result = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal([]byte(result.stderr), &envelope); err != nil {
		t.Fatalf("decode error output: %v; output = %q", err, result.stderr)
	}
	if envelope.Error.Code != string(want) {
		t.Errorf("error code = %q, want %q; message = %q", envelope.Error.Code, want, envelope.Error.Message)
	}
}

func providerHelpFixture(t *testing.T) (*helpFixtureProvider, *coreapp.Factory) {
	t.Helper()
	fixture := &helpFixtureProvider{result: provider.HelpResult{Help: completeFixtureHelp()}}
	registry := provider.NewRegistry()
	if err := registry.Register(provider.Registration{
		Name: "fixture", SDKAPIVersion: provider.APIVersion, Implementation: fixture,
	}); err != nil {
		t.Fatal(err)
	}
	return fixture, coreapp.NewFactory(registry.Resolve)
}

func completeFixtureHelp() provider.Help {
	return provider.Help{
		Name: "fixture", DisplayName: "Fixture Shop", Description: "Fixture product discovery.",
		Capabilities: []provider.CapabilityHelp{
			{Name: provider.CapabilitySearch, Supported: true, Description: "Find products."},
			{Name: provider.CapabilityBrandSearch, Supported: false, Notes: []string{"Use local brand search."}},
		},
		Search: &provider.SearchHelp{QueryRequired: true, Syntax: "Plain product terms", Examples: []string{"trail bike"}},
		Filters: []provider.FilterDefinition{{
			Key: "brand", Type: provider.FilterTypeEnum, Repeatable: true,
			AllowedValues: []provider.FilterValue{{Value: "acme", Label: "Acme"}},
			AppliesTo:     []provider.CapabilityName{provider.CapabilitySearch},
		}},
		SortModes: []provider.SortMode{{Value: "price-asc", Label: "Lowest price", AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}}},
		Pagination: &provider.PaginationHelp{
			Mode: provider.PaginationPageNumber, FirstPage: 1, DefaultPageSize: 48,
			SupportedPageSizes: []int{24, 48, 96}, ReportsTotalItems: true, ReportsTotalPages: true,
		},
		Markets:   &provider.MarketRestrictions{Countries: []string{"CA"}, Languages: []string{"fr"}, Currencies: []string{"CAD"}},
		Access:    &provider.AccessRequirements{Authentication: provider.AuthenticationNone, Browser: provider.BrowserFallback, SupportsCDP: true, SupportsInteractive: true},
		Transport: []provider.TransportNote{{Mode: provider.TransportHTTP, UseWhen: "Use for normal requests."}, {Mode: provider.TransportBrowser, UseWhen: "Use when JavaScript is required."}},
		Warnings:  []provider.HelpWarning{{Code: "site_changes", Message: "Site changes can affect extraction."}},
		ProviderData: provider.Data{
			"fixture": json.RawMessage(`{"catalog":"ca"}`),
		},
	}
}
