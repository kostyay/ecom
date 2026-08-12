package transport

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/internal/cache"
	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/internal/storage/sqlite"
	"github.com/kostyay/ecom/provider"
)

type pipelineClock struct{ now time.Time }

func (clock *pipelineClock) Now() time.Time { return clock.now }

type pipeline struct {
	service    *CachedHTTPService
	repository *sqlite.RawResponseRepository
	permits    *fakePermitAcquirer
	clock      *pipelineClock
	requests   int
	respond    func(context.Context, provider.ResourceRequest, int) (provider.ResourceResponse, error)
}

func newPipeline(t *testing.T, retries int, respond func(context.Context, provider.ResourceRequest, int) (provider.ResourceResponse, error)) *pipeline {
	t.Helper()
	clock := &pipelineClock{now: time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)}
	database, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	repository, err := sqlite.NewRawResponseRepository(database, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	cacheService, err := cache.NewService(repository, clock, cache.Limits{MaxSize: 1024 * 1024, MaxResponseSize: 64 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	result := &pipeline{repository: repository, permits: &fakePermitAcquirer{}, clock: clock, respond: respond}
	network := retryServiceFunc(func(ctx context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
		result.requests++
		return result.respond(ctx, request, result.requests)
	})
	retry, err := NewRetryExecutor(network, result.permits, "bike-discount", retries, &fakeRetryScheduler{now: clock.now}, RandomFunc(func() float64 { return 1 }))
	if err != nil {
		t.Fatal(err)
	}
	result.service, err = NewCachedHTTPService("bike-discount", cacheService, retry, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func pipelineRequest() provider.ResourceRequest {
	return provider.ResourceRequest{
		URL: "https://shop.example/search?category=bikes",
		Query: []provider.RequestValue{
			{Name: "page", Values: []string{"1"}},
			{Name: "token", Values: []string{"secret"}, Sensitive: true},
		},
		Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"},
	}
}

func successfulPipelineResponse(body string, retrievedAt time.Time) provider.ResourceResponse {
	return provider.ResourceResponse{
		Body: []byte(body), StatusCode: http.StatusOK,
		FinalURL:    "https://shop.example/search?category=bikes&page=1&token=secret",
		SafeHeaders: map[string][]string{"Content-Type": {"text/html; charset=utf-8"}, "ETag": {`"one"`}},
		RetrievedAt: retrievedAt, Transport: provider.TransportHTTP,
	}
}

func TestCachedHTTPMissStoresThenHitAvoidsNetworkAndPermits(t *testing.T) {
	p := newPipeline(t, 0, func(_ context.Context, _ provider.ResourceRequest, _ int) (provider.ResourceResponse, error) {
		return successfulPipelineResponse(strings.Repeat("product ", 300), pTime()), nil
	})
	request := pipelineRequest()

	fresh, err := p.service.Fetch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Cache.Hit || fresh.Cache.Stale || fresh.Cache.TTL != 24*time.Hour || fresh.Cache.StoredAt != p.clock.now {
		t.Fatalf("fresh cache metadata = %#v", fresh.Cache)
	}
	if fresh.RetrievedAt != pTime() || fresh.Transport != provider.TransportHTTP {
		t.Fatalf("fresh transport metadata = %#v", fresh)
	}
	p.clock.now = p.clock.now.Add(time.Hour)
	hit, err := p.service.Fetch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if p.requests != 1 || p.permits.acquires != 1 || p.permits.releases != 1 {
		t.Fatalf("requests/acquires/releases = %d/%d/%d, want 1/1/1", p.requests, p.permits.acquires, p.permits.releases)
	}
	if !hit.Cache.Hit || hit.Cache.Stale || hit.Cache.Age != time.Hour || hit.RetrievedAt != fresh.Cache.StoredAt {
		t.Fatalf("hit cache metadata = %#v, retrieved = %v", hit.Cache, hit.RetrievedAt)
	}
	if !reflect.DeepEqual(hit.Body, fresh.Body) {
		t.Fatal("cache changed response bytes")
	}

	entries, err := p.repository.ListByProvider(context.Background(), "bike-discount")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].URL != "https://shop.example/search" || entries[0].Encoding != cache.EncodingGzip {
		t.Fatalf("stored entry = %#v", entries)
	}
}

func TestCachedHTTPSeparatesMarkets(t *testing.T) {
	p := newPipeline(t, 0, func(_ context.Context, request provider.ResourceRequest, _ int) (provider.ResourceResponse, error) {
		return successfulPipelineResponse(request.Market.Country, pTime()), nil
	})
	de := pipelineRequest()
	fr := pipelineRequest()
	fr.Market.Country = "FR"
	for _, request := range []provider.ResourceRequest{de, fr, de, fr} {
		if _, err := p.service.Fetch(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	if p.requests != 2 || p.permits.acquires != 2 {
		t.Fatalf("network requests/permits = %d/%d, want 2/2", p.requests, p.permits.acquires)
	}
	entries, err := p.repository.ListByProvider(context.Background(), "bike-discount")
	if err != nil || len(entries) != 2 {
		t.Fatalf("market entries = %d, error = %v", len(entries), err)
	}
}

func TestCachedHTTPRefreshSuccessAndFailurePreserveEntry(t *testing.T) {
	failure := false
	p := newPipeline(t, 0, func(_ context.Context, _ provider.ResourceRequest, call int) (provider.ResourceResponse, error) {
		if failure {
			return provider.ResourceResponse{}, provider.NewError(provider.ErrorCodeHTTPFailure, "failed", nil)
		}
		return successfulPipelineResponse(string(rune('0'+call)), pTime()), nil
	})
	request := pipelineRequest()
	first, err := p.service.Fetch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	p.clock.now = p.clock.now.Add(time.Hour)
	request.Cache.Refresh = true
	second, err := p.service.Fetch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if string(second.Body) == string(first.Body) || second.Cache.Hit {
		t.Fatalf("refresh response = %q, metadata = %#v", second.Body, second.Cache)
	}
	failure = true
	request.Cache.StaleIfError = true
	if _, err := p.service.Fetch(context.Background(), request); !errors.Is(err, provider.ErrorCodeHTTPFailure) {
		t.Fatalf("failed valid refresh error = %v", err)
	}
	failure = false
	request.Cache = provider.CachePolicy{}
	stored, err := p.service.Fetch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.Body) != string(second.Body) || !stored.Cache.Hit || p.requests != 3 {
		t.Fatalf("preserved response = %q, cache = %#v, requests = %d", stored.Body, stored.Cache, p.requests)
	}
}

func TestCachedHTTPExpiredStaleRequiresOptIn(t *testing.T) {
	failure := false
	p := newPipeline(t, 0, func(_ context.Context, _ provider.ResourceRequest, _ int) (provider.ResourceResponse, error) {
		if failure {
			return provider.ResourceResponse{}, provider.NewError(provider.ErrorCodeAccessBlocked, "blocked", nil)
		}
		return successfulPipelineResponse("old", pTime()), nil
	})
	request := pipelineRequest()
	if _, err := p.service.Fetch(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	p.clock.now = p.clock.now.Add(24 * time.Hour)
	failure = true
	if _, err := p.service.Fetch(context.Background(), request); !errors.Is(err, provider.ErrorCodeAccessBlocked) {
		t.Fatalf("expired request error = %v", err)
	}
	request.Cache.StaleIfError = true
	stale, err := p.service.Fetch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if string(stale.Body) != "old" || !stale.Cache.Hit || !stale.Cache.Stale || stale.Cache.Age != 24*time.Hour {
		t.Fatalf("stale response = %q, metadata = %#v", stale.Body, stale.Cache)
	}
}

func TestCachedHTTPRetriesThenStoresOnlySuccess(t *testing.T) {
	p := newPipeline(t, 1, func(_ context.Context, _ provider.ResourceRequest, call int) (provider.ResourceResponse, error) {
		if call == 1 {
			return provider.ResourceResponse{StatusCode: 503, SafeHeaders: map[string][]string{"Retry-After": {"0"}}}, provider.NewError(provider.ErrorCodeRetryableHTTP, "retry", nil)
		}
		return successfulPipelineResponse("success", pTime()), nil
	})
	request := pipelineRequest()
	if _, err := p.service.Fetch(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.Fetch(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if p.requests != 2 || p.permits.acquires != 2 || p.permits.releases != 2 {
		t.Fatalf("retry requests/acquires/releases = %d/%d/%d", p.requests, p.permits.acquires, p.permits.releases)
	}
}

func TestCachedHTTPDoesNotCacheErrorsOrInvalidResponses(t *testing.T) {
	tests := []struct {
		name     string
		response provider.ResourceResponse
		err      error
	}{
		{name: "access block", err: provider.NewError(provider.ErrorCodeAccessBlocked, "blocked", nil)},
		{name: "challenge", err: provider.NewError(provider.ErrorCodeBrowserChallengeRequired, "challenge", nil)},
		{name: "too large", err: provider.NewError(provider.ErrorCodeResponseTooLarge, "large", nil)},
		{name: "server error", err: provider.NewError(provider.ErrorCodeRetryableHTTP, "server", nil)},
		{name: "non-2xx", response: provider.ResourceResponse{StatusCode: 304, FinalURL: "https://shop.example", Transport: provider.TransportHTTP}},
		{name: "unsafe final URL", response: provider.ResourceResponse{StatusCode: 200, FinalURL: "https://user:secret@shop.example", Transport: provider.TransportHTTP}},
		{name: "browser response", response: provider.ResourceResponse{StatusCode: 200, FinalURL: "https://shop.example", Transport: provider.TransportBrowser}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := newPipeline(t, 0, func(context.Context, provider.ResourceRequest, int) (provider.ResourceResponse, error) {
				return test.response, test.err
			})
			for range 2 {
				if _, err := p.service.Fetch(context.Background(), pipelineRequest()); err == nil {
					t.Fatal("Fetch error = nil")
				}
			}
			entries, err := p.repository.ListByProvider(context.Background(), "bike-discount")
			if err != nil || len(entries) != 0 || p.requests != 2 {
				t.Fatalf("entries/requests/error = %d/%d/%v", len(entries), p.requests, err)
			}
		})
	}
}

func TestCachedHTTPCancellationAndValidationAvoidNetwork(t *testing.T) {
	p := newPipeline(t, 0, func(context.Context, provider.ResourceRequest, int) (provider.ResourceResponse, error) {
		t.Fatal("network called")
		return provider.ResourceResponse{}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.service.Fetch(ctx, pipelineRequest()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Fetch error = %v", err)
	}
	invalid := pipelineRequest()
	invalid.Market = provider.Market{}
	if _, err := p.service.Fetch(context.Background(), invalid); !errors.Is(err, provider.ErrorCodeInvalidResourceRequest) {
		t.Fatalf("invalid market error = %v", err)
	}
	invalid = pipelineRequest()
	invalid.Transport.Required = provider.TransportBrowser
	if _, err := p.service.Fetch(context.Background(), invalid); !errors.Is(err, provider.ErrorCodeInvalidResourceRequest) {
		t.Fatalf("required browser error = %v", err)
	}
	if p.requests != 0 || p.permits.acquires != 0 {
		t.Fatalf("requests/permits = %d/%d", p.requests, p.permits.acquires)
	}
}

type failingPipelineRepository struct {
	getErr error
	putErr error
}

func (repository failingPipelineRepository) Put(context.Context, cache.Entry) (cache.Entry, error) {
	return cache.Entry{}, repository.putErr
}

func (repository failingPipelineRepository) Get(context.Context, string, time.Time) (cache.Entry, error) {
	return cache.Entry{}, repository.getErr
}

func (repository failingPipelineRepository) ListByProvider(context.Context, string) ([]cache.Entry, error) {
	return nil, repository.getErr
}

func TestCachedHTTPReturnsRepositoryReadErrorsWithoutNetwork(t *testing.T) {
	repositoryErr := errors.New("repository unavailable")
	cacheService, err := cache.NewService(failingPipelineRepository{getErr: repositoryErr}, &pipelineClock{now: pTime()}, cache.Limits{MaxSize: 1024, MaxResponseSize: 1024})
	if err != nil {
		t.Fatal(err)
	}
	networkCalls := 0
	service, err := NewCachedHTTPService("bike-discount", cacheService, retryServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		networkCalls++
		return successfulPipelineResponse("body", pTime()), nil
	}), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Fetch(context.Background(), pipelineRequest()); !errors.Is(err, repositoryErr) {
		t.Fatalf("Fetch error = %v", err)
	}
	if networkCalls != 0 {
		t.Fatalf("network calls = %d, want 0", networkCalls)
	}
}

func TestCachedHTTPReturnsRepositoryWriteErrorsAfterNetwork(t *testing.T) {
	repositoryErr := errors.New("repository unavailable")
	cacheService, err := cache.NewService(failingPipelineRepository{getErr: cache.ErrEntryNotFound, putErr: repositoryErr}, &pipelineClock{now: pTime()}, cache.Limits{MaxSize: 1024, MaxResponseSize: 1024})
	if err != nil {
		t.Fatal(err)
	}
	networkCalls := 0
	service, err := NewCachedHTTPService("bike-discount", cacheService, retryServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		networkCalls++
		return successfulPipelineResponse("body", pTime()), nil
	}), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Fetch(context.Background(), pipelineRequest()); !errors.Is(err, repositoryErr) {
		t.Fatalf("Fetch error = %v", err)
	}
	if networkCalls != 1 {
		t.Fatalf("network calls = %d, want 1", networkCalls)
	}
}

func TestNewCachedHTTPServiceValidatesDependencies(t *testing.T) {
	validCache, err := cache.NewService(failingPipelineRepository{getErr: cache.ErrEntryNotFound}, &pipelineClock{now: pTime()}, cache.Limits{MaxSize: 1, MaxResponseSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	validHTTP := retryServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		return provider.ResourceResponse{}, nil
	})
	tests := []struct {
		name string
		make func() (*CachedHTTPService, error)
	}{
		{name: "provider", make: func() (*CachedHTTPService, error) { return NewCachedHTTPService(" ", validCache, validHTTP, time.Hour) }},
		{name: "cache", make: func() (*CachedHTTPService, error) { return NewCachedHTTPService("shop", nil, validHTTP, time.Hour) }},
		{name: "HTTP", make: func() (*CachedHTTPService, error) { return NewCachedHTTPService("shop", validCache, nil, time.Hour) }},
		{name: "TTL", make: func() (*CachedHTTPService, error) { return NewCachedHTTPService("shop", validCache, validHTTP, 0) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.make(); err == nil {
				t.Fatal("constructor error = nil")
			}
		})
	}
}

func pTime() time.Time {
	return time.Date(2026, 8, 12, 11, 59, 59, 0, time.UTC)
}

func TestConfiguredHTTPResourceServiceWiresSQLite(t *testing.T) {
	database, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	settings := config.Settings{
		Cache:   config.CacheSettings{TTL: time.Hour, MaxSize: 1024 * 1024, MaxResponseSize: 4096},
		Network: config.NetworkSettings{RequestsPerSecond: 1000, MaxConcurrentHTTP: 1, MaxConcurrentBrowser: 1, Retries: 0},
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/plain"}}, Body: ioNopCloser("body"), Request: request}, nil
	})}
	service, err := NewConfiguredHTTPResourceService(database, client, "bike-discount", settings)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := service.Fetch(context.Background(), pipelineRequest()); err != nil || string(response.Body) != "body" {
		t.Fatalf("configured Fetch response/error = %q/%v", response.Body, err)
	}
}

func ioNopCloser(value string) *stringReadCloser {
	return &stringReadCloser{Reader: strings.NewReader(value)}
}

type stringReadCloser struct{ *strings.Reader }

func (closer *stringReadCloser) Close() error { return nil }
