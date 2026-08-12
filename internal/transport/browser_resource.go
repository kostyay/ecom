package transport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/internal/session"
	"github.com/kostyay/ecom/internal/storage/sqlite"
	"github.com/kostyay/ecom/provider"
)

type browserNavigator interface {
	NavigateHeadless(context.Context, provider.ResourceRequest, session.State) (BrowserNavigation, error)
	NavigateInteractive(context.Context, provider.ResourceRequest, session.State) (BrowserNavigation, error)
}

// BrowserResourceService adds portable session storage and provider request
// limits to one isolated browser executor. It does not cache rendered pages.
type BrowserResourceService struct {
	providerName       string
	browser            browserNavigator
	sessions           session.Repository
	limits             permitAcquirer
	clock              Clock
	interactiveTimeout time.Duration
	withTimeout        func(context.Context, time.Duration) (context.Context, context.CancelFunc)
}

// NewBrowserResourceService creates a session-aware isolated browser service.
func NewBrowserResourceService(
	providerName string,
	browser browserNavigator,
	sessions session.Repository,
	limits permitAcquirer,
	clock Clock,
	interactiveTimeouts ...time.Duration,
) (*BrowserResourceService, error) {
	if strings.TrimSpace(providerName) == "" || providerName != strings.TrimSpace(providerName) {
		return nil, errors.New("browser resource provider is required")
	}
	if browser == nil {
		return nil, errors.New("browser resource executor is required")
	}
	if sessions == nil {
		return nil, errors.New("browser session repository is required")
	}
	if limits == nil {
		return nil, errors.New("browser request limits are required")
	}
	if clock == nil {
		return nil, errors.New("browser resource clock is required")
	}
	if len(interactiveTimeouts) > 1 {
		return nil, errors.New("browser resource accepts one interactive timeout")
	}
	interactiveTimeout := 5 * time.Minute
	if len(interactiveTimeouts) == 1 {
		interactiveTimeout = interactiveTimeouts[0]
	}
	if interactiveTimeout <= 0 {
		return nil, errors.New("browser interactive timeout must be positive")
	}
	if err := (session.Record{
		Provider:  providerName,
		Market:    provider.Market{Country: "X", Language: "x", Currency: "XXX"},
		UpdatedAt: time.Unix(0, 0),
	}).Validate(); err != nil {
		return nil, fmt.Errorf("browser resource provider: %w", err)
	}
	return &BrowserResourceService{
		providerName:       providerName,
		browser:            browser,
		sessions:           sessions,
		limits:             limits,
		clock:              clock,
		interactiveTimeout: interactiveTimeout,
		withTimeout:        context.WithTimeout,
	}, nil
}

// NewConfiguredBrowserResourceService wires SQLite session state, an isolated
// Chrome executor, and browser-family request limits. The caller owns database.
func NewConfiguredBrowserResourceService(
	database *sqlite.Database,
	providerName string,
	settings config.Settings,
) (*BrowserResourceService, error) {
	limits, err := NewRequestLimitManager(RequestLimitsFromConfig(settings.Network), nil, RealWaitScheduler{})
	if err != nil {
		return nil, err
	}
	return newConfiguredBrowserResourceService(database, providerName, settings, limits)
}

func newConfiguredBrowserResourceService(
	database *sqlite.Database,
	providerName string,
	settings config.Settings,
	limits *RequestLimitManager,
) (*BrowserResourceService, error) {
	sessions, err := sqlite.NewBrowserSessionRepository(database)
	if err != nil {
		return nil, fmt.Errorf("create browser session repository: %w", err)
	}
	browser, err := NewConfiguredBrowserExecutor(settings)
	if err != nil {
		return nil, err
	}
	interactiveTimeout := settings.Browser.InteractiveTimeout
	if interactiveTimeout == 0 {
		interactiveTimeout = 5 * time.Minute
	}
	return NewBrowserResourceService(providerName, browser, sessions, limits, ClockFunc(time.Now), interactiveTimeout)
}

