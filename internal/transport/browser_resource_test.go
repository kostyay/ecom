package transport

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/internal/session"
	"github.com/kostyay/ecom/internal/storage/sqlite"
	"github.com/kostyay/ecom/provider"
)

type fakeBrowserNavigator struct {
	states                []session.State
	headed                []bool
	navigation            BrowserNavigation
	err                   error
	interactiveNavigation BrowserNavigation
	interactiveErr        error
	waitInteractive       bool
	onNavigate            func()
}

func (browser *fakeBrowserNavigator) NavigateHeadless(
	_ context.Context,
	_ provider.ResourceRequest,
	state session.State,
) (BrowserNavigation, error) {
	browser.states = append(browser.states, state)
	browser.headed = append(browser.headed, false)
	if browser.onNavigate != nil {
		browser.onNavigate()
	}
	return browser.navigation, browser.err
}

func (browser *fakeBrowserNavigator) NavigateInteractive(
	ctx context.Context,
	_ provider.ResourceRequest,
	state session.State,
) (BrowserNavigation, error) {
	browser.states = append(browser.states, state)
	browser.headed = append(browser.headed, true)
	if browser.onNavigate != nil {
		browser.onNavigate()
	}
	if browser.interactiveErr != nil {
		return browser.interactiveNavigation, browser.interactiveErr
	}
	if browser.waitInteractive {
		<-ctx.Done()
		return browser.interactiveNavigation, ctx.Err()
	}
	return browser.interactiveNavigation, ctx.Err()
}

type fakeBrowserPermits struct {
	acquires int
	releases int
	mode     provider.TransportMode
	err      error
}

func (permits *fakeBrowserPermits) Acquire(_ context.Context, _ string, mode provider.TransportMode) (*RequestPermit, error) {
	permits.acquires++
	permits.mode = mode
	if permits.err != nil {
		return nil, permits.err
	}
	return &RequestPermit{release: func() { permits.releases++ }}, nil
}

type fakeSessionRepository struct {
	record session.Record
	getErr error
	putErr error
	gets   int
	puts   int
}

func (repository *fakeSessionRepository) Get(context.Context, string, provider.Market) (session.Record, error) {
	repository.gets++
	return repository.record, repository.getErr
}

func (repository *fakeSessionRepository) Put(_ context.Context, record session.Record) (session.Record, error) {
	repository.puts++
	repository.record = record
	return record, repository.putErr
}

func (*fakeSessionRepository) Delete(context.Context, string, provider.Market) (bool, error) {
	return false, nil
}

func TestBrowserResourceServiceStoresAndReloadsSQLiteState(t *testing.T) {
	repository, database := openBrowserResourceRepository(t)
	now := time.Date(2026, time.August, 12, 18, 0, 0, 0, time.UTC)
	market := browserResourceMarket()
	firstState := browserResourceState("first")
	secondState := browserResourceState("second")
	browser := &fakeBrowserNavigator{navigation: successfulBrowserNavigation(firstState)}
	permits := &fakeBrowserPermits{}
	service := newBrowserResourceService(t, "bike-discount", browser, repository, permits, now)

	response, err := service.Fetch(context.Background(), browserResourceRequest(market))
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("first Fetch() = %#v, %v", response, err)
	}
	if len(browser.states) != 1 || !reflect.DeepEqual(browser.states[0], session.State{}) {
		t.Fatalf("first imported states = %#v", browser.states)
	}
	record, err := repository.Get(context.Background(), "bike-discount", market)
	if err != nil || !reflect.DeepEqual(record.State, firstState) || !record.UpdatedAt.Equal(now) {
		t.Fatalf("stored record = %#v, %v", record, err)
	}

	browser.navigation = successfulBrowserNavigation(secondState)
	if _, err := service.Fetch(context.Background(), browserResourceRequest(market)); err != nil {
		t.Fatalf("second Fetch() error = %v", err)
	}
	if len(browser.states) != 2 || !reflect.DeepEqual(browser.states[1], firstState) {
		t.Fatalf("second imported state = %#v", browser.states)
	}
	record, err = repository.Get(context.Background(), "bike-discount", market)
	if err != nil || !reflect.DeepEqual(record.State, secondState) {
		t.Fatalf("updated record = %#v, %v", record, err)
	}
	if permits.acquires != 2 || permits.releases != 2 || permits.mode != provider.TransportBrowser {
		t.Fatalf("permit calls = %d/%d, mode %q", permits.acquires, permits.releases, permits.mode)
	}
	_ = database
}

