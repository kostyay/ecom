package transport

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/provider"
)

// RequestLimits control request starts and in-flight work for one provider.
// Browser and CDP requests share MaxConcurrentBrowser because both use browser
// resources. HTTP requests use a separate pool.
type RequestLimits struct {
	RequestsPerSecond    float64
	MaxConcurrentHTTP    int
	MaxConcurrentBrowser int
}

// RequestLimitsFromConfig selects the request-limit fields from network
// configuration. Retry settings are applied by a different transport policy.
func RequestLimitsFromConfig(settings config.NetworkSettings) RequestLimits {
	return RequestLimits{
		RequestsPerSecond:    settings.RequestsPerSecond,
		MaxConcurrentHTTP:    settings.MaxConcurrentHTTP,
		MaxConcurrentBrowser: settings.MaxConcurrentBrowser,
	}
}

// WaitScheduler supplies time and context-aware waits. Tests can inject a
// controlled scheduler and do not have to wait for wall-clock time.
type WaitScheduler interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}

// RealWaitScheduler uses the system clock. It creates no background worker.
type RealWaitScheduler struct{}

// Now returns the current time.
func (RealWaitScheduler) Now() time.Time { return time.Now() }

// Wait blocks for the duration or until the context ends.
func (RealWaitScheduler) Wait(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// RequestLimitManager owns independent request limits for each provider.
// Acquire is intentionally separate from cache access. A cache caller must
// call Acquire only from its cache-miss fetch function.
type RequestLimitManager struct {
	mu        sync.Mutex
	defaults  RequestLimits
	overrides map[string]RequestLimits
	scheduler WaitScheduler
	providers map[string]*providerLimitState
}

type providerLimitState struct {
	limits       RequestLimits
	httpSlots    chan struct{}
	browserSlots chan struct{}
	rateTurn     chan struct{}
	nextStart    time.Time
}

// RequestPermit holds one concurrency slot. Release is safe to call more than
// once. A successful Acquire must always be paired with Release.
type RequestPermit struct {
	once    sync.Once
	release func()
}

// Release returns the concurrency slot held by the permit.
func (permit *RequestPermit) Release() {
	if permit == nil {
		return
	}
	permit.once.Do(permit.release)
}

// NewRequestLimitManager creates a provider-scoped request limit manager.
// Override keys must be exact provider names.
func NewRequestLimitManager(defaults RequestLimits, overrides map[string]RequestLimits, scheduler WaitScheduler) (*RequestLimitManager, error) {
	if err := validateRequestLimits(defaults); err != nil {
		return nil, fmt.Errorf("validate default request limits: %w", err)
	}
	if scheduler == nil {
		return nil, errors.New("request limit scheduler is required")
	}
	validatedOverrides := make(map[string]RequestLimits, len(overrides))
	for name, limits := range overrides {
		if strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) {
			return nil, errors.New("request limit override provider is required")
		}
		if err := validateRequestLimits(limits); err != nil {
			return nil, fmt.Errorf("validate request limits for provider %q: %w", name, err)
		}
		validatedOverrides[name] = limits
	}
	return &RequestLimitManager{
		defaults:  defaults,
		overrides: validatedOverrides,
		scheduler: scheduler,
		providers: make(map[string]*providerLimitState),
	}, nil
}

// Acquire waits for a transport concurrency slot and the provider request
// start interval. Cancellation returns all partially acquired resources.
func (manager *RequestLimitManager) Acquire(ctx context.Context, providerName string, mode provider.TransportMode) (*RequestPermit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(providerName) == "" || providerName != strings.TrimSpace(providerName) {
		return nil, errors.New("request limit provider is required")
	}
	state, slots, err := manager.stateAndSlots(providerName, mode)
	if err != nil {
		return nil, err
	}
	if err := take(ctx, slots); err != nil {
		return nil, err
	}
	releaseSlot := func() { <-slots }
	if err := takeTurn(ctx, state.rateTurn); err != nil {
		releaseSlot()
		return nil, err
	}
	releaseRateTurn := func() { state.rateTurn <- struct{}{} }

	delay := state.nextStart.Sub(manager.scheduler.Now())
	if delay > 0 {
		if err := manager.scheduler.Wait(ctx, delay); err != nil {
			releaseRateTurn()
			releaseSlot()
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		releaseRateTurn()
		releaseSlot()
		return nil, err
	}
	state.nextStart = manager.scheduler.Now().Add(requestInterval(state.limits.RequestsPerSecond))
	releaseRateTurn()
	return &RequestPermit{release: releaseSlot}, nil
}

func (manager *RequestLimitManager) stateAndSlots(providerName string, mode provider.TransportMode) (*providerLimitState, chan struct{}, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state := manager.providers[providerName]
	if state == nil {
		limits := manager.defaults
		if override, found := manager.overrides[providerName]; found {
			limits = override
		}
		state = &providerLimitState{
			limits:       limits,
			httpSlots:    make(chan struct{}, limits.MaxConcurrentHTTP),
			browserSlots: make(chan struct{}, limits.MaxConcurrentBrowser),
			rateTurn:     make(chan struct{}, 1),
		}
		state.rateTurn <- struct{}{}
		manager.providers[providerName] = state
	}
	switch mode {
	case provider.TransportHTTP:
		return state, state.httpSlots, nil
	case provider.TransportBrowser, provider.TransportCDP:
		return state, state.browserSlots, nil
	default:
		return nil, nil, fmt.Errorf("unsupported request limit transport mode %q", mode)
	}
}

func take(ctx context.Context, semaphore chan struct{}) error {
	select {
	case semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func takeTurn(ctx context.Context, turn chan struct{}) error {
	select {
	case <-turn:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func validateRequestLimits(limits RequestLimits) error {
	if limits.RequestsPerSecond <= 0 || math.IsNaN(limits.RequestsPerSecond) || math.IsInf(limits.RequestsPerSecond, 0) {
		return errors.New("requests per second must be finite and positive")
	}
	if limits.MaxConcurrentHTTP <= 0 || limits.MaxConcurrentBrowser <= 0 {
		return errors.New("request concurrency limits must be positive")
	}
	if requestInterval(limits.RequestsPerSecond) <= 0 {
		return errors.New("requests per second is too large")
	}
	return nil
}

func requestInterval(requestsPerSecond float64) time.Duration {
	return time.Duration(float64(time.Second) / requestsPerSecond)
}
