package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
)

type retryServiceFunc func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error)

func (function retryServiceFunc) Fetch(ctx context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
	return function(ctx, request)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type fakePermitAcquirer struct {
	mu       sync.Mutex
	acquires int
	releases int
	acquire  func(context.Context, int) error
}

func (fake *fakePermitAcquirer) Acquire(ctx context.Context, _ string, _ provider.TransportMode) (*RequestPermit, error) {
	fake.mu.Lock()
	fake.acquires++
	number := fake.acquires
	fake.mu.Unlock()
	if fake.acquire != nil {
		if err := fake.acquire(ctx, number); err != nil {
			return nil, err
		}
	}
	return &RequestPermit{release: func() {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.releases++
	}}, nil
}

type fakeRetryScheduler struct {
	now   time.Time
	waits []time.Duration
	wait  func(context.Context, time.Duration) error
}

func (fake *fakeRetryScheduler) Now() time.Time { return fake.now }

func (fake *fakeRetryScheduler) Wait(ctx context.Context, delay time.Duration) error {
	fake.waits = append(fake.waits, delay)
	if fake.wait != nil {
		return fake.wait(ctx, delay)
	}
	return nil
}

func TestRetryExecutorRetriesClassifiedHTTPStatuses(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			attempts := 0
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				attempts++
				responseStatus := http.StatusOK
				if attempts == 1 {
					responseStatus = status
				}
				return &http.Response{
					StatusCode: responseStatus,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("body")),
					Request:    request,
				}, nil
			})}

			httpExecutor := mustHTTPExecutor(t, client, ClockFunc(time.Now), 1024)
			limits := &fakePermitAcquirer{}
			scheduler := &fakeRetryScheduler{}
			executor := mustRetryExecutor(t, httpExecutor, limits, 1, scheduler, RandomFunc(func() float64 { return 1 }))
			response, err := executor.Fetch(context.Background(), provider.ResourceRequest{URL: "https://example.test/item"})
			if err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
			if response.StatusCode != http.StatusOK || attempts != 2 || limits.acquires != 2 || limits.releases != 2 {
				t.Fatalf("response status = %d, attempts = %d, acquires = %d, releases = %d", response.StatusCode, attempts, limits.acquires, limits.releases)
			}
		})
	}
}

func TestRetryExecutorDoesNotRetryOtherErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "invalid request", err: provider.NewError(provider.ErrorCodeInvalidResourceRequest, "invalid", nil)},
		{name: "access block", err: provider.NewError(provider.ErrorCodeAccessBlocked, "blocked", nil)},
		{name: "challenge", err: provider.NewError(provider.ErrorCodeBrowserChallengeRequired, "challenge", nil)},
		{name: "other HTTP failure", err: provider.NewError(provider.ErrorCodeHTTPFailure, "failure", nil)},
		{name: "response too large", err: provider.NewError(provider.ErrorCodeResponseTooLarge, "large", nil)},
		{name: "context", err: context.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			service := retryServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
				attempts++
				return provider.ResourceResponse{StatusCode: 400}, test.err
			})
			executor := mustRetryExecutor(t, service, &fakePermitAcquirer{}, 3, &fakeRetryScheduler{}, RandomFunc(func() float64 { return 1 }))
			_, err := executor.Fetch(context.Background(), provider.ResourceRequest{})
			if !errors.Is(err, test.err) {
				t.Fatalf("Fetch() error = %v, want errors.Is(_, %v)", err, test.err)
			}
			if attempts != 1 {
				t.Fatalf("attempts = %d, want 1", attempts)
			}
		})
	}
}