func TestBrowserResourceServiceIsolatesProviderAndExactMarket(t *testing.T) {
	repository, _ := openBrowserResourceRepository(t)
	now := time.Date(2026, time.August, 12, 18, 0, 0, 0, time.UTC)
	market := browserResourceMarket()
	otherMarket := market
	otherMarket.Language = "de"
	putBrowserResourceState(t, repository, "bike-discount", market, browserResourceState("bike-en"), now)
	putBrowserResourceState(t, repository, "bike-discount", otherMarket, browserResourceState("bike-de"), now)
	putBrowserResourceState(t, repository, "other-shop", market, browserResourceState("other-en"), now)

	browser := &fakeBrowserNavigator{navigation: successfulBrowserNavigation(browserResourceState("updated"))}
	service := newBrowserResourceService(t, "bike-discount", browser, repository, &fakeBrowserPermits{}, now)
	if _, err := service.Fetch(context.Background(), browserResourceRequest(market)); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if !reflect.DeepEqual(browser.states[0], browserResourceState("bike-en")) {
		t.Fatalf("imported state = %#v", browser.states[0])
	}
	assertBrowserResourceState(t, repository, "bike-discount", otherMarket, "bike-de")
	assertBrowserResourceState(t, repository, "other-shop", market, "other-en")
}

func TestBrowserResourceServiceStopsOnStateReadFailure(t *testing.T) {
	repository := &fakeSessionRepository{getErr: errors.New("secret database path")}
	browser := &fakeBrowserNavigator{}
	permits := &fakeBrowserPermits{}
	service := newBrowserResourceService(t, "bike-discount", browser, repository, permits, time.Now())

	_, err := service.Fetch(context.Background(), browserResourceRequest(browserResourceMarket()))
	if !errors.Is(err, provider.ErrorCodeBrowserFailure) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(browser.states) != 0 || permits.acquires != 0 {
		t.Fatalf("browser states/acquires = %d/%d", len(browser.states), permits.acquires)
	}
}

func TestBrowserResourceServiceReportsStateWriteFailureWithoutSuccess(t *testing.T) {
	repository := &fakeSessionRepository{getErr: session.ErrStateNotFound, putErr: errors.New("secret database path")}
	browser := &fakeBrowserNavigator{navigation: successfulBrowserNavigation(browserResourceState("new"))}
	permits := &fakeBrowserPermits{}
	service := newBrowserResourceService(t, "bike-discount", browser, repository, permits, time.Now())

	response, err := service.Fetch(context.Background(), browserResourceRequest(browserResourceMarket()))
	if !errors.Is(err, provider.ErrorCodeBrowserFailure) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("Fetch() = %#v, %v", response, err)
	}
	if response.Page != nil || repository.puts != 1 || permits.releases != 1 {
		t.Fatalf("response/puts/releases = %#v/%d/%d", response, repository.puts, permits.releases)
	}
}

