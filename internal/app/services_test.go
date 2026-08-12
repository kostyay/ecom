package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/internal/storage/sqlite"
	"github.com/kostyay/ecom/internal/transport"
	"github.com/kostyay/ecom/provider"
)

type fixtureProvider struct{}

func (*fixtureProvider) Help(context.Context, provider.HelpRequest) (provider.HelpResult, error) {
	return provider.HelpResult{Help: provider.Help{Name: "fixture"}}, nil
}

type configValidatingProvider struct {
	fixtureProvider
	configuration map[string]interface{}
}

func (p *configValidatingProvider) ValidateConfig(configuration map[string]interface{}) error {
	p.configuration = configuration
	return errors.New("invalid fixture configuration")
}

func TestFactoryProviderSelectionAndComposition(t *testing.T) {
	registry := fixtureRegistry(t)
	settings := validSettings(filepath.Join(t.TempDir(), "state", "cache.db"))
	settings.Pricing.IncludeShipping = true
	factory := NewFactory(registry.Resolve)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	services, err := factory.NewServices(t.Context(), settings, "fixture", logger)
	if err != nil {
		t.Fatalf("NewServices() error = %v", err)
	}
	t.Cleanup(func() { _ = services.Close() })

	if services.Provider.Name() != "fixture" {
		t.Errorf("provider = %q", services.Provider.Name())
	}
	wantMarket := provider.Market{Country: "FR", Language: "fr", Currency: "EUR"}
	if services.Market != wantMarket || services.Request().Market != wantMarket {
		t.Errorf("market = %#v; request = %#v", services.Market, services.Request())
	}
	if !services.Request().Pricing.IncludeShipping {
		t.Errorf("pricing = %#v", services.Request().Pricing)
	}
	if services.Resources == nil || services.Maintenance == nil || services.Logger != logger {
		t.Error("shared services are incomplete")
	}
	wantPath, _ := filepath.Abs(settings.Cache.Path)
	if services.DatabasePath != wantPath {
		t.Errorf("database path = %q, want %q", services.DatabasePath, wantPath)
	}
}

func TestFactoryProviderContextDoesNotCreateResources(t *testing.T) {
	registry := fixtureRegistry(t)
	factory := NewFactory(registry.Resolve)
	openCalls := 0
	resourceCalls := 0
	factory.openDatabase = func(context.Context, string) (*sqlite.Database, error) {
		openCalls++
		return nil, errors.New("must not open")
	}
	factory.newResources = func(*sqlite.Database, *http.Client, string, config.Settings) (*transport.ResourceService, error) {
		resourceCalls++
		return nil, errors.New("must not create")
	}
	settings := validSettings(filepath.Join(t.TempDir(), "cache.db"))

	providerContext, err := factory.NewProviderContext(settings, "fixture", slog.Default())
	if err != nil {
		t.Fatalf("NewProviderContext() error = %v", err)
	}
	if providerContext.Provider.Name() != "fixture" || providerContext.Market.Country != "FR" {
		t.Errorf("provider context = %#v", providerContext)
	}
	if openCalls != 0 || resourceCalls != 0 {
		t.Errorf("resource calls = database %d, transport %d; want zero", openCalls, resourceCalls)
	}
}

func TestFactoryValidatesProviderConfiguration(t *testing.T) {
	implementation := &configValidatingProvider{}
	registry := provider.NewRegistry()
	if err := registry.Register(provider.Registration{
		Name: "fixture", SDKAPIVersion: provider.APIVersion, Implementation: implementation,
	}); err != nil {
		t.Fatal(err)
	}
	settings := validSettings(filepath.Join(t.TempDir(), "cache.db"))
	settings.Providers = map[string]map[string]interface{}{"fixture": {"page_size": 48}}

	_, err := NewFactory(registry.Resolve).NewProviderContext(settings, "fixture", slog.Default())
	if !errors.Is(err, provider.ErrorCodeInvalidProviderConfig) {
		t.Fatalf("NewProviderContext() error = %v, want invalid_provider_config", err)
	}
	if implementation.configuration["page_size"] != 48 {
		t.Errorf("configuration = %#v", implementation.configuration)
	}
}

