// Package app composes the provider-neutral application services used by CLI
// command handlers.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kostyay/ecom/internal/cache"
	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/internal/maintenance"
	"github.com/kostyay/ecom/internal/storage/sqlite"
	"github.com/kostyay/ecom/internal/transport"
	"github.com/kostyay/ecom/provider"
)

const cacheDatabaseName = "cache.db"

// ProviderResolver resolves a compiled provider by its stable name.
type ProviderResolver func(string) (provider.Provider, error)

// ProviderContext contains the values needed for an operation that does not
// use Core resource services, such as provider help.
type ProviderContext struct {
	Provider provider.Provider
	Market   provider.Market
	Logger   *slog.Logger
	Settings config.Settings
}

// ProviderHelpResult contains validated provider help and its output context.
type ProviderHelpResult struct {
	ProviderName string
	Market       provider.Market
	Data         provider.HelpResult
}

// Request returns the common provider request values.
func (providerContext *ProviderContext) Request() provider.Request {
	return provider.Request{Market: providerContext.Market, Pricing: PricingFromConfig(providerContext.Settings.Pricing)}
}

// Services contains the selected provider and the shared Core services for one
// command. Provider-free commands do not create this value.
type Services struct {
	Provider     provider.Provider
	Resources    provider.ResourceService
	Maintenance  *maintenance.Service
	Market       provider.Market
	Logger       *slog.Logger
	Settings     config.Settings
	DatabasePath string

	database  *sqlite.Database
	closeOnce sync.Once
	closeErr  error
}

// MaintenanceServices contains the provider-free storage services used by
// cache and browser-session maintenance commands.
type MaintenanceServices struct {
	Maintenance  *maintenance.Service
	Market       provider.Market
	DatabasePath string

	database  *sqlite.Database
	closeOnce sync.Once
	closeErr  error
}

// Close releases maintenance storage resources. It is safe to call more than once.
func (services *MaintenanceServices) Close() error {
	if services == nil {
		return nil
	}
	services.closeOnce.Do(func() {
		if services.database != nil {
			services.closeErr = services.database.Close()
		}
	})
	return services.closeErr
}

// Request returns the common provider request values for a command handler.
func (services *Services) Request() provider.Request {
	return provider.Request{Market: services.Market, Pricing: PricingFromConfig(services.Settings.Pricing), Resources: services.Resources}
}

// Close releases shared command resources. It is safe to call more than once.
func (services *Services) Close() error {
	if services == nil {
		return nil
	}
	services.closeOnce.Do(func() {
		if services.database != nil {
			services.closeErr = services.database.Close()
		}
	})
	return services.closeErr
}

// Factory lazily creates one command service set. Tests can supply a local
// provider resolver without changing the package registry.
type Factory struct {
	resolveProvider ProviderResolver
	userCacheDir    func() (string, error)
	openDatabase    func(context.Context, string) (*sqlite.Database, error)
	newResources    func(*sqlite.Database, *http.Client, string, config.Settings) (*transport.ResourceService, error)
	httpClient      *http.Client
}

// NewFactory returns the production application service factory.
func NewFactory(resolveProvider ProviderResolver) *Factory {
	return &Factory{
		resolveProvider: resolveProvider,
		userCacheDir:    os.UserCacheDir,
		openDatabase:    sqlite.Open,
		newResources:    transport.NewConfiguredResourceService,
		httpClient:      http.DefaultClient,
	}
}

// NewProviderContext validates a provider selection and the common values for
// a provider operation. It does not open the database or create transports.
func (factory *Factory) NewProviderContext(
	settings config.Settings,
	providerName string,
	logger *slog.Logger,
) (*ProviderContext, error) {
	if factory == nil || factory.resolveProvider == nil {
		return nil, errors.New("application provider resolver is required")
	}
	selected, err := factory.resolveProvider(providerName)
	if err != nil {
		return nil, err
	}
	if err := selected.ValidateConfig(settings.Providers[selected.Name()]); err != nil {
		return nil, provider.NewError(
			provider.ErrorCodeInvalidProviderConfig,
			fmt.Sprintf("provider %q configuration is invalid", selected.Name()),
			err,
		)
	}
	market := MarketFromConfig(settings.Market)
	if err := market.Validate(); err != nil {
		return nil, fmt.Errorf("configure provider market: %w", err)
	}
	if logger == nil {
		return nil, errors.New("application logger is required")
	}

	return &ProviderContext{Provider: selected, Market: market, Logger: logger, Settings: settings}, nil
}

