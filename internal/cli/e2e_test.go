package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	coreapp "github.com/kostyay/ecom/internal/app"
	"github.com/kostyay/ecom/internal/testprovider"
	"github.com/kostyay/ecom/provider"
)

const fixtureCatalogURL = "https://fixture.invalid/catalog-version"

func TestCLIEndToEndDiscoveryWorkflow(t *testing.T) {
	environment := newE2EEnvironment(t, "24h")

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "provider help", args: []string{"provider", "help", testprovider.Name}, want: []string{`"name":"test-fixture"`, `"search"`}},
		{name: "search JSON", args: []string{"search", "helmet", "--filter", "in-stock=true", "--sort", "price-asc", "--page-size", "2"}, want: []string{`"schema_version":"1"`, `"trail-helmet"`, `"road-helmet"`}},
		{name: "category list", args: []string{"categories", "--recursive"}, want: []string{`"id":"cycling"`, `"id":"helmets"`}},
		{name: "category search", args: []string{"categories", "helmet"}, want: []string{`"search_method":"provider"`, `"id":"helmets"`}},
		{name: "category items", args: []string{"category-items", "helmets", "--sort", "price-desc"}, want: []string{`"road-helmet"`, `"trail-helmet"`}},
		{name: "brand list", args: []string{"brands"}, want: []string{`"id":"acme"`, `"id":"velo"`}},
		{name: "brand search", args: []string{"brands", "velo"}, want: []string{`"search_method":"provider"`, `"id":"velo"`}},
		{name: "brand items", args: []string{"brand-items", "acme"}, want: []string{`"trail-helmet"`, `"winter-gloves"`}},
		{name: "deals", args: []string{"deals", "--filter", "min-discount=20"}, want: []string{`"original_price"`, `"trail-helmet"`, `"winter-gloves"`}},
		{name: "filters", args: []string{"filters", "deals"}, want: []string{`"filters"`, `"min-discount"`, `"price-asc"`}},
		{name: "item", args: []string{"item", "trail-helmet"}, want: []string{`"detail_level":"full"`, `"variants"`}},
		{name: "variant", args: []string{"item", "trail-helmet", "--variant", "size=M", "--variant", "color=black"}, want: []string{`"selected_variant"`, `"selected":true`}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := environment.run(t, test.args...)
			assertE2ESuccess(t, result)
			for _, want := range test.want {
				if !strings.Contains(result.stdout, want) {
					t.Errorf("stdout does not contain %q: %s", want, result.stdout)
				}
			}
		})
	}

	table := environment.run(t, "search", "helmet", "-o", "table")
	assertE2ESuccess(t, table)
	for _, want := range []string{"Provider:  test-fixture", "Trail Helmet", "Road Helmet", "Cache:"} {
		if !strings.Contains(table.stdout, want) {
			t.Errorf("table output does not contain %q: %s", want, table.stdout)
		}
	}

	jsonPath := environment.run(t, "item", "trail-helmet", "-o", `jsonpath={.data.item.price.display}`)
	assertE2ESuccess(t, jsonPath)
	if jsonPath.stdout != "€79.95" {
		t.Errorf("JSONPath stdout = %q, want %q", jsonPath.stdout, "€79.95")
	}

	invalid := environment.run(t, "search", "helmet", "--filter", "unknown=value")
	if invalid.status != 1 || invalid.stdout != "" {
		t.Fatalf("invalid command = status %d, stdout %q, stderr %q", invalid.status, invalid.stdout, invalid.stderr)
	}
	var failure errorEnvelope
	if err := json.Unmarshal([]byte(invalid.stderr), &failure); err != nil {
		t.Fatalf("decode error envelope: %v; stderr = %q", err, invalid.stderr)
	}
	if failure.Error.Code != string(provider.ErrorCodeInvalidFilter) || !strings.Contains(failure.Error.Message, "unknown") {
		t.Errorf("error = %#v", failure.Error)
	}
}

