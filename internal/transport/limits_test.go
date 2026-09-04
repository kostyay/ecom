package transport

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/provider"
)

var defaultTestLimits = RequestLimits{
	RequestsPerSecond:    1,
	MaxConcurrentHTTP:    2,
	MaxConcurrentBrowser: 1,
}

type advancingScheduler struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func newAdvancingScheduler() *advancingScheduler {
	return &advancingScheduler{now: time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)}
}

func (scheduler *advancingScheduler) Now() time.Time {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return scheduler.now
}

func (scheduler *advancingScheduler) Wait(ctx context.Context, duration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	scheduler.mu.Lock()
	scheduler.waits = append(scheduler.waits, duration)
	scheduler.now = scheduler.now.Add(duration)
	scheduler.mu.Unlock()
	return ctx.Err()
}

func (scheduler *advancingScheduler) recordedWaits() []time.Duration {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return append([]time.Duration(nil), scheduler.waits...)
}

type blockingScheduler struct {
	now     time.Time
	started chan time.Duration
}

func (scheduler *blockingScheduler) Now() time.Time { return scheduler.now }

func (scheduler *blockingScheduler) Wait(ctx context.Context, duration time.Duration) error {
	scheduler.started <- duration
	<-ctx.Done()
	return ctx.Err()
}

func newTestLimitManager(t *testing.T, limits RequestLimits, scheduler WaitScheduler) *RequestLimitManager {
	t.Helper()
	manager, err := NewRequestLimitManager(limits, nil, scheduler)
	if err != nil {
		t.Fatalf("NewRequestLimitManager() error = %v", err)
	}
	return manager
}

func acquireTestPermit(t *testing.T, manager *RequestLimitManager, providerName string, mode provider.TransportMode) *RequestPermit {
	t.Helper()
	permit, err := manager.Acquire(t.Context(), providerName, mode)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	return permit
}

func TestRequestLimitManagerSpacesStartsPerProvider(t *testing.T) {
	scheduler := newAdvancingScheduler()
	manager := newTestLimitManager(t, defaultTestLimits, scheduler)

	for range 3 {
		permit := acquireTestPermit(t, manager, "bike-discount", provider.TransportHTTP)
		permit.Release()
	}

	waits := scheduler.recordedWaits()
	if len(waits) != 2 || waits[0] != time.Second || waits[1] != time.Second {
		t.Fatalf("waits = %v, want [1s 1s]", waits)
	}
}

func TestRequestLimitManagerIsolatesProviders(t *testing.T) {
	scheduler := newAdvancingScheduler()
	manager := newTestLimitManager(t, defaultTestLimits, scheduler)

	first := acquireTestPermit(t, manager, "bike-discount", provider.TransportHTTP)
	first.Release()
	second := acquireTestPermit(t, manager, "another-shop", provider.TransportHTTP)
	second.Release()

	if waits := scheduler.recordedWaits(); len(waits) != 0 {
		t.Fatalf("provider isolation waits = %v, want none", waits)
	}
}

func TestRequestLimitManagerUsesProviderOverride(t *testing.T) {
	scheduler := newAdvancingScheduler()
	override := RequestLimits{RequestsPerSecond: 4, MaxConcurrentHTTP: 1, MaxConcurrentBrowser: 1}
	manager, err := NewRequestLimitManager(defaultTestLimits, map[string]RequestLimits{"fast-shop": override}, scheduler)
	if err != nil {
		t.Fatalf("NewRequestLimitManager() error = %v", err)
	}

	for range 2 {
		permit := acquireTestPermit(t, manager, "fast-shop", provider.TransportHTTP)
		permit.Release()
	}
	if waits := scheduler.recordedWaits(); len(waits) != 1 || waits[0] != 250*time.Millisecond {
		t.Fatalf("override waits = %v, want [250ms]", waits)
	}
}

func TestRequestLimitManagerEnforcesHTTPConcurrency(t *testing.T) {
	manager := newTestLimitManager(t, defaultTestLimits, newAdvancingScheduler())
	first := acquireTestPermit(t, manager, "bike-discount", provider.TransportHTTP)
	second := acquireTestPermit(t, manager, "bike-discount", provider.TransportHTTP)
	defer second.Release()
	state := manager.providers["bike-discount"]
	if got, want := len(state.httpSlots), cap(state.httpSlots); got != want {
		t.Fatalf("used HTTP slots = %d, want capacity %d", got, want)
	}

	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		_, err := manager.Acquire(ctx, "bike-discount", provider.TransportHTTP)
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire() error = %v, want context canceled", err)
	}
	if got := len(state.httpSlots); got != 2 {
		t.Fatalf("used HTTP slots after queued cancellation = %d, want 2", got)
	}
	first.Release()
	third := acquireTestPermit(t, manager, "bike-discount", provider.TransportHTTP)
	third.Release()
}

func TestRequestLimitManagerSharesBrowserAndCDPConcurrency(t *testing.T) {
	manager := newTestLimitManager(t, defaultTestLimits, newAdvancingScheduler())
	browser := acquireTestPermit(t, manager, "bike-discount", provider.TransportBrowser)
	state := manager.providers["bike-discount"]
	if got, want := len(state.browserSlots), cap(state.browserSlots); got != want {
		t.Fatalf("used browser-family slots = %d, want capacity %d", got, want)
	}

	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		_, err := manager.Acquire(ctx, "bike-discount", provider.TransportCDP)
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("CDP Acquire() error = %v, want context canceled", err)
	}

	http := acquireTestPermit(t, manager, "bike-discount", provider.TransportHTTP)
	http.Release()
	browser.Release()
	cdp := acquireTestPermit(t, manager, "bike-discount", provider.TransportCDP)
	cdp.Release()
}