func TestBrowserResourceServiceReportsSQLiteReadAndWriteFailuresSafely(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		repository, database := openBrowserResourceRepository(t)
		if err := database.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		browser := &fakeBrowserNavigator{}
		service := newBrowserResourceService(t, "bike-discount", browser, repository, &fakeBrowserPermits{}, time.Now())

		_, err := service.Fetch(context.Background(), browserResourceRequest(browserResourceMarket()))
		if !errors.Is(err, provider.ErrorCodeBrowserFailure) || strings.Contains(err.Error(), database.Path()) {
			t.Fatalf("Fetch() error = %v", err)
		}
		if len(browser.states) != 0 {
			t.Fatalf("browser calls = %d", len(browser.states))
		}
	})

	t.Run("write", func(t *testing.T) {
		repository, database := openBrowserResourceRepository(t)
		browser := &fakeBrowserNavigator{navigation: successfulBrowserNavigation(browserResourceState("new"))}
		browser.onNavigate = func() { _ = database.Close() }
		service := newBrowserResourceService(t, "bike-discount", browser, repository, &fakeBrowserPermits{}, time.Now())

		response, err := service.Fetch(context.Background(), browserResourceRequest(browserResourceMarket()))
		if !errors.Is(err, provider.ErrorCodeBrowserFailure) || strings.Contains(err.Error(), database.Path()) {
			t.Fatalf("Fetch() = %#v, %v", response, err)
		}
		if response.Page != nil {
			t.Fatalf("Fetch() returned false success: %#v", response)
		}
	})
}

func TestBrowserResourceServicePreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repository := &fakeSessionRepository{}
	browser := &fakeBrowserNavigator{}
	permits := &fakeBrowserPermits{}
	service := newBrowserResourceService(t, "bike-discount", browser, repository, permits, time.Now())

	_, err := service.Fetch(ctx, browserResourceRequest(browserResourceMarket()))
	if !errors.Is(err, context.Canceled) || repository.gets != 0 || permits.acquires != 0 || len(browser.states) != 0 {
		t.Fatalf("Fetch() error/calls = %v/%d/%d/%d", err, repository.gets, permits.acquires, len(browser.states))
	}
}

func TestBrowserResourceServiceDoesNotSaveAfterNavigationCancelsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repository := &fakeSessionRepository{getErr: session.ErrStateNotFound}
	browser := &fakeBrowserNavigator{navigation: successfulBrowserNavigation(browserResourceState("new")), onNavigate: cancel}
	service := newBrowserResourceService(t, "bike-discount", browser, repository, &fakeBrowserPermits{}, time.Now())

	response, err := service.Fetch(ctx, browserResourceRequest(browserResourceMarket()))
	if !errors.Is(err, context.Canceled) || response.Page != nil || repository.puts != 0 {
		t.Fatalf("Fetch() = %#v, %v; puts = %d", response, err, repository.puts)
	}
}

func TestBrowserResourceServiceReleasesPermitAfterNavigationFailure(t *testing.T) {
	repository := &fakeSessionRepository{getErr: session.ErrStateNotFound}
	browser := &fakeBrowserNavigator{err: provider.NewError(provider.ErrorCodeBrowserFailure, "failed", nil)}
	permits := &fakeBrowserPermits{}
	service := newBrowserResourceService(t, "bike-discount", browser, repository, permits, time.Now())

	_, _ = service.Fetch(context.Background(), browserResourceRequest(browserResourceMarket()))
	if permits.acquires != 1 || permits.releases != 1 || repository.puts != 0 {
		t.Fatalf("acquires/releases/puts = %d/%d/%d", permits.acquires, permits.releases, repository.puts)
	}
}

