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
	"github.com/kostyay/ecom/provider"
)

type fakeBrowserBackend struct {
	command BrowserCommand
	result  BrowserResult
	err     error
	wait    bool
	called  chan struct{}
}

func (backend *fakeBrowserBackend) Navigate(ctx context.Context, command BrowserCommand) (BrowserResult, error) {
	backend.command = command
	if backend.called != nil {
		close(backend.called)
	}
	if backend.wait {
		<-ctx.Done()
		return BrowserResult{}, ctx.Err()
	}
	return backend.result, backend.err
}

func TestBrowserExecutorNavigatesAndReturnsClosedSnapshotAndState(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	expires := int64(2_000_000_000)
	initial := session.State{
		Cookies: []session.Cookie{{Name: "market", Value: "de", Domain: "shop.example", Path: "/"}},
		Origins: []session.Origin{{Origin: "https://shop.example", LocalStorage: []session.StorageEntry{{Name: "language", Value: "en"}}}},
	}
	updated := session.State{Cookies: []session.Cookie{{
		Name: "session", Value: "private", Domain: "shop.example", Path: "/", Expires: &expires,
	}}}
	backend := &fakeBrowserBackend{result: BrowserResult{
		HTML: []byte("<html><body>Helmet</body></html>"),
		DOM:  map[string][]string{"names": {"Helmet"}}, FinalURL: "https://shop.example/search?q=helmet&token=private",
		StatusCode: http.StatusOK, Headers: http.Header{"Content-Type": {"text/html"}, "Set-Cookie": {"secret=yes"}}, State: updated,
	}}
	executor := newTestBrowserExecutor(t, backend, now, 1024, true)
	request := provider.ResourceRequest{
		URL: "https://shop.example/search?q=helmet", Query: []provider.RequestValue{{Name: "token", Values: []string{"private"}, Sensitive: true}},
		Headers: []provider.RequestValue{{Name: "Accept-Language", Values: []string{"en"}}},
		DOM:     []provider.DOMExtraction{{Name: "names", Selector: ".name", Kind: provider.DOMText, All: true}},
	}

	got, err := executor.Navigate(context.Background(), request, initial)
	if err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if got.Response.FinalURL != "https://shop.example/search?q=helmet&token=%5BREDACTED%5D" || got.Response.StatusCode != 200 || got.Response.Transport != provider.TransportBrowser {
		t.Fatalf("Navigate() response metadata = %#v", got.Response)
	}
	if string(got.Response.Page.HTML) != "<html><body>Helmet</body></html>" || !reflect.DeepEqual(got.Response.Page.DOM, map[string][]string{"names": {"Helmet"}}) {
		t.Fatalf("Navigate() page = %#v", got.Response.Page)
	}
	if !reflect.DeepEqual(got.State, updated) || !reflect.DeepEqual(backend.command.State, initial) || !backend.command.Headed {
		t.Fatalf("state or headed setting was not passed: got %#v, command %#v", got.State, backend.command)
	}
	if _, unsafe := got.Response.SafeHeaders["Set-Cookie"]; unsafe || got.Response.SafeHeaders["Content-Type"][0] != "text/html" {
		t.Fatalf("safe headers = %#v", got.Response.SafeHeaders)
	}
	if !got.Response.RetrievedAt.Equal(now) {
		t.Fatalf("retrieved at = %v", got.Response.RetrievedAt)
	}
}

func TestConfiguredBrowserExecutorUsesBrowserSettings(t *testing.T) {
	executor, err := NewConfiguredBrowserExecutor(config.Settings{
		Cache:   config.CacheSettings{MaxResponseSize: 2048},
		Browser: config.BrowserSettings{Headed: true},
	})
	if err != nil {
		t.Fatalf("NewConfiguredBrowserExecutor() error = %v", err)
	}
	if !executor.headed || executor.maxResponseSize != 2048 {
		t.Fatalf("configured browser = %#v", executor)
	}
}

func TestBrowserExecutorHonorsCancellation(t *testing.T) {
	backend := &fakeBrowserBackend{wait: true, called: make(chan struct{})}
	executor := newTestBrowserExecutor(t, backend, time.Now(), 1024, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := executor.Fetch(ctx, provider.ResourceRequest{URL: "https://shop.example"})
		done <- err
	}()
	<-backend.called
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch() error = %v, want context.Canceled", err)
	}
}