func TestCLIEndToEndCachePersistenceRefreshAndStaleFallback(t *testing.T) {
	environment := newE2EEnvironment(t, "1s")

	fresh := environment.run(t, "search", "helmet")
	assertE2ESuccess(t, fresh)
	assertE2ECache(t, fresh.stdout, false, false)
	if calls := environment.transport.callCount(); calls != 1 {
		t.Fatalf("fresh resource requests = %d, want 1", calls)
	}

	hit := environment.run(t, "search", "helmet")
	assertE2ESuccess(t, hit)
	assertE2ECache(t, hit.stdout, true, false)
	if calls := environment.transport.callCount(); calls != 1 {
		t.Fatalf("cached resource requests = %d, want 1", calls)
	}

	refreshed := environment.run(t, "search", "helmet", "--refresh")
	assertE2ESuccess(t, refreshed)
	assertE2ECache(t, refreshed.stdout, false, false)
	if calls := environment.transport.callCount(); calls != 2 {
		t.Fatalf("refreshed resource requests = %d, want 2", calls)
	}

	time.Sleep(1100 * time.Millisecond)
	environment.transport.setFailure(errors.New("fixture transport is unavailable"))
	stale := environment.run(t, "search", "helmet", "--stale-if-error")
	assertE2ESuccess(t, stale)
	assertE2ECache(t, stale.stdout, true, true)
	if calls := environment.transport.callCount(); calls != 3 {
		t.Fatalf("stale fallback resource requests = %d, want 3", calls)
	}

	cleared := environment.run(t, "cache", "clear", "--provider", testprovider.Name)
	assertE2ESuccess(t, cleared)
	if !strings.Contains(cleared.stdout, `"entries_deleted":1`) {
		t.Errorf("cache clear did not find the persistent entry: %s", cleared.stdout)
	}
}

type e2eEnvironment struct {
	factory    *coreapp.Factory
	configPath string
	logPath    string
	transport  *fixtureRoundTripper
}

func newE2EEnvironment(t *testing.T, ttl string) *e2eEnvironment {
	t.Helper()
	directory := t.TempDir()
	cachePath := filepath.Join(directory, "cache.db")
	configPath := filepath.Join(directory, "config.yaml")
	logPath := filepath.Join(directory, "ecom.log")
	configuration := "provider: " + testprovider.Name + "\n" +
		"cache:\n  path: " + cachePath + "\n  ttl: " + ttl + "\n" +
		"network:\n  requests_per_second: 1000\n  max_concurrent_http: 1\n  max_concurrent_browser: 1\n  retries: 0\n"
	if err := os.WriteFile(configPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}

	registry := provider.NewRegistry()
	if err := registry.Register(testprovider.Registration()); err != nil {
		t.Fatal(err)
	}
	transport := &fixtureRoundTripper{}
	client := http.DefaultClient
	previousTransport := client.Transport
	client.Transport = transport
	t.Cleanup(func() { client.Transport = previousTransport })

	return &e2eEnvironment{
		factory: coreapp.NewFactory(registry.Resolve), configPath: configPath,
		logPath: logPath, transport: transport,
	}
}

func (environment *e2eEnvironment) run(t *testing.T, args ...string) commandResult {
	t.Helper()
	arguments := append([]string(nil), args...)
	arguments = append(arguments, "--config", environment.configPath, "--log-file", environment.logPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	status := run(t.Context(), arguments, &stdout, &stderr, environment.factory)
	return commandResult{status: status, stdout: stdout.String(), stderr: stderr.String()}
}

func assertE2ESuccess(t *testing.T, result commandResult) {
	t.Helper()
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("command = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
}

func assertE2ECache(t *testing.T, raw string, hit, stale bool) {
	t.Helper()
	var envelope struct {
		Cache struct {
			Hit           bool `json:"hit"`
			Stale         bool `json:"stale"`
			ResourceCount int  `json:"resource_count"`
			HitCount      int  `json:"hit_count"`
			StaleCount    int  `json:"stale_count"`
		} `json:"cache"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("decode output envelope: %v; stdout = %q", err, raw)
	}
	if envelope.Cache.Hit != hit || envelope.Cache.Stale != stale || envelope.Cache.ResourceCount != 1 {
		t.Errorf("cache metadata = %#v, want hit=%t stale=%t resources=1", envelope.Cache, hit, stale)
	}
	if hit && envelope.Cache.HitCount != 1 {
		t.Errorf("cache hit count = %d, want 1", envelope.Cache.HitCount)
	}
	if stale && envelope.Cache.StaleCount != 1 {
		t.Errorf("cache stale count = %d, want 1", envelope.Cache.StaleCount)
	}
}

type fixtureRoundTripper struct {
	mu      sync.Mutex
	calls   int
	failure error
}

func (transport *fixtureRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.Lock()
	transport.calls++
	failure := transport.failure
	transport.mu.Unlock()
	if request.URL.String() != fixtureCatalogURL {
		return nil, errors.New("unexpected fixture resource URL")
	}
	if failure != nil {
		return nil, failure
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("fixture-catalog-v1")),
		Request:    request,
	}, nil
}

func (transport *fixtureRoundTripper) callCount() int {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.calls
}

func (transport *fixtureRoundTripper) setFailure(err error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.failure = err
}
