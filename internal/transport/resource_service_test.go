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

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/internal/storage/sqlite"
	"github.com/kostyay/ecom/provider"
)

type selectorServiceFunc func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error)

func (function selectorServiceFunc) Fetch(ctx context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
	return function(ctx, request)
}

type sequenceClock struct {
	times []time.Time
	index int
}

func (clock *sequenceClock) Now() time.Time {
	value := clock.times[clock.index]
	clock.index++
	return value
}

func TestResourceServiceSucceedsAtEachStage(t *testing.T) {
	for _, successMode := range defaultTransportOrder {
		t.Run(string(successMode), func(t *testing.T) {
			var calls []provider.TransportMode
			services := make(map[provider.TransportMode]provider.ResourceService)
			for _, mode := range defaultTransportOrder {
				services[mode] = selectorServiceFunc(func(_ context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
					calls = append(calls, mode)
					if request.Transport.Required != mode || len(request.Transport.Preferred) != 0 {
						t.Fatalf("attempt policy = %#v", request.Transport)
					}
					if mode == successMode {
						return provider.ResourceResponse{Body: []byte(mode), Transport: mode}, nil
					}
					code := provider.ErrorCodeAccessBlocked
					if mode == provider.TransportBrowser {
						code = provider.ErrorCodeBrowserFailure
					}
					return provider.ResourceResponse{}, provider.NewError(code, "failed", nil)
				})
			}
			service := newSelector(t, services, ClockFunc(time.Now))
			response, err := service.Fetch(t.Context(), selectorRequest())
			if err != nil || response.Transport != successMode {
				t.Fatalf("Fetch() = %#v, %v", response, err)
			}
			wantCalls := defaultTransportOrder[:indexOfMode(successMode)+1]
			if !reflect.DeepEqual(calls, wantCalls) || len(response.Attempts) != len(wantCalls) || response.Attempts[len(response.Attempts)-1].Outcome != provider.AttemptSucceeded {
				t.Fatalf("calls/attempts = %v/%#v, want %v", calls, response.Attempts, wantCalls)
			}
		})
	}
}

func TestResourceServiceRequiredAndPreferredAreFullOrders(t *testing.T) {
	var calls []provider.TransportMode
	recorder := func(mode provider.TransportMode, err error) provider.ResourceService {
		return selectorServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
			calls = append(calls, mode)
			return provider.ResourceResponse{}, err
		})
	}
	service := newSelector(t, map[provider.TransportMode]provider.ResourceService{
		provider.TransportHTTP:    recorder(provider.TransportHTTP, nil),
		provider.TransportBrowser: recorder(provider.TransportBrowser, provider.NewError(provider.ErrorCodeBrowserFailure, "failed", nil)),
		provider.TransportCDP:     recorder(provider.TransportCDP, nil),
	}, ClockFunc(time.Now))

	request := selectorRequest()
	request.Transport.Required = provider.TransportCDP
	if _, err := service.Fetch(t.Context(), request); err != nil || !reflect.DeepEqual(calls, []provider.TransportMode{provider.TransportCDP}) {
		t.Fatalf("required calls/error = %v/%v", calls, err)
	}
	calls = nil
	request.Transport = provider.TransportPolicy{Preferred: []provider.TransportMode{provider.TransportBrowser, provider.TransportCDP}}
	if _, err := service.Fetch(t.Context(), request); err != nil || !reflect.DeepEqual(calls, []provider.TransportMode{provider.TransportBrowser, provider.TransportCDP}) {
		t.Fatalf("preferred calls/error = %v/%v", calls, err)
	}
}

func TestResourceServiceRejectsInvalidPoliciesWithoutAttempt(t *testing.T) {
	calls := 0
	fake := selectorServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		calls++
		return provider.ResourceResponse{}, nil
	})
	service, err := NewResourceService(fake, fake, fake, ClockFunc(time.Now))
	if err != nil {
		t.Fatal(err)
	}
	tests := []provider.TransportPolicy{
		{Required: "unknown"},
		{Required: provider.TransportHTTP, Preferred: []provider.TransportMode{provider.TransportHTTP}},
		{Preferred: []provider.TransportMode{"unknown"}},
		{Preferred: []provider.TransportMode{provider.TransportHTTP, provider.TransportHTTP}},
	}
	for _, policy := range tests {
		request := selectorRequest()
		request.Transport = policy
		if _, err := service.Fetch(t.Context(), request); !errors.Is(err, provider.ErrorCodeInvalidResourceRequest) {
			t.Fatalf("Fetch(%#v) error = %v", policy, err)
		}
	}
	if calls != 0 {
		t.Fatalf("transport calls = %d", calls)
	}
}