// Fetch loads state for the exact provider and market, performs one limited
// navigation, and atomically replaces state after a useful navigation.
func (service *BrowserResourceService) Fetch(ctx context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
	if err := ctx.Err(); err != nil {
		return provider.ResourceResponse{}, err
	}
	if request.Transport.Required != "" && request.Transport.Required != provider.TransportBrowser {
		return provider.ResourceResponse{}, provider.NewError(
			provider.ErrorCodeInvalidResourceRequest,
			"the resource requires a non-browser transport",
			nil,
		)
	}
	if err := validateBrowserSessionScope(service.providerName, request.Market); err != nil {
		return provider.ResourceResponse{}, provider.NewError(
			provider.ErrorCodeInvalidResourceRequest,
			"the browser resource market is invalid",
			err,
		)
	}

	state, err := service.loadState(ctx, request.Market)
	if err != nil {
		return provider.ResourceResponse{}, err
	}
	permit, err := service.limits.Acquire(ctx, service.providerName, provider.TransportBrowser)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return provider.ResourceResponse{}, ctxErr
		}
		return provider.ResourceResponse{}, provider.NewError(
			provider.ErrorCodeBrowserFailure,
			"the browser request limit could not be acquired",
			errors.New("browser request limit failed"),
		)
	}
	defer permit.Release()
	navigation, navigateErr := service.browser.NavigateHeadless(ctx, request, state)
	if navigateErr == nil {
		if err := service.saveState(ctx, request.Market, navigation.State); err != nil {
			return provider.ResourceResponse{}, err
		}
		return navigation.Response, nil
	}
	if !errors.Is(navigateErr, provider.ErrorCodeBrowserChallengeRequired) || !request.Interactive {
		return navigation.Response, navigateErr
	}

	interactiveState := state
	if stateHasValues(navigation.State) {
		interactiveState = navigation.State
	}
	interactiveContext, cancel := service.withTimeout(ctx, service.interactiveTimeout)
	defer cancel()
	interactiveNavigation, interactiveErr := service.browser.NavigateInteractive(interactiveContext, request, interactiveState)
	if interactiveErr != nil {
		if parentErr := ctx.Err(); parentErr != nil {
			return interactiveNavigation.Response, parentErr
		}
		if errors.Is(interactiveErr, context.DeadlineExceeded) || errors.Is(interactiveErr, provider.ErrorCodeBrowserChallengeRequired) {
			return interactiveNavigation.Response, provider.NewError(
				provider.ErrorCodeBrowserChallengeTimeout,
				"the browser challenge was not completed before the interactive timeout",
				nil,
			)
		}
		return interactiveNavigation.Response, interactiveErr
	}
	if err := service.saveState(ctx, request.Market, interactiveNavigation.State); err != nil {
		return provider.ResourceResponse{}, err
	}
	return interactiveNavigation.Response, nil
}

func (service *BrowserResourceService) loadState(ctx context.Context, market provider.Market) (session.State, error) {
	record, err := service.sessions.Get(ctx, service.providerName, market)
	if errors.Is(err, session.ErrStateNotFound) {
		return session.State{}, nil
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return session.State{}, ctxErr
		}
		return session.State{}, provider.NewError(
			provider.ErrorCodeBrowserFailure,
			"the browser session state could not be loaded",
			errors.New("browser session storage read failed"),
		)
	}
	if err := record.Validate(); err != nil || record.Provider != service.providerName || record.Market != market {
		return session.State{}, provider.NewError(
			provider.ErrorCodeBrowserFailure,
			"the browser session state could not be loaded",
			errors.New("browser session storage returned an invalid record"),
		)
	}
	return record.State, nil
}

func (service *BrowserResourceService) saveState(ctx context.Context, market provider.Market, state session.State) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := service.sessions.Put(ctx, session.Record{
		Provider:  service.providerName,
		Market:    market,
		State:     state,
		UpdatedAt: service.clock.Now().UTC(),
	})
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return provider.NewError(
		provider.ErrorCodeBrowserFailure,
		"the browser session state could not be saved",
		errors.New("browser session storage write failed"),
	)
}

func validateBrowserSessionScope(providerName string, market provider.Market) error {
	return (session.Record{
		Provider: providerName, Market: market, UpdatedAt: time.Unix(0, 0),
	}).Validate()
}

func stateHasValues(state session.State) bool {
	return len(state.Cookies) > 0 || len(state.Origins) > 0
}

var _ provider.ResourceService = (*BrowserResourceService)(nil)