func TestBrowserResourceServiceChallengeAndBlockStatePolicy(t *testing.T) {
	tests := []struct {
		name      string
		code      provider.ErrorCode
		state     session.State
		wantPuts  int
		wantState session.State
	}{
		{name: "challenge state is not persisted", code: provider.ErrorCodeBrowserChallengeRequired, state: browserResourceState("challenge")},
		{name: "challenge does not erase state with empty export", code: provider.ErrorCodeBrowserChallengeRequired, state: session.State{}},
		{name: "block does not save state", code: provider.ErrorCodeAccessBlocked, state: browserResourceState("blocked")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeSessionRepository{getErr: session.ErrStateNotFound}
			browser := &fakeBrowserNavigator{
				navigation: successfulBrowserNavigation(test.state),
				err:        provider.NewError(test.code, "classified", nil),
			}
			service := newBrowserResourceService(t, "bike-discount", browser, repository, &fakeBrowserPermits{}, time.Now())

			response, err := service.Fetch(context.Background(), browserResourceRequest(browserResourceMarket()))
			if !errors.Is(err, test.code) || response.Page == nil {
				t.Fatalf("Fetch() = %#v, %v", response, err)
			}
			if repository.puts != test.wantPuts || !reflect.DeepEqual(repository.record.State, test.wantState) {
				t.Fatalf("stored = %d, %#v", repository.puts, repository.record.State)
			}
		})
	}
}

func TestBrowserResourceServiceInteractiveChallengeSuccess(t *testing.T) {
	initial := browserResourceState("initial")
	challenge := browserResourceState("challenge")
	clearance := browserResourceState("clearance")
	repository := &fakeSessionRepository{record: session.Record{
		Provider: "bike-discount", Market: browserResourceMarket(), State: initial, UpdatedAt: time.Now(),
	}}
	browser := &fakeBrowserNavigator{
		navigation:            successfulBrowserNavigation(challenge),
		err:                   provider.NewError(provider.ErrorCodeBrowserChallengeRequired, "challenge", nil),
		interactiveNavigation: successfulBrowserNavigation(clearance),
	}
	permits := &fakeBrowserPermits{}
	service := newBrowserResourceService(t, "bike-discount", browser, repository, permits, time.Now())
	request := browserResourceRequest(browserResourceMarket())
	request.Interactive = true

	response, err := service.Fetch(context.Background(), request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("Fetch() = %#v, %v", response, err)
	}
	if !reflect.DeepEqual(browser.states, []session.State{initial, challenge}) || !reflect.DeepEqual(browser.headed, []bool{false, true}) {
		t.Fatalf("browser states/headed = %#v/%#v", browser.states, browser.headed)
	}
	if repository.puts != 1 || !reflect.DeepEqual(repository.record.State, clearance) {
		t.Fatalf("saved state = %d/%#v", repository.puts, repository.record.State)
	}
	if permits.acquires != 1 || permits.releases != 1 {
		t.Fatalf("permit calls = %d/%d", permits.acquires, permits.releases)
	}
}

func TestBrowserResourceServiceNonInteractiveChallengeReturnsImmediately(t *testing.T) {
	repository := &fakeSessionRepository{getErr: session.ErrStateNotFound}
	browser := &fakeBrowserNavigator{
		navigation: successfulBrowserNavigation(browserResourceState("challenge")),
		err:        provider.NewError(provider.ErrorCodeBrowserChallengeRequired, "challenge", nil),
	}
	permits := &fakeBrowserPermits{}
	service := newBrowserResourceService(t, "bike-discount", browser, repository, permits, time.Now())

	_, err := service.Fetch(context.Background(), browserResourceRequest(browserResourceMarket()))
	if !errors.Is(err, provider.ErrorCodeBrowserChallengeRequired) || len(browser.states) != 1 || browser.headed[0] || repository.puts != 0 {
		t.Fatalf("Fetch() error/calls/headed/puts = %v/%d/%#v/%d", err, len(browser.states), browser.headed, repository.puts)
	}
}

func TestBrowserResourceServiceInteractiveChallengeTimeout(t *testing.T) {
	repository := &fakeSessionRepository{getErr: session.ErrStateNotFound}
	browser := &fakeBrowserNavigator{
		navigation:      successfulBrowserNavigation(browserResourceState("challenge")),
		err:             provider.NewError(provider.ErrorCodeBrowserChallengeRequired, "challenge", nil),
		waitInteractive: true,
	}
	permits := &fakeBrowserPermits{}
	service := newBrowserResourceService(t, "bike-discount", browser, repository, permits, time.Now())
	service.withTimeout = func(parent context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
		return context.WithDeadline(parent, time.Now().Add(-time.Second))
	}
	request := browserResourceRequest(browserResourceMarket())
	request.Interactive = true

	_, err := service.Fetch(context.Background(), request)
	if !errors.Is(err, provider.ErrorCodeBrowserChallengeTimeout) || repository.puts != 0 || permits.acquires != 1 || permits.releases != 1 {
		t.Fatalf("Fetch() error/puts/permits = %v/%d/%d/%d", err, repository.puts, permits.acquires, permits.releases)
	}
}