func TestResourceServiceDoesNotFallbackForTerminalClasses(t *testing.T) {
	tests := []error{
		provider.NewError(provider.ErrorCodeInvalidResourceRequest, "invalid", nil),
		provider.NewError(provider.ErrorCodeResponseTooLarge, "large", nil),
		provider.NewError(provider.ErrorCodeHTTPFailure, "not found", nil),
		errors.New("provider parsing failed"),
		context.Canceled,
		context.DeadlineExceeded,
	}
	for _, terminalErr := range tests {
		t.Run(terminalErr.Error(), func(t *testing.T) {
			calls := 0
			httpService := selectorServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
				calls++
				return provider.ResourceResponse{StatusCode: http.StatusNotFound}, terminalErr
			})
			unused := selectorServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
				calls++
				return provider.ResourceResponse{}, nil
			})
			service, _ := NewResourceService(httpService, unused, unused, ClockFunc(time.Now))
			response, err := service.Fetch(t.Context(), selectorRequest())
			if !errors.Is(err, terminalErr) || calls != 1 || response.StatusCode != http.StatusNotFound || len(response.Attempts) != 1 {
				t.Fatalf("Fetch() = %#v, %v; calls = %d", response, err, calls)
			}
		})
	}
}

func TestResourceServiceBrowserFallbackClasses(t *testing.T) {
	for _, code := range []provider.ErrorCode{provider.ErrorCodeAccessBlocked, provider.ErrorCodeBrowserFailure, provider.ErrorCodeBrowserChallengeTimeout} {
		t.Run(string(code), func(t *testing.T) {
			browser := selectorServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
				return provider.ResourceResponse{}, provider.NewError(code, "failed", nil)
			})
			cdp := selectorServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
				return provider.ResourceResponse{Transport: provider.TransportCDP}, nil
			})
			service := newSelector(t, map[provider.TransportMode]provider.ResourceService{
				provider.TransportHTTP:    failingService(provider.ErrorCodeTransportUnavailable),
				provider.TransportBrowser: browser,
				provider.TransportCDP:     cdp,
			}, ClockFunc(time.Now))
			response, err := service.Fetch(t.Context(), selectorRequest())
			if err != nil || response.Transport != provider.TransportCDP || len(response.Attempts) != 3 {
				t.Fatalf("Fetch() = %#v, %v", response, err)
			}
		})
	}
}

func TestResourceServiceChallengeRequiredDoesNotSkipInteractiveBrowser(t *testing.T) {
	cdpCalls := 0
	browser := selectorServiceFunc(func(_ context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
		if !request.Interactive {
			t.Fatal("interactive flag was not shared")
		}
		return provider.ResourceResponse{}, provider.NewError(provider.ErrorCodeBrowserChallengeRequired, "challenge", nil)
	})
	cdp := selectorServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		cdpCalls++
		return provider.ResourceResponse{}, nil
	})
	service := newSelector(t, map[provider.TransportMode]provider.ResourceService{
		provider.TransportHTTP:    failingService(provider.ErrorCodeBrowserChallengeRequired),
		provider.TransportBrowser: browser,
		provider.TransportCDP:     cdp,
	}, ClockFunc(time.Now))
	request := selectorRequest()
	request.Interactive = true
	_, err := service.Fetch(t.Context(), request)
	if !errors.Is(err, provider.ErrorCodeBrowserChallengeRequired) || cdpCalls != 0 {
		t.Fatalf("Fetch() error/CDP calls = %v/%d", err, cdpCalls)
	}
}

func TestResourceServicePreservesLastUsefulResponseAndErrorWhenCDPUnavailable(t *testing.T) {
	httpResponse := provider.ResourceResponse{Body: []byte("blocked"), StatusCode: http.StatusForbidden, FinalURL: "https://shop.example/product"}
	service := newSelector(t, map[provider.TransportMode]provider.ResourceService{
		provider.TransportHTTP: selectorServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
			return httpResponse, provider.NewError(provider.ErrorCodeAccessBlocked, "blocked", nil)
		}),
		provider.TransportBrowser: failingService(provider.ErrorCodeBrowserFailure),
		provider.TransportCDP:     failingService(provider.ErrorCodeTransportUnavailable),
	}, ClockFunc(time.Now))
	response, err := service.Fetch(t.Context(), selectorRequest())
	if !errors.Is(err, provider.ErrorCodeBrowserFailure) || string(response.Body) != "blocked" || len(response.Attempts) != 3 || response.Attempts[2].Outcome != provider.AttemptUnavailable {
		t.Fatalf("Fetch() = %#v, %v", response, err)
	}
	serialized := strings.ToLower(strings.Join([]string{string(response.Attempts[0].Mode), string(response.Attempts[0].Code)}, " "))
	if strings.Contains(serialized, "shop.example") || strings.Contains(serialized, "secret") {
		t.Fatalf("attempt metadata leaked request data: %q", serialized)
	}
}