func TestRequestLimitManagerCancellationDuringRateWaitReleasesSlot(t *testing.T) {
	scheduler := &blockingScheduler{
		now:     time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC),
		started: make(chan time.Duration, 1),
	}
	limits := RequestLimits{RequestsPerSecond: 1, MaxConcurrentHTTP: 1, MaxConcurrentBrowser: 1}
	manager := newTestLimitManager(t, limits, scheduler)
	first := acquireTestPermit(t, manager, "bike-discount", provider.TransportHTTP)
	first.Release()

	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := manager.Acquire(ctx, "bike-discount", provider.TransportHTTP)
		result <- err
	}()
	if duration := <-scheduler.started; duration != time.Second {
		t.Fatalf("rate wait = %v, want 1s", duration)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued Acquire() error = %v, want context canceled", err)
	}

	manager.scheduler = newAdvancingScheduler()
	permit := acquireTestPermit(t, manager, "bike-discount", provider.TransportHTTP)
	permit.Release()
}

func TestRequestPermitReleaseIsIdempotent(t *testing.T) {
	limits := RequestLimits{RequestsPerSecond: 1, MaxConcurrentHTTP: 1, MaxConcurrentBrowser: 1}
	manager := newTestLimitManager(t, limits, newAdvancingScheduler())
	permit := acquireTestPermit(t, manager, "bike-discount", provider.TransportHTTP)
	permit.Release()
	permit.Release()
	(*RequestPermit)(nil).Release()

	next := acquireTestPermit(t, manager, "bike-discount", provider.TransportHTTP)
	next.Release()
}

func TestNewRequestLimitManagerValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		defaults  RequestLimits
		overrides map[string]RequestLimits
		scheduler WaitScheduler
	}{
		{name: "zero rate", defaults: RequestLimits{MaxConcurrentHTTP: 1, MaxConcurrentBrowser: 1}, scheduler: newAdvancingScheduler()},
		{name: "NaN rate", defaults: RequestLimits{RequestsPerSecond: math.NaN(), MaxConcurrentHTTP: 1, MaxConcurrentBrowser: 1}, scheduler: newAdvancingScheduler()},
		{name: "infinite rate", defaults: RequestLimits{RequestsPerSecond: math.Inf(1), MaxConcurrentHTTP: 1, MaxConcurrentBrowser: 1}, scheduler: newAdvancingScheduler()},
		{name: "too large rate", defaults: RequestLimits{RequestsPerSecond: float64(time.Second) * 2, MaxConcurrentHTTP: 1, MaxConcurrentBrowser: 1}, scheduler: newAdvancingScheduler()},
		{name: "zero HTTP", defaults: RequestLimits{RequestsPerSecond: 1, MaxConcurrentBrowser: 1}, scheduler: newAdvancingScheduler()},
		{name: "zero browser", defaults: RequestLimits{RequestsPerSecond: 1, MaxConcurrentHTTP: 1}, scheduler: newAdvancingScheduler()},
		{name: "nil scheduler", defaults: defaultTestLimits},
		{name: "blank override", defaults: defaultTestLimits, overrides: map[string]RequestLimits{" ": defaultTestLimits}, scheduler: newAdvancingScheduler()},
		{name: "invalid override", defaults: defaultTestLimits, overrides: map[string]RequestLimits{"shop": {}}, scheduler: newAdvancingScheduler()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewRequestLimitManager(test.defaults, test.overrides, test.scheduler); err == nil {
				t.Fatal("NewRequestLimitManager() error = nil")
			}
		})
	}
}

func TestRequestLimitsFromConfig(t *testing.T) {
	settings := config.NetworkSettings{RequestsPerSecond: 2.5, MaxConcurrentHTTP: 4, MaxConcurrentBrowser: 3, Retries: 9}
	want := RequestLimits{RequestsPerSecond: 2.5, MaxConcurrentHTTP: 4, MaxConcurrentBrowser: 3}
	if got := RequestLimitsFromConfig(settings); got != want {
		t.Fatalf("RequestLimitsFromConfig() = %#v, want %#v", got, want)
	}
}

func TestCacheHitPathDoesNotAcquireRequestLimit(t *testing.T) {
	scheduler := newAdvancingScheduler()
	manager := newTestLimitManager(t, defaultTestLimits, scheduler)

	fetch := func(cached bool) error {
		if cached {
			return nil
		}
		permit, err := manager.Acquire(t.Context(), "bike-discount", provider.TransportHTTP)
		if err != nil {
			return err
		}
		defer permit.Release()
		return nil
	}
	if err := fetch(true); err != nil {
		t.Fatalf("cached fetch error = %v", err)
	}
	if len(manager.providers) != 0 {
		t.Fatalf("cache hit created %d provider limit states, want 0", len(manager.providers))
	}
	if err := fetch(false); err != nil {
		t.Fatalf("cache miss fetch error = %v", err)
	}
	if len(manager.providers) != 1 {
		t.Fatalf("cache miss created %d provider limit states, want 1", len(manager.providers))
	}
}

func TestRequestLimitManagerRejectsUnknownTransport(t *testing.T) {
	manager := newTestLimitManager(t, defaultTestLimits, newAdvancingScheduler())
	if _, err := manager.Acquire(t.Context(), "bike-discount", provider.TransportMode("other")); err == nil {
		t.Fatal("Acquire() error = nil")
	}
}