func TestBrowserResourceServiceInteractiveBlockIsNotSaved(t *testing.T) {
	repository := &fakeSessionRepository{getErr: session.ErrStateNotFound}
	browser := &fakeBrowserNavigator{
		navigation:            successfulBrowserNavigation(browserResourceState("challenge")),
		err:                   provider.NewError(provider.ErrorCodeBrowserChallengeRequired, "challenge", nil),
		interactiveNavigation: successfulBrowserNavigation(browserResourceState("blocked")),
		interactiveErr:        provider.NewError(provider.ErrorCodeAccessBlocked, "blocked", nil),
	}
	service := newBrowserResourceService(t, "bike-discount", browser, repository, &fakeBrowserPermits{}, time.Now())
	request := browserResourceRequest(browserResourceMarket())
	request.Interactive = true

	_, err := service.Fetch(context.Background(), request)
	if !errors.Is(err, provider.ErrorCodeAccessBlocked) || repository.puts != 0 {
		t.Fatalf("Fetch() error/puts = %v/%d", err, repository.puts)
	}
}

func TestBrowserResourceServiceInteractiveCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repository := &fakeSessionRepository{getErr: session.ErrStateNotFound}
	browser := &fakeBrowserNavigator{
		navigation:      successfulBrowserNavigation(browserResourceState("challenge")),
		err:             provider.NewError(provider.ErrorCodeBrowserChallengeRequired, "challenge", nil),
		waitInteractive: true,
	}
	calls := 0
	browser.onNavigate = func() {
		calls++
		if calls == 2 {
			cancel()
		}
	}
	service := newBrowserResourceService(t, "bike-discount", browser, repository, &fakeBrowserPermits{}, time.Now())
	request := browserResourceRequest(browserResourceMarket())
	request.Interactive = true

	_, err := service.Fetch(ctx, request)
	if !errors.Is(err, context.Canceled) || repository.puts != 0 {
		t.Fatalf("Fetch() error/puts = %v/%d", err, repository.puts)
	}
}