func TestRetryExecutorConfiguredRetriesAreAfterFirstAttempt(t *testing.T) {
	for _, retries := range []int{0, 1, 3} {
		t.Run(fmt.Sprint(retries), func(t *testing.T) {
			attempts := 0
			service := retryServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
				attempts++
				return provider.ResourceResponse{}, provider.NewError(provider.ErrorCodeRetryableHTTP, "retry", nil)
			})
			executor := mustRetryExecutor(t, service, &fakePermitAcquirer{}, retries, &fakeRetryScheduler{}, RandomFunc(func() float64 { return 1 }))
			_, err := executor.Fetch(context.Background(), provider.ResourceRequest{})
			if !errors.Is(err, provider.ErrorCodeRetryableHTTP) {
				t.Fatalf("Fetch() error = %v", err)
			}
			if attempts != retries+1 {
				t.Fatalf("attempts = %d, want %d", attempts, retries+1)
			}
		})
	}
}

func TestRetryExecutorRetryAfter(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		header string
		random float64
		want   time.Duration
	}{
		{name: "delta seconds", header: "12", want: 12 * time.Second},
		{name: "HTTP date", header: now.Add(25 * time.Second).Format(http.TimeFormat), want: 25 * time.Second},
		{name: "invalid uses exponential jitter", header: "later", random: 1, want: time.Second},
		{name: "past date uses exponential jitter", header: now.Add(-time.Second).Format(http.TimeFormat), random: 1, want: time.Second},
		{name: "large delta is capped", header: "999999", want: MaximumRetryDelay},
		{name: "future date is capped", header: now.Add(24 * time.Hour).Format(http.TimeFormat), want: MaximumRetryDelay},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			service := retryServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
				attempts++
				if attempts == 1 {
					return provider.ResourceResponse{SafeHeaders: map[string][]string{"retry-after": {test.header}}}, provider.NewError(provider.ErrorCodeRetryableHTTP, "retry", nil)
				}
				return provider.ResourceResponse{}, nil
			})
			scheduler := &fakeRetryScheduler{now: now}
			executor := mustRetryExecutor(t, service, &fakePermitAcquirer{}, 1, scheduler, RandomFunc(func() float64 { return test.random }))
			if _, err := executor.Fetch(context.Background(), provider.ResourceRequest{}); err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
			if len(scheduler.waits) != 1 || scheduler.waits[0] != test.want {
				t.Fatalf("waits = %v, want [%v]", scheduler.waits, test.want)
			}
		})
	}
}

func TestRetryExecutorUsesExponentialDelayAndBoundedJitter(t *testing.T) {
	attempts := 0
	service := retryServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		attempts++
		return provider.ResourceResponse{}, provider.NewError(provider.ErrorCodeRetryableHTTP, "retry", nil)
	})
	scheduler := &fakeRetryScheduler{}
	randomValues := []float64{0, 0.5, 1}
	randomIndex := 0
	executor := mustRetryExecutor(t, service, &fakePermitAcquirer{}, 3, scheduler, RandomFunc(func() float64 {
		value := randomValues[randomIndex]
		randomIndex++
		return value
	}))
	_, _ = executor.Fetch(context.Background(), provider.ResourceRequest{})
	want := []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond, 4 * time.Second}
	if fmt.Sprint(scheduler.waits) != fmt.Sprint(want) {
		t.Fatalf("waits = %v, want %v", scheduler.waits, want)
	}
}

func TestRetryExecutorPreservesFinalResponseAndError(t *testing.T) {
	cause := errors.New("temporary cause")
	attempts := 0
	service := retryServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		attempts++
		response := provider.ResourceResponse{StatusCode: 502 + attempts, FinalURL: fmt.Sprintf("https://example.test/%d", attempts)}
		return response, provider.NewError(provider.ErrorCodeRetryableHTTP, "retry", cause)
	})
	executor := mustRetryExecutor(t, service, &fakePermitAcquirer{}, 1, &fakeRetryScheduler{}, RandomFunc(func() float64 { return 1 }))
	response, err := executor.Fetch(context.Background(), provider.ResourceRequest{})
	if response.StatusCode != 504 || response.FinalURL != "https://example.test/2" {
		t.Fatalf("response = %#v", response)
	}
	if !errors.Is(err, provider.ErrorCodeRetryableHTTP) || !errors.Is(err, cause) {
		t.Fatalf("Fetch() error = %v; coded and cause chains must be preserved", err)
	}
}

func TestRetryExecutorCancellationDuringDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	service := retryServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		return provider.ResourceResponse{StatusCode: 503}, provider.NewError(provider.ErrorCodeRetryableHTTP, "retry", nil)
	})
	scheduler := &fakeRetryScheduler{wait: func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}}
	executor := mustRetryExecutor(t, service, &fakePermitAcquirer{}, 2, scheduler, RandomFunc(func() float64 { return 1 }))
	response, err := executor.Fetch(ctx, provider.ResourceRequest{})
	if !errors.Is(err, context.Canceled) || response.StatusCode != 503 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestRetryExecutorCancellationDuringAcquire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	limits := &fakePermitAcquirer{acquire: func(_ context.Context, attempt int) error {
		if attempt == 2 {
			cancel()
			return ctx.Err()
		}
		return nil
	}}
	service := retryServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		return provider.ResourceResponse{StatusCode: 503}, provider.NewError(provider.ErrorCodeRetryableHTTP, "retry", nil)
	})
	executor := mustRetryExecutor(t, service, limits, 2, &fakeRetryScheduler{}, RandomFunc(func() float64 { return 1 }))
	response, err := executor.Fetch(ctx, provider.ResourceRequest{})
	if !errors.Is(err, context.Canceled) || response.StatusCode != 503 || limits.releases != 1 {
		t.Fatalf("response = %#v, error = %v, releases = %d", response, err, limits.releases)
	}
}

func TestRetryExecutorCancellationDuringAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	service := retryServiceFunc(func(ctx context.Context, _ provider.ResourceRequest) (provider.ResourceResponse, error) {
		cancel()
		return provider.ResourceResponse{StatusCode: 503}, ctx.Err()
	})
	limits := &fakePermitAcquirer{}
	executor := mustRetryExecutor(t, service, limits, 3, &fakeRetryScheduler{}, RandomFunc(func() float64 { return 1 }))
	_, err := executor.Fetch(ctx, provider.ResourceRequest{})
	if !errors.Is(err, context.Canceled) || limits.acquires != 1 || limits.releases != 1 {
		t.Fatalf("error = %v, acquires = %d, releases = %d", err, limits.acquires, limits.releases)
	}
}

func TestNewRetryExecutorValidatesDependencies(t *testing.T) {
	service := retryServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		return provider.ResourceResponse{}, nil
	})
	limits := &fakePermitAcquirer{}
	scheduler := &fakeRetryScheduler{}
	random := RandomFunc(func() float64 { return 0 })
	tests := []struct {
		name string
		make func() (*RetryExecutor, error)
	}{
		{name: "service", make: func() (*RetryExecutor, error) { return NewRetryExecutor(nil, limits, "provider", 1, scheduler, random) }},
		{name: "limits", make: func() (*RetryExecutor, error) {
			return NewRetryExecutor(service, nil, "provider", 1, scheduler, random)
		}},
		{name: "provider", make: func() (*RetryExecutor, error) { return NewRetryExecutor(service, limits, " ", 1, scheduler, random) }},
		{name: "retries", make: func() (*RetryExecutor, error) {
			return NewRetryExecutor(service, limits, "provider", -1, scheduler, random)
		}},
		{name: "scheduler", make: func() (*RetryExecutor, error) { return NewRetryExecutor(service, limits, "provider", 1, nil, random) }},
		{name: "random", make: func() (*RetryExecutor, error) {
			return NewRetryExecutor(service, limits, "provider", 1, scheduler, nil)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.make(); err == nil {
				t.Fatal("NewRetryExecutor() error = nil")
			}
		})
	}
}

func mustRetryExecutor(t *testing.T, service provider.ResourceService, limits permitAcquirer, retries int, scheduler WaitScheduler, random Random) *RetryExecutor {
	t.Helper()
	executor, err := NewRetryExecutor(service, limits, "bike-discount", retries, scheduler, random)
	if err != nil {
		t.Fatalf("NewRetryExecutor() error = %v", err)
	}
	return executor
}
