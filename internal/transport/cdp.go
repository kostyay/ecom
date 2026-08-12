package transport

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/internal/session"
	"github.com/kostyay/ecom/provider"
)

// CDPConnector attaches to one existing browser without owning its process.
// Close disconnects ecom from the browser. It must not stop the browser.
type CDPConnector interface {
	Connect(context.Context, string) (CDPConnection, error)
}

// CDPConnection creates targets owned by ecom in an attached browser.
type CDPConnection interface {
	NewTarget(context.Context) (CDPTarget, error)
	Close() error
}

// CDPTarget performs closed browser operations on one target owned by ecom.
// Close closes only that target.
type CDPTarget interface {
	Navigate(context.Context, BrowserCommand) (BrowserResult, error)
	Close() error
}

type cdpBackend struct {
	address   string
	connector CDPConnector
}

func (backend *cdpBackend) Navigate(ctx context.Context, command BrowserCommand) (result BrowserResult, err error) {
	connection, err := backend.connector.Connect(ctx, backend.address)
	if err != nil {
		return BrowserResult{}, errors.New("connect to remote browser")
	}
	defer func() {
		if closeErr := connection.Close(); err == nil && closeErr != nil {
			err = errors.New("disconnect from remote browser")
		}
	}()

	target, err := connection.NewTarget(ctx)
	if err != nil {
		return BrowserResult{}, errors.New("create remote browser target")
	}
	defer func() {
		if closeErr := target.Close(); err == nil && closeErr != nil {
			err = errors.New("close remote browser target")
		}
	}()
	return target.Navigate(ctx, command)
}

// CDPResourceService uses a configured Chrome session without reading or
// changing portable session state. Chrome owns all profile state.
type CDPResourceService struct {
	providerName string
	address      string
	executor     *BrowserExecutor
	limits       permitAcquirer
}

// NewCDPResourceService creates a limited CDP resource service. An empty
// address is accepted so Fetch can return a stable fallback signal.
func NewCDPResourceService(
	providerName string,
	address string,
	connector CDPConnector,
	limits permitAcquirer,
	clock Clock,
	maxResponseSize config.ByteSize,
) (*CDPResourceService, error) {
	if strings.TrimSpace(providerName) == "" || providerName != strings.TrimSpace(providerName) {
		return nil, errors.New("CDP resource provider is required")
	}
	if connector == nil {
		return nil, errors.New("CDP connector is required")
	}
	if limits == nil {
		return nil, errors.New("CDP request limits are required")
	}
	executor, err := NewBrowserExecutor(&cdpBackend{address: address, connector: connector}, clock, maxResponseSize, false)
	if err != nil {
		return nil, err
	}
	return &CDPResourceService{
		providerName: providerName,
		address:      address,
		executor:     executor,
		limits:       limits,
	}, nil
}

// NewConfiguredCDPResourceService wires the configured CDP address and shared
// browser-family request limits. It does not use SQLite session storage.
func NewConfiguredCDPResourceService(providerName string, settings config.Settings) (*CDPResourceService, error) {
	limits, err := NewRequestLimitManager(RequestLimitsFromConfig(settings.Network), nil, RealWaitScheduler{})
	if err != nil {
		return nil, err
	}
	return newConfiguredCDPResourceService(providerName, settings, limits)
}

func newConfiguredCDPResourceService(providerName string, settings config.Settings, limits *RequestLimitManager) (*CDPResourceService, error) {
	return NewCDPResourceService(
		providerName,
		settings.Browser.CDPAddress,
		NewChromedpRemoteConnector(),
		limits,
		ClockFunc(time.Now),
		settings.Cache.MaxResponseSize,
	)
}

// Fetch opens a new target in the configured browser and closes only that
// target after it captures the page snapshot.
func (service *CDPResourceService) Fetch(ctx context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
	if err := ctx.Err(); err != nil {
		return provider.ResourceResponse{}, err
	}
	if strings.TrimSpace(service.address) == "" {
		return provider.ResourceResponse{}, provider.NewError(
			provider.ErrorCodeTransportUnavailable,
			"the Chrome CDP transport is not configured",
			nil,
		)
	}
	if request.Transport.Required != "" && request.Transport.Required != provider.TransportCDP {
		return provider.ResourceResponse{}, provider.NewError(
			provider.ErrorCodeInvalidResourceRequest,
			"the resource requires a non-CDP transport",
			nil,
		)
	}

	permit, err := service.limits.Acquire(ctx, service.providerName, provider.TransportCDP)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return provider.ResourceResponse{}, ctxErr
		}
		return provider.ResourceResponse{}, provider.NewError(
			provider.ErrorCodeBrowserFailure,
			"the CDP request limit could not be acquired",
			errors.New("CDP request limit failed"),
		)
	}
	defer permit.Release()

	navigation, err := service.executor.Navigate(ctx, request, session.State{})
	navigation.Response.Transport = provider.TransportCDP
	return navigation.Response, err
}

var _ provider.ResourceService = (*CDPResourceService)(nil)