func TestBrowserResourceServiceInteractiveWriteFailureDoesNotReturnFalseSuccess(t *testing.T) {
	repository := &fakeSessionRepository{getErr: session.ErrStateNotFound, putErr: errors.New("secret write detail")}
	browser := &fakeBrowserNavigator{
		navigation:            successfulBrowserNavigation(browserResourceState("challenge")),
		err:                   provider.NewError(provider.ErrorCodeBrowserChallengeRequired, "challenge", nil),
		interactiveNavigation: successfulBrowserNavigation(browserResourceState("clearance")),
	}
	service := newBrowserResourceService(t, "bike-discount", browser, repository, &fakeBrowserPermits{}, time.Now())

	request := browserResourceRequest(browserResourceMarket())
	request.Interactive = true
	response, err := service.Fetch(context.Background(), request)
	if !errors.Is(err, provider.ErrorCodeBrowserFailure) || errors.Is(err, provider.ErrorCodeBrowserChallengeRequired) || strings.Contains(err.Error(), "secret") || response.Page != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func TestBrowserResourceServiceRejectsWrongTransportAndInvalidMarketBeforeStorage(t *testing.T) {
	repository := &fakeSessionRepository{}
	service := newBrowserResourceService(t, "bike-discount", &fakeBrowserNavigator{}, repository, &fakeBrowserPermits{}, time.Now())
	tests := []provider.ResourceRequest{
		{URL: "https://shop.example", Market: browserResourceMarket(), Transport: provider.TransportPolicy{Required: provider.TransportHTTP}},
		{URL: "https://shop.example", Market: provider.Market{Country: "DE", Language: "en", Currency: "eur"}},
	}
	for _, request := range tests {
		_, err := service.Fetch(context.Background(), request)
		if !errors.Is(err, provider.ErrorCodeInvalidResourceRequest) {
			t.Fatalf("Fetch(%#v) error = %v", request, err)
		}
	}
	if repository.gets != 0 {
		t.Fatalf("repository gets = %d", repository.gets)
	}
}

func TestConfiguredBrowserResourceServiceUsesSQLiteWithoutResponseCache(t *testing.T) {
	database, err := sqlite.Open(context.Background(), t.TempDir()+"/cache.db")
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	settings := config.Settings{
		Cache: config.CacheSettings{MaxResponseSize: 1024},
		Network: config.NetworkSettings{
			RequestsPerSecond: 100, MaxConcurrentHTTP: 2, MaxConcurrentBrowser: 1,
		},
	}
	service, err := NewConfiguredBrowserResourceService(database, "bike-discount", settings)
	if err != nil || service == nil {
		t.Fatalf("NewConfiguredBrowserResourceService() = %#v, %v", service, err)
	}
}

func openBrowserResourceRepository(t *testing.T) (*sqlite.BrowserSessionRepository, *sqlite.Database) {
	t.Helper()
	database, err := sqlite.Open(context.Background(), t.TempDir()+"/cache.db")
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, err := sqlite.NewBrowserSessionRepository(database)
	if err != nil {
		t.Fatalf("NewBrowserSessionRepository() error = %v", err)
	}
	return repository, database
}

func newBrowserResourceService(
	t *testing.T,
	providerName string,
	browser browserNavigator,
	repository session.Repository,
	permits permitAcquirer,
	now time.Time,
) *BrowserResourceService {
	t.Helper()
	service, err := NewBrowserResourceService(providerName, browser, repository, permits, ClockFunc(func() time.Time { return now }), time.Minute)
	if err != nil {
		t.Fatalf("NewBrowserResourceService() error = %v", err)
	}
	return service
}

func browserResourceMarket() provider.Market {
	return provider.Market{Country: "DE", Language: "en", Currency: "EUR"}
}

func browserResourceRequest(market provider.Market) provider.ResourceRequest {
	return provider.ResourceRequest{URL: "https://shop.example/search", Market: market}
}

func browserResourceState(value string) session.State {
	return session.State{Cookies: []session.Cookie{{Name: "session", Value: value, Domain: "shop.example", Path: "/"}}}
}

func successfulBrowserNavigation(state session.State) BrowserNavigation {
	return BrowserNavigation{
		Response: provider.ResourceResponse{
			Page: &provider.PageSnapshot{HTML: []byte("<html></html>")}, StatusCode: http.StatusOK,
			FinalURL: "https://shop.example/search", Transport: provider.TransportBrowser,
		},
		State: state,
	}
}

func putBrowserResourceState(
	t *testing.T,
	repository *sqlite.BrowserSessionRepository,
	providerName string,
	market provider.Market,
	state session.State,
	now time.Time,
) {
	t.Helper()
	if _, err := repository.Put(context.Background(), session.Record{Provider: providerName, Market: market, State: state, UpdatedAt: now}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
}

func assertBrowserResourceState(t *testing.T, repository *sqlite.BrowserSessionRepository, providerName string, market provider.Market, value string) {
	t.Helper()
	record, err := repository.Get(context.Background(), providerName, market)
	if err != nil || !reflect.DeepEqual(record.State, browserResourceState(value)) {
		t.Fatalf("Get(%q, %#v) = %#v, %v", providerName, market, record, err)
	}
}

var _ browserNavigator = (*fakeBrowserNavigator)(nil)
