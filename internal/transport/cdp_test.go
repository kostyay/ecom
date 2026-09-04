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
	"github.com/kostyay/ecom/provider"
)

type fakeCDPConnector struct {
	address    string
	connection *fakeCDPConnection
	err        error
	connects   int
}

func (connector *fakeCDPConnector) Connect(_ context.Context, address string) (CDPConnection, error) {
	connector.connects++
	connector.address = address
	return connector.connection, connector.err
}

type fakeCDPConnection struct {
	target      *fakeCDPTarget
	newErr      error
	closeErr    error
	newTargets  int
	disconnects int
}

func (connection *fakeCDPConnection) NewTarget(context.Context) (CDPTarget, error) {
	connection.newTargets++
	return connection.target, connection.newErr
}

func (connection *fakeCDPConnection) Close() error {
	connection.disconnects++
	return connection.closeErr
}

type fakeCDPTarget struct {
	command   BrowserCommand
	result    BrowserResult
	err       error
	wait      bool
	called    chan struct{}
	navigates int
	closed    int
	closeErr  error
}

func (target *fakeCDPTarget) Navigate(ctx context.Context, command BrowserCommand) (BrowserResult, error) {
	target.navigates++
	target.command = command
	if target.called != nil {
		close(target.called)
	}
	if target.wait {
		<-ctx.Done()
		return BrowserResult{}, ctx.Err()
	}
	return target.result, target.err
}

func (target *fakeCDPTarget) Close() error {
	target.closed++
	return target.closeErr
}

type fakeCDPPermits struct {
	providerName string
	mode         provider.TransportMode
	acquires     int
	releases     int
	err          error
}

func (permits *fakeCDPPermits) Acquire(_ context.Context, providerName string, mode provider.TransportMode) (*RequestPermit, error) {
	permits.acquires++
	permits.providerName = providerName
	permits.mode = mode
	if permits.err != nil {
		return nil, permits.err
	}
	return &RequestPermit{release: func() { permits.releases++ }}, nil
}

func TestCDPResourceServiceReturnsClosedPageSnapshot(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	target := &fakeCDPTarget{result: BrowserResult{
		HTML:       []byte("<html><body>Helmet</body></html>"),
		DOM:        map[string][]string{"names": {"Helmet"}},
		FinalURL:   "https://shop.example/search?q=helmet&token=private",
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": {"text/html"}, "Set-Cookie": {"secret=yes"}},
	}}
	connection := &fakeCDPConnection{target: target}
	connector := &fakeCDPConnector{connection: connection}
	permits := &fakeCDPPermits{}
	service := newTestCDPService(t, "http://127.0.0.1:9222", connector, permits, now)
	request := provider.ResourceRequest{
		URL:       "https://shop.example/search?q=helmet",
		Query:     []provider.RequestValue{{Name: "token", Values: []string{"private"}, Sensitive: true}},
		DOM:       []provider.DOMExtraction{{Name: "names", Selector: ".name", Kind: provider.DOMText, All: true}},
		Transport: provider.TransportPolicy{Required: provider.TransportCDP},
	}

	response, err := service.Fetch(t.Context(), request)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if connector.address != "http://127.0.0.1:9222" || connector.connects != 1 {
		t.Fatalf("connector address/calls = %q/%d", connector.address, connector.connects)
	}
	if response.Transport != provider.TransportCDP || response.FinalURL != "https://shop.example/search?q=helmet&token=%5BREDACTED%5D" || !response.RetrievedAt.Equal(now) {
		t.Fatalf("Fetch() metadata = %#v", response)
	}
	if response.Page == nil || string(response.Page.HTML) != "<html><body>Helmet</body></html>" || !reflect.DeepEqual(response.Page.DOM, map[string][]string{"names": {"Helmet"}}) {
		t.Fatalf("Fetch() page = %#v", response.Page)
	}
	if _, found := response.SafeHeaders["Set-Cookie"]; found {
		t.Fatalf("Fetch() safe headers = %#v", response.SafeHeaders)
	}
	if target.command.Headed || len(target.command.State.Cookies) != 0 || len(target.command.State.Origins) != 0 {
		t.Fatalf("CDP command imported state or headed mode: %#v", target.command)
	}
	if connection.newTargets != 1 || target.closed != 1 || connection.disconnects != 1 {
		t.Fatalf("target lifecycle = new %d, target close %d, disconnect %d", connection.newTargets, target.closed, connection.disconnects)
	}
	if permits.acquires != 1 || permits.releases != 1 || permits.providerName != "bike-discount" || permits.mode != provider.TransportCDP {
		t.Fatalf("permit lifecycle = %#v", permits)
	}
}

func TestCDPResourceServiceReportsMissingConfigurationForFallback(t *testing.T) {
	connector := &fakeCDPConnector{}
	permits := &fakeCDPPermits{}
	service := newTestCDPService(t, "", connector, permits, time.Now())

	_, err := service.Fetch(t.Context(), provider.ResourceRequest{URL: "https://shop.example"})
	if !errors.Is(err, provider.ErrorCodeTransportUnavailable) || connector.connects != 0 || permits.acquires != 0 {
		t.Fatalf("Fetch() = %v, connector calls %d, permit calls %d", err, connector.connects, permits.acquires)
	}
}