func TestFactoryMaintenanceServicesDoNotResolveProviderOrCreateTransports(t *testing.T) {
	resolveCalls := 0
	resourceCalls := 0
	factory := NewFactory(func(string) (provider.Provider, error) {
		resolveCalls++
		return nil, errors.New("must not resolve")
	})
	factory.newResources = func(*sqlite.Database, *http.Client, string, config.Settings) (*transport.ResourceService, error) {
		resourceCalls++
		return nil, errors.New("must not create")
	}
	settings := validSettings(filepath.Join(t.TempDir(), "cache.db"))

	services, err := factory.NewMaintenanceServices(t.Context(), settings)
	if err != nil {
		t.Fatalf("NewMaintenanceServices() error = %v", err)
	}
	if services.Maintenance == nil || services.Market.Country != "FR" || services.DatabasePath == "" {
		t.Errorf("maintenance services = %#v", services)
	}
	if resolveCalls != 0 || resourceCalls != 0 {
		t.Errorf("provider calls = resolve %d, resources %d; want zero", resolveCalls, resourceCalls)
	}
	if err := services.Close(); err != nil {
		t.Fatal(err)
	}
	if err := services.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestFactoryRejectsProviderBeforeOpeningDatabase(t *testing.T) {
	registry := fixtureRegistry(t)
	factory := NewFactory(registry.Resolve)
	openCalls := 0
	factory.openDatabase = func(context.Context, string) (*sqlite.Database, error) {
		openCalls++
		return nil, errors.New("must not open")
	}
	settings := validSettings(filepath.Join(t.TempDir(), "cache.db"))

	for _, test := range []struct {
		name string
		code provider.ErrorCode
	}{
		{name: "", code: provider.ErrorCodeProviderRequired},
		{name: "   ", code: provider.ErrorCodeProviderRequired},
		{name: "unknown", code: provider.ErrorCodeProviderNotFound},
	} {
		_, err := factory.NewServices(t.Context(), settings, test.name, slog.Default())
		if !errors.Is(err, test.code) {
			t.Errorf("NewServices(%q) error = %v, want %s", test.name, err, test.code)
		}
	}
	if openCalls != 0 {
		t.Errorf("database open calls = %d, want 0", openCalls)
	}
}

func TestFactoryDatabasePath(t *testing.T) {
	factory := NewFactory(nil)
	factory.userCacheDir = func() (string, error) { return "/cache-root", nil }

	path, err := factory.DatabasePath("")
	if err != nil || path != filepath.Join("/cache-root", "ecom", "cache.db") {
		t.Fatalf("DatabasePath(default) = %q, %v", path, err)
	}
	explicit := filepath.Join(t.TempDir(), "explicit.db")
	path, err = factory.DatabasePath(explicit)
	if err != nil || path != explicit {
		t.Fatalf("DatabasePath(explicit) = %q, %v", path, err)
	}

	factory.userCacheDir = func() (string, error) { return "", errors.New("unavailable") }
	if _, err := factory.DatabasePath(""); err == nil || !strings.Contains(err.Error(), "user cache") {
		t.Fatalf("DatabasePath() error = %v", err)
	}
}

func TestServicesCloseIsIdempotent(t *testing.T) {
	registry := fixtureRegistry(t)
	factory := NewFactory(registry.Resolve)
	services, err := factory.NewServices(
		t.Context(), validSettings(filepath.Join(t.TempDir(), "cache.db")), "fixture", slog.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}
	database := services.database
	if err := services.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := services.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if _, err := database.SchemaVersion(t.Context()); err == nil {
		t.Error("database remained usable after Close")
	}
}

func TestFactoryClosesDatabaseAfterInitializationError(t *testing.T) {
	registry := fixtureRegistry(t)
	factory := NewFactory(registry.Resolve)
	var opened *sqlite.Database
	factory.openDatabase = func(ctx context.Context, path string) (*sqlite.Database, error) {
		var err error
		opened, err = sqlite.Open(ctx, path)
		return opened, err
	}
	factory.newResources = func(*sqlite.Database, *http.Client, string, config.Settings) (*transport.ResourceService, error) {
		return nil, errors.New("resource setup failed")
	}

	_, err := factory.NewServices(
		t.Context(), validSettings(filepath.Join(t.TempDir(), "cache.db")), "fixture", slog.Default(),
	)
	if err == nil || !strings.Contains(err.Error(), "resource setup failed") {
		t.Fatalf("NewServices() error = %v", err)
	}
	if opened == nil {
		t.Fatal("database was not opened")
	}
	if _, err := opened.SchemaVersion(t.Context()); err == nil {
		t.Error("database remained usable after initialization error")
	}
}

func fixtureRegistry(t *testing.T) *provider.Registry {
	t.Helper()
	registry := provider.NewRegistry()
	if err := registry.Register(provider.Registration{
		Name: "fixture", SDKAPIVersion: provider.APIVersion, Implementation: &fixtureProvider{},
	}); err != nil {
		t.Fatal(err)
	}
	return registry
}

func validSettings(databasePath string) config.Settings {
	return config.Settings{
		Provider: "fixture",
		Market:   config.MarketSettings{Country: "FR", Language: "fr", Currency: "EUR"},
		Cache: config.CacheSettings{
			Path: databasePath, TTL: time.Hour,
			MaxSize: config.ByteSize(1024 * 1024), MaxResponseSize: config.ByteSize(1024),
		},
		Network: config.NetworkSettings{
			RequestsPerSecond: 10, MaxConcurrentHTTP: 1, MaxConcurrentBrowser: 1,
		},
		Browser: config.BrowserSettings{InteractiveTimeout: time.Minute},
		Log:     config.LogSettings{Level: "info"},
	}
}
