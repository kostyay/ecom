package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/provider"
)

func TestHTTPExecutorSendsRequestAndReturnsMetadata(t *testing.T) {
	retrievedAt := time.Date(2026, time.August, 12, 12, 30, 0, 0, time.FixedZone("test", 2*60*60))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", request.Method)
		}
		if got := request.URL.Query()["tag"]; len(got) != 2 || got[0] != "bike" || got[1] != "sale" {
			t.Errorf("query tag = %#v", got)
		}
		if got := request.Header.Values("X-Mode"); len(got) != 2 || got[0] != "one" || got[1] != "two" {
			t.Errorf("X-Mode = %#v", got)
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != "request body" {
			t.Errorf("body = %q", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Add("Vary", "Accept")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	executor := mustHTTPExecutor(t, server.Client(), ClockFunc(func() time.Time { return retrievedAt }), 1024)
	response, err := executor.Fetch(context.Background(), provider.ResourceRequest{
		Method:  http.MethodPost,
		URL:     server.URL + "/items?existing=yes",
		Query:   []provider.RequestValue{{Name: "tag", Values: []string{"bike", "sale"}}},
		Headers: []provider.RequestValue{{Name: "X-Mode", Values: []string{"one", "two"}}},
		Body:    provider.RequestBody{Bytes: []byte("request body")},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if response.StatusCode != http.StatusCreated || string(response.Body) != `{"ok":true}` {
		t.Fatalf("response = %#v", response)
	}
	if response.FinalURL != server.URL+"/items?existing=yes&tag=bike&tag=sale" {
		t.Errorf("final URL = %q", response.FinalURL)
	}
	if !response.RetrievedAt.Equal(retrievedAt.UTC()) || response.Transport != provider.TransportHTTP {
		t.Errorf("metadata = %#v", response)
	}
	if got := response.SafeHeaders["Content-Type"]; len(got) != 1 || got[0] != "application/json" {
		t.Errorf("safe Content-Type = %#v", got)
	}
}

func TestHTTPExecutorSendsSensitiveValuesWithoutReturningThem(t *testing.T) {
	const secret = "highly-secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("token") != secret || request.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("sensitive values were not sent")
		}
		http.Redirect(writer, request, "https://different.example/path", http.StatusFound)
	}))
	defer server.Close()

	executor := mustHTTPExecutor(t, server.Client(), ClockFunc(time.Now), 1024)
	_, err := executor.Fetch(context.Background(), provider.ResourceRequest{
		URL:     server.URL,
		Query:   []provider.RequestValue{{Name: "token", Values: []string{secret}, Sensitive: true}},
		Headers: []provider.RequestValue{{Name: "Authorization", Values: []string{"Bearer " + secret}, Sensitive: true}},
	})
	if !errors.Is(err, provider.ErrorCodeHTTPFailure) {
		t.Fatalf("Fetch() error = %v, want http_failure", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("safe error contains secret: %v", err)
	}
	var coded *provider.CodedError
	if !errors.As(err, &coded) || strings.Contains(coded.Unwrap().Error(), secret) {
		t.Fatalf("internal error contains secret: %#v", coded)
	}
}

func TestHTTPExecutorRedactsSensitiveQueryInFinalURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("ok"))
	}))
	defer server.Close()
	executor := mustHTTPExecutor(t, server.Client(), ClockFunc(time.Now), 1024)
	response, err := executor.Fetch(context.Background(), provider.ResourceRequest{
		URL:   server.URL,
		Query: []provider.RequestValue{{Name: "token", Values: []string{"secret"}, Sensitive: true}},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if strings.Contains(response.FinalURL, "secret") || !strings.Contains(response.FinalURL, "%5BREDACTED%5D") {
		t.Fatalf("final URL = %q", response.FinalURL)
	}
}

func TestHTTPExecutorRedirectPolicy(t *testing.T) {
	var authorization string
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		_, _ = writer.Write([]byte("target"))
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/same-origin" {
			http.Redirect(writer, request, "/done", http.StatusFound)
			return
		}
		if request.URL.Path == "/loop" {
			http.Redirect(writer, request, "/loop", http.StatusFound)
			return
		}
		if request.URL.Path == "/done" {
			_, _ = writer.Write([]byte("done"))
			return
		}
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer source.Close()

	executor := mustHTTPExecutor(t, source.Client(), ClockFunc(time.Now), 1024)
	response, err := executor.Fetch(context.Background(), provider.ResourceRequest{URL: source.URL + "/same-origin"})
	if err != nil || string(response.Body) != "done" {
		t.Fatalf("same-origin Fetch() = %#v, %v", response, err)
	}
	_, err = executor.Fetch(context.Background(), provider.ResourceRequest{
		URL:     source.URL + "/cross-origin",
		Headers: []provider.RequestValue{{Name: "Authorization", Values: []string{"Bearer secret"}, Sensitive: true}},
	})
	if !errors.Is(err, provider.ErrorCodeHTTPFailure) {
		t.Fatalf("cross-origin error = %v, want http_failure", err)
	}
	if authorization != "" {
		t.Fatalf("redirect leaked Authorization header %q", authorization)
	}
	_, err = executor.Fetch(context.Background(), provider.ResourceRequest{URL: source.URL + "/loop"})
	if !errors.Is(err, provider.ErrorCodeHTTPFailure) {
		t.Fatalf("redirect loop error = %v, want http_failure", err)
	}
}