// ProviderHelp gets and validates compiled provider help without creating
// database, cache, HTTP, or browser services.
func (factory *Factory) ProviderHelp(
	ctx context.Context,
	settings config.Settings,
	providerName string,
	logger *slog.Logger,
) (ProviderHelpResult, error) {
	providerContext, err := factory.NewProviderContext(settings, providerName, logger)
	if err != nil {
		return ProviderHelpResult{}, err
	}
	result, err := providerContext.Provider.Help(ctx, provider.HelpRequest{Request: providerContext.Request()})
	if err != nil {
		return ProviderHelpResult{}, err
	}
	if err := result.Help.Validate(); err != nil {
		return ProviderHelpResult{}, provider.NewError(
			provider.ErrorCodeInvalidProviderResult,
			"provider returned invalid help data",
			err,
		)
	}
	if result.Help.Name != providerContext.Provider.Name() {
		return ProviderHelpResult{}, provider.NewError(
			provider.ErrorCodeInvalidProviderResult,
			"provider help name does not match the selected provider",
			nil,
		)
	}
	return ProviderHelpResult{
		ProviderName: providerContext.Provider.Name(),
		Market:       providerContext.Market,
		Data:         result,
	}, nil
}

// NewServices validates the provider selection and creates all shared Core
// services. It closes an opened database if later initialization fails.
func (factory *Factory) NewServices(
	ctx context.Context,
	settings config.Settings,
	providerName string,
	logger *slog.Logger,
) (_ *Services, err error) {
	providerContext, err := factory.NewProviderContext(settings, providerName, logger)
	if err != nil {
		return nil, err
	}

	databasePath, err := factory.DatabasePath(settings.Cache.Path)
	if err != nil {
		return nil, err
	}
	database, err := factory.openDatabase(ctx, databasePath)
	if err != nil {
		return nil, fmt.Errorf("open application database: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, database.Close())
		}
	}()

	maintenanceService, err := newMaintenanceService(database, settings)
	if err != nil {
		return nil, err
	}
	resources, err := factory.newResources(database, factory.httpClient, providerName, settings)
	if err != nil {
		return nil, fmt.Errorf("configure resource service: %w", err)
	}

	return &Services{
		Provider: providerContext.Provider, Resources: resources, Maintenance: maintenanceService,
		Market: providerContext.Market, Logger: logger, Settings: settings, DatabasePath: database.Path(), database: database,
	}, nil
}

// NewMaintenanceServices creates storage maintenance without resolving a
// provider or creating network and browser transports.
func (factory *Factory) NewMaintenanceServices(
	ctx context.Context,
	settings config.Settings,
) (_ *MaintenanceServices, err error) {
	if factory == nil || factory.openDatabase == nil {
		return nil, errors.New("application database opener is required")
	}
	market := MarketFromConfig(settings.Market)
	if err := market.Validate(); err != nil {
		return nil, fmt.Errorf("configure maintenance market: %w", err)
	}
	databasePath, err := factory.DatabasePath(settings.Cache.Path)
	if err != nil {
		return nil, err
	}
	database, err := factory.openDatabase(ctx, databasePath)
	if err != nil {
		return nil, fmt.Errorf("open application database: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, database.Close())
		}
	}()

	service, err := newMaintenanceService(database, settings)
	if err != nil {
		return nil, err
	}
	return &MaintenanceServices{
		Maintenance: service, Market: market, DatabasePath: database.Path(), database: database,
	}, nil
}

func newMaintenanceService(database *sqlite.Database, settings config.Settings) (*maintenance.Service, error) {
	rawResponses, err := sqlite.NewRawResponseRepository(database, settings.Cache.MaxResponseSize)
	if err != nil {
		return nil, fmt.Errorf("configure response repository: %w", err)
	}
	cacheService, err := cache.NewService(rawResponses, cache.ClockFunc(time.Now), cache.Limits{
		MaxSize: settings.Cache.MaxSize, MaxResponseSize: settings.Cache.MaxResponseSize,
	})
	if err != nil {
		return nil, fmt.Errorf("configure response cache: %w", err)
	}
	sessions, err := sqlite.NewBrowserSessionRepository(database)
	if err != nil {
		return nil, fmt.Errorf("configure browser sessions: %w", err)
	}
	service, err := maintenance.NewService(cacheService, sessions)
	if err != nil {
		return nil, fmt.Errorf("configure maintenance service: %w", err)
	}
	return service, nil
}

// DatabasePath resolves an explicit cache path or the operating system default.
func (factory *Factory) DatabasePath(configuredPath string) (string, error) {
	if configuredPath != "" {
		return configuredPath, nil
	}
	if factory == nil || factory.userCacheDir == nil {
		return "", errors.New("user cache directory resolver is required")
	}
	cacheDir, err := factory.userCacheDir()
	if err != nil {
		return "", fmt.Errorf("find user cache directory: %w", err)
	}
	return filepath.Join(cacheDir, "ecom", cacheDatabaseName), nil
}

// MarketFromConfig converts validated configuration to the public SDK type.
func MarketFromConfig(settings config.MarketSettings) provider.Market {
	return provider.Market{Country: settings.Country, Language: settings.Language, Currency: settings.Currency}
}

// PricingFromConfig converts the global pricing selection to the public SDK type.
func PricingFromConfig(settings config.PricingSettings) provider.PricingPolicy {
	return provider.PricingPolicy{IncludeShipping: settings.IncludeShipping}
}