func TestBrowserExecutorRejectsInvalidRequestsAndResults(t *testing.T) {
	tests := []struct {
		name    string
		request provider.ResourceRequest
		result  BrowserResult
		code    provider.ErrorCode
	}{
		{name: "unsafe request URL", request: provider.ResourceRequest{URL: "file:///private/etc/passwd"}, code: provider.ErrorCodeInvalidResourceRequest},
		{name: "unsupported method", request: provider.ResourceRequest{Method: "POST", URL: "https://shop.example"}, code: provider.ErrorCodeInvalidResourceRequest},
		{name: "invalid DOM", request: provider.ResourceRequest{URL: "https://shop.example", DOM: []provider.DOMExtraction{{Name: "x", Selector: ".x", Kind: provider.DOMAttribute}}}, code: provider.ErrorCodeInvalidResourceRequest},
		{name: "credential header", request: provider.ResourceRequest{URL: "https://shop.example", Headers: []provider.RequestValue{{Name: "Authorization", Values: []string{"private"}, Sensitive: true}}}, code: provider.ErrorCodeInvalidResourceRequest},
		{name: "unsafe final URL", request: provider.ResourceRequest{URL: "https://shop.example"}, result: validBrowserResult("javascript:alert(1)"), code: provider.ErrorCodeBrowserFailure},
		{name: "cross-origin redirect", request: provider.ResourceRequest{URL: "https://shop.example"}, result: validBrowserResult("https://evil.example"), code: provider.ErrorCodeBrowserFailure},
		{name: "large HTML", request: provider.ResourceRequest{URL: "https://shop.example"}, result: BrowserResult{HTML: []byte(strings.Repeat("x", 1025)), FinalURL: "https://shop.example", StatusCode: 200, State: session.State{}}, code: provider.ErrorCodeResponseTooLarge},
		{name: "large DOM", request: provider.ResourceRequest{URL: "https://shop.example"}, result: BrowserResult{DOM: map[string][]string{"x": {strings.Repeat("x", 1025)}}, FinalURL: "https://shop.example", StatusCode: 200, State: session.State{}}, code: provider.ErrorCodeResponseTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &fakeBrowserBackend{result: test.result}
			executor := newTestBrowserExecutor(t, backend, time.Now(), 1024, false)
			_, err := executor.Fetch(context.Background(), test.request)
			if !errors.Is(err, test.code) {
				t.Fatalf("Fetch() error = %v, want %s", err, test.code)
			}
		})
	}
}

func TestBrowserExecutorClassifiesChallengesAndBlocks(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		code   provider.ErrorCode
	}{
		{name: "challenge", status: 403, body: `<title>Just a moment...</title><script src="/cdn-cgi/challenge-platform/x"></script>`, code: provider.ErrorCodeBrowserChallengeRequired},
		{name: "access block", status: 403, body: `<title>Access Denied</title><p>Request blocked</p>`, code: provider.ErrorCodeAccessBlocked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := validBrowserResult("https://shop.example")
			result.StatusCode, result.HTML = test.status, []byte(test.body)
			backend := &fakeBrowserBackend{result: result}
			executor := newTestBrowserExecutor(t, backend, time.Now(), 4096, false)
			navigation, err := executor.Navigate(context.Background(), provider.ResourceRequest{URL: "https://shop.example"}, session.State{})
			if !errors.Is(err, test.code) || navigation.Response.Page == nil {
				t.Fatalf("Navigate() = %#v, %v; want %s with snapshot", navigation, err, test.code)
			}
		})
	}
}

func TestBrowserExecutorNavigationModes(t *testing.T) {
	backend := &fakeBrowserBackend{result: validBrowserResult("https://shop.example")}
	executor := newTestBrowserExecutor(t, backend, time.Now(), 1024, true)
	request := provider.ResourceRequest{URL: "https://shop.example"}
	if _, err := executor.NavigateHeadless(context.Background(), request, session.State{}); err != nil {
		t.Fatalf("NavigateHeadless() error = %v", err)
	}
	if backend.command.Headed || backend.command.WaitForChallenge {
		t.Fatalf("headless command = %#v", backend.command)
	}
	if _, err := executor.NavigateInteractive(context.Background(), request, session.State{}); err != nil {
		t.Fatalf("NavigateInteractive() error = %v", err)
	}
	if !backend.command.Headed || !backend.command.WaitForChallenge {
		t.Fatalf("interactive command = %#v", backend.command)
	}
}

func TestBrowserExecutorDoesNotExposeBackendErrorText(t *testing.T) {
	backend := &fakeBrowserBackend{err: errors.New("cookie=private-secret")}
	executor := newTestBrowserExecutor(t, backend, time.Now(), 1024, false)
	_, err := executor.Fetch(context.Background(), provider.ResourceRequest{URL: "https://shop.example"})
	if !errors.Is(err, provider.ErrorCodeBrowserFailure) || strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func newTestBrowserExecutor(t *testing.T, backend BrowserBackend, now time.Time, limit config.ByteSize, headed bool) *BrowserExecutor {
	t.Helper()
	executor, err := NewBrowserExecutor(backend, ClockFunc(func() time.Time { return now }), limit, headed)
	if err != nil {
		t.Fatalf("NewBrowserExecutor() error = %v", err)
	}
	return executor
}

func validBrowserResult(finalURL string) BrowserResult {
	return BrowserResult{HTML: []byte("<html></html>"), FinalURL: finalURL, StatusCode: 200, Headers: http.Header{"Content-Type": {"text/html"}}, State: session.State{}}
}

var _ BrowserBackend = (*fakeBrowserBackend)(nil)