func TestHTTPExecutorClassifiesStatusesAndChallenges(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		headers map[string]string
		body    string
		code    provider.ErrorCode
	}{
		{name: "unauthorized", status: 401, code: provider.ErrorCodeAccessBlocked},
		{name: "forbidden", status: 403, code: provider.ErrorCodeAccessBlocked},
		{name: "rate limited", status: 429, code: provider.ErrorCodeRetryableHTTP},
		{name: "bad gateway", status: 502, code: provider.ErrorCodeRetryableHTTP},
		{name: "service unavailable", status: 503, code: provider.ErrorCodeRetryableHTTP},
		{name: "gateway timeout", status: 504, code: provider.ErrorCodeRetryableHTTP},
		{name: "not found", status: 404, code: provider.ErrorCodeHTTPFailure},
		{name: "explicit challenge", status: 403, headers: map[string]string{"Cf-Mitigated": "challenge"}, code: provider.ErrorCodeBrowserChallengeRequired},
		{name: "clear challenge page", status: 503, headers: map[string]string{"Content-Type": "text/html"}, body: `<title>Just a moment...</title><script src="/cdn-cgi/challenge-platform/x"></script>`, code: provider.ErrorCodeBrowserChallengeRequired},
		{name: "clear access block page", status: 200, headers: map[string]string{"Content-Type": "text/html"}, body: `<title>Access Denied</title><p>You don't have permission to access this server.</p>`, code: provider.ErrorCodeAccessBlocked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				for name, value := range test.headers {
					writer.Header().Set(name, value)
				}
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			executor := mustHTTPExecutor(t, server.Client(), ClockFunc(time.Now), 4096)
			response, err := executor.Fetch(context.Background(), provider.ResourceRequest{URL: server.URL})
			if !errors.Is(err, test.code) {
				t.Fatalf("Fetch() error = %v, want %s", err, test.code)
			}
			if response.StatusCode != test.status || response.Transport != provider.TransportHTTP {
				t.Errorf("error response metadata = %#v", response)
			}
		})
	}
}

func TestHTTPExecutorReturnsOnlySafeResponseHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		writer.Header().Set("Cache-Control", "max-age=60")
		writer.Header().Set("Set-Cookie", "session=secret")
		writer.Header().Set("Www-Authenticate", "Bearer secret")
		writer.Header().Set("Content-Security-Policy", "default-src 'none'")
		writer.Header().Set("X-Secret", "secret")
		_, _ = writer.Write([]byte("ok"))
	}))
	defer server.Close()
	executor := mustHTTPExecutor(t, server.Client(), ClockFunc(time.Now), 1024)
	response, err := executor.Fetch(context.Background(), provider.ResourceRequest{URL: server.URL})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(response.SafeHeaders) != 3 { // Content-Length is added by net/http.
		t.Fatalf("safe headers = %#v", response.SafeHeaders)
	}
	if response.SafeHeaders["Content-Type"][0] != "text/plain" || response.SafeHeaders["Cache-Control"][0] != "max-age=60" {
		t.Fatalf("safe headers = %#v", response.SafeHeaders)
	}
}

func TestHTTPExecutorEnforcesBodyLimit(t *testing.T) {
	tests := []struct {
		name  string
		flush bool
	}{
		{name: "known content length"},
		{name: "unknown content length", flush: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if test.flush {
					writer.(http.Flusher).Flush()
				}
				_, _ = writer.Write([]byte("12345"))
			}))
			defer server.Close()
			executor := mustHTTPExecutor(t, server.Client(), ClockFunc(time.Now), 4)
			_, err := executor.Fetch(context.Background(), provider.ResourceRequest{URL: server.URL})
			if !errors.Is(err, provider.ErrorCodeResponseTooLarge) {
				t.Fatalf("Fetch() error = %v, want response_too_large", err)
			}
		})
	}
}

func TestHTTPExecutorCancelsDuringBodyRead(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.(http.Flusher).Flush()
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()
	executor := mustHTTPExecutor(t, server.Client(), ClockFunc(time.Now), 1024)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := executor.Fetch(ctx, provider.ResourceRequest{URL: server.URL})
		result <- err
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Fetch() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Fetch() did not stop after cancellation")
	}
}

func TestHTTPExecutorRejectsInvalidRequests(t *testing.T) {
	tests := []provider.ResourceRequest{
		{URL: "file:///tmp/item"},
		{URL: "https://user:secret@example.com/item"},
		{URL: "https://example.com/item#fragment"},
		{URL: "https://example.com", Method: http.MethodConnect},
		{URL: "https://example.com", Query: []provider.RequestValue{{Name: "bad&name", Values: []string{"x"}}}},
		{URL: "https://example.com", Headers: []provider.RequestValue{{Name: "Host", Values: []string{"evil.example"}}}},
		{URL: "https://example.com", Headers: []provider.RequestValue{{Name: "X-Test", Values: []string{"ok\r\nbad"}}}},
	}
	executor := mustHTTPExecutor(t, nil, ClockFunc(time.Now), 1024)
	for _, request := range tests {
		if _, err := executor.Fetch(context.Background(), request); !errors.Is(err, provider.ErrorCodeInvalidResourceRequest) {
			t.Errorf("Fetch(%#v) error = %v, want invalid_resource_request", request, err)
		}
	}
}

func mustHTTPExecutor(t *testing.T, client *http.Client, clock Clock, limit int64) *HTTPExecutor {
	t.Helper()
	executor, err := NewHTTPExecutor(client, clock, config.ByteSize(limit))
	if err != nil {
		t.Fatalf("NewHTTPExecutor() error = %v", err)
	}
	return executor
}