func TestCDPResourceServiceSafelyReportsLifecycleFailures(t *testing.T) {
	tests := []struct {
		name       string
		connector  *fakeCDPConnector
		wantTarget bool
	}{
		{name: "connect", connector: &fakeCDPConnector{err: errors.New("ws://secret-address")}},
		{name: "target", connector: &fakeCDPConnector{connection: &fakeCDPConnection{newErr: errors.New("private target detail")}}},
		{name: "navigate", connector: &fakeCDPConnector{connection: &fakeCDPConnection{target: &fakeCDPTarget{err: errors.New("cookie=private")}}}, wantTarget: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			permits := &fakeCDPPermits{}
			service := newTestCDPService(t, "ws://configured-private-address", test.connector, permits, time.Now())
			_, err := service.Fetch(t.Context(), provider.ResourceRequest{URL: "https://shop.example"})
			if !errors.Is(err, provider.ErrorCodeBrowserFailure) || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("Fetch() error = %v", err)
			}
			if permits.releases != 1 {
				t.Fatalf("permit releases = %d", permits.releases)
			}
			if test.connector.connection != nil && test.connector.connection.disconnects != 1 {
				t.Fatalf("disconnects = %d", test.connector.connection.disconnects)
			}
			if test.wantTarget && test.connector.connection.target.closed != 1 {
				t.Fatalf("target closes = %d", test.connector.connection.target.closed)
			}
		})
	}
}

func TestCDPResourceServicePreservesCancellationAndCleansUpNewTarget(t *testing.T) {
	target := &fakeCDPTarget{wait: true, called: make(chan struct{})}
	connection := &fakeCDPConnection{target: target}
	permits := &fakeCDPPermits{}
	service := newTestCDPService(t, "http://127.0.0.1:9222", &fakeCDPConnector{connection: connection}, permits, time.Now())
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := service.Fetch(ctx, provider.ResourceRequest{URL: "https://shop.example"})
		done <- err
	}()
	<-target.called
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch() error = %v", err)
	}
	if target.closed != 1 || connection.disconnects != 1 || permits.releases != 1 {
		t.Fatalf("cleanup = target %d, disconnect %d, permit %d", target.closed, connection.disconnects, permits.releases)
	}
}

func TestCDPResourceServiceClassifiesChallengesAndBlocks(t *testing.T) {
	tests := []struct {
		name string
		body string
		code provider.ErrorCode
	}{
		{name: "challenge", body: `<title>Just a moment...</title><script src="/cdn-cgi/challenge-platform/x"></script>`, code: provider.ErrorCodeBrowserChallengeRequired},
		{name: "block", body: `<title>Access Denied</title><p>Request blocked</p>`, code: provider.ErrorCodeAccessBlocked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := &fakeCDPTarget{result: BrowserResult{
				HTML: []byte(test.body), FinalURL: "https://shop.example", StatusCode: http.StatusForbidden,
				Headers: http.Header{"Content-Type": {"text/html"}},
			}}
			service := newTestCDPService(t, "http://127.0.0.1:9222", &fakeCDPConnector{connection: &fakeCDPConnection{target: target}}, &fakeCDPPermits{}, time.Now())
			response, err := service.Fetch(t.Context(), provider.ResourceRequest{URL: "https://shop.example"})
			if !errors.Is(err, test.code) || response.Page == nil || response.Transport != provider.TransportCDP {
				t.Fatalf("Fetch() = %#v, %v; want %s", response, err, test.code)
			}
		})
	}
}

func TestCDPResourceServiceReportsPermitFailure(t *testing.T) {
	service := newTestCDPService(t, "http://127.0.0.1:9222", &fakeCDPConnector{}, &fakeCDPPermits{err: errors.New("internal limit detail")}, time.Now())
	_, err := service.Fetch(t.Context(), provider.ResourceRequest{URL: "https://shop.example"})
	if !errors.Is(err, provider.ErrorCodeBrowserFailure) || strings.Contains(err.Error(), "internal") {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func TestConfiguredCDPResourceServiceUsesSettingsWithoutSessionStorage(t *testing.T) {
	service, err := NewConfiguredCDPResourceService("bike-discount", config.Settings{
		Cache:   config.CacheSettings{MaxResponseSize: 2048},
		Network: config.NetworkSettings{RequestsPerSecond: 1, MaxConcurrentHTTP: 2, MaxConcurrentBrowser: 1},
		Browser: config.BrowserSettings{CDPAddress: "http://127.0.0.1:9222"},
	})
	if err != nil {
		t.Fatalf("NewConfiguredCDPResourceService() error = %v", err)
	}
	if service.address != "http://127.0.0.1:9222" || service.executor.maxResponseSize != 2048 {
		t.Fatalf("configured service = %#v", service)
	}
}

func newTestCDPService(t *testing.T, address string, connector CDPConnector, permits permitAcquirer, now time.Time) *CDPResourceService {
	t.Helper()
	service, err := NewCDPResourceService("bike-discount", address, connector, permits, ClockFunc(func() time.Time { return now }), 4096)
	if err != nil {
		t.Fatalf("NewCDPResourceService() error = %v", err)
	}
	return service
}

var _ CDPConnector = (*fakeCDPConnector)(nil)
var _ CDPConnection = (*fakeCDPConnection)(nil)
var _ CDPTarget = (*fakeCDPTarget)(nil)