func TestResourceServiceCancelsBetweenAttemptsAndDoesNotMutateRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	request := selectorRequest()
	original := cloneResourceRequest(request)
	browserCalls := 0
	httpService := selectorServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		cancel()
		return provider.ResourceResponse{}, provider.NewError(provider.ErrorCodeAccessBlocked, "blocked", nil)
	})
	browser := selectorServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		browserCalls++
		return provider.ResourceResponse{}, nil
	})
	service, _ := NewResourceService(httpService, browser, browser, ClockFunc(time.Now))
	_, err := service.Fetch(ctx, request)
	if !errors.Is(err, context.Canceled) || browserCalls != 0 || !reflect.DeepEqual(request, original) {
		t.Fatalf("Fetch() error/calls/request = %v/%d/%#v", err, browserCalls, request)
	}
}

func TestResourceServiceSharesRequestInputsAndAttemptDuration(t *testing.T) {
	request := selectorRequest()
	var browserRequest provider.ResourceRequest
	httpService := selectorServiceFunc(func(_ context.Context, attempt provider.ResourceRequest) (provider.ResourceResponse, error) {
		attempt.Query[0].Values[0] = "mutated"
		attempt.Headers[0].Values[0] = "mutated"
		attempt.Body.Bytes[0] = 'x'
		return provider.ResourceResponse{}, provider.NewError(provider.ErrorCodeAccessBlocked, "blocked", nil)
	})
	browser := selectorServiceFunc(func(_ context.Context, attempt provider.ResourceRequest) (provider.ResourceResponse, error) {
		browserRequest = attempt
		return provider.ResourceResponse{Transport: provider.TransportBrowser}, nil
	})
	clock := &sequenceClock{times: []time.Time{time.Unix(0, 0), time.Unix(0, int64(time.Second)), time.Unix(0, int64(2*time.Second)), time.Unix(0, int64(5*time.Second))}}
	service := newSelector(t, map[provider.TransportMode]provider.ResourceService{
		provider.TransportHTTP: httpService, provider.TransportBrowser: browser, provider.TransportCDP: browser,
	}, clock)
	response, err := service.Fetch(t.Context(), request)
	if err != nil || browserRequest.Query[0].Values[0] != "one" || browserRequest.Headers[0].Values[0] != "value" || string(browserRequest.Body.Bytes) != "body" {
		t.Fatalf("shared inputs = %#v, error = %v", browserRequest, err)
	}
	if browserRequest.Market != request.Market || browserRequest.Cache != request.Cache || response.Attempts[0].Duration != time.Second || response.Attempts[1].Duration != 3*time.Second {
		t.Fatalf("request/attempt metadata = %#v/%#v", browserRequest, response.Attempts)
	}
}

func TestConfiguredResourceServiceSharesOneLimitManager(t *testing.T) {
	database, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	settings := config.Settings{
		Cache:   config.CacheSettings{TTL: time.Hour, MaxSize: 1024 * 1024, MaxResponseSize: 4096},
		Network: config.NetworkSettings{RequestsPerSecond: 100, MaxConcurrentHTTP: 2, MaxConcurrentBrowser: 1},
		Browser: config.BrowserSettings{InteractiveTimeout: time.Minute},
	}
	service, err := NewConfiguredResourceService(database, http.DefaultClient, "bike-discount", settings)
	if err != nil {
		t.Fatal(err)
	}
	httpLimits := service.services[provider.TransportHTTP].(*CachedHTTPService).http.(*RetryExecutor).limits
	browserLimits := service.services[provider.TransportBrowser].(*BrowserResourceService).limits
	cdpLimits := service.services[provider.TransportCDP].(*CDPResourceService).limits
	if httpLimits != browserLimits || browserLimits != cdpLimits {
		t.Fatal("configured transports do not share one request-limit manager")
	}
}

func selectorRequest() provider.ResourceRequest {
	return provider.ResourceRequest{
		Method: "POST", URL: "https://shop.example/product",
		Query:   []provider.RequestValue{{Name: "query", Values: []string{"one"}}},
		Headers: []provider.RequestValue{{Name: "X-Test", Values: []string{"value"}}},
		Body:    provider.RequestBody{Bytes: []byte("body")}, Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"},
		Cache: provider.CachePolicy{Refresh: true, StaleIfError: true},
	}
}

func failingService(code provider.ErrorCode) provider.ResourceService {
	return selectorServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		return provider.ResourceResponse{}, provider.NewError(code, "failed", nil)
	})
}

func newSelector(t *testing.T, services map[provider.TransportMode]provider.ResourceService, clock Clock) *ResourceService {
	t.Helper()
	service, err := NewResourceService(services[provider.TransportHTTP], services[provider.TransportBrowser], services[provider.TransportCDP], clock)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func indexOfMode(want provider.TransportMode) int {
	for index, mode := range defaultTransportOrder {
		if mode == want {
			return index
		}
	}
	return -1
}
