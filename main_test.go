package main

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kostyay/ecom/internal/cli"
	"github.com/kostyay/ecom/provider"
)

func TestBuscoCotxeCLI(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.yaml")
	configuration := "provider: buscocotxe\n" +
		"market:\n  country: AD\n  language: ca\n  currency: EUR\n" +
		"cache:\n  path: " + filepath.Join(directory, "cache.db") + "\n" +
		"network:\n  requests_per_second: 1000\n  max_concurrent_http: 1\n  max_concurrent_browser: 1\n  retries: 0\n"
	if err := os.WriteFile(configPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}

	transport := &buscoCotxeRoundTripper{t: t}
	client := http.DefaultClient
	previousTransport := client.Transport
	client.Transport = transport
	t.Cleanup(func() { client.Transport = previousTransport })

	run := func(args ...string) commandResult {
		t.Helper()
		args = append(args, "--config", configPath, "--log-file", filepath.Join(directory, "ecom.log"))
		var stdout, stderr bytes.Buffer
		status := cli.Run(t.Context(), args, &stdout, &stderr)
		return commandResult{status: status, stdout: stdout.String(), stderr: stderr.String()}
	}
	checkSuccess := func(result commandResult, contains ...string) {
		t.Helper()
		if result.status != 0 || result.stderr != "" {
			t.Fatalf("status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
		}
		for _, want := range contains {
			if !strings.Contains(result.stdout, want) {
				t.Errorf("stdout does not contain %q: %s", want, result.stdout)
			}
		}
	}

	checkSuccess(run("provider", "help", "buscocotxe"), `"name":"buscocotxe"`, `"default_page_size":30`)
	checkSuccess(run("search", "BMW", "--page", "1", "--page-size", "30"), `"provider":"buscocotxe"`, `"id":"605610"`, `"number":1`)
	checkSuccess(run("search", "BMW", "--page", "1", "--page-size", "30", "-o", "table"), "Provider:  buscocotxe", "BMW X1 xDrive20i", "page 1, size 30")
	checkSuccess(run("search", "BMW", "--page", "1", "--page-size", "30", "-o", `jsonpath={.data.items[0].price.display}`), "18.900 €")
	checkSuccess(run("search", "BMW", "--page", "2", "--page-size", "30"), `"id":"603213"`, `"number":2`)

	calls := transport.calls.Load()
	invalid := run("search", "BMW", "--filter", "price=1000")
	if invalid.status != 1 || invalid.stdout != "" || transport.calls.Load() != calls {
		t.Fatalf("invalid filter result: status %d, stdout %q, stderr %q, resource calls %d", invalid.status, invalid.stdout, invalid.stderr, transport.calls.Load()-calls)
	}
	var failure struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(invalid.stderr), &failure); err != nil {
		t.Fatalf("decode error: %v; stderr %q", err, invalid.stderr)
	}
	if failure.Error.Code != string(provider.ErrorCodeInvalidFilter) {
		t.Errorf("error code = %q, want %q", failure.Error.Code, provider.ErrorCodeInvalidFilter)
	}
}

type commandResult struct {
	status         int
	stdout, stderr string
}

func TestTradeinnCLI(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("ECOM_CACHE_PATH", filepath.Join(directory, "cache.db"))
	t.Setenv("ECOM_MARKET_COUNTRY", "DE")
	t.Setenv("ECOM_MARKET_LANGUAGE", "en")
	t.Setenv("ECOM_MARKET_CURRENCY", "EUR")
	transport := &tradeinnRoundTripper{t: t}
	previousTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = transport
	t.Cleanup(func() { http.DefaultClient.Transport = previousTransport })
	run := func(args ...string) string {
		t.Helper()
		args = append(args, "--provider", "tradeinn", "--log-file", filepath.Join(directory, "ecom.log"))
		var stdout, stderr bytes.Buffer
		if status := cli.Run(t.Context(), args, &stdout, &stderr); status != 0 || stderr.Len() != 0 {
			t.Fatalf("status %d, stdout %q, stderr %q", status, stdout.String(), stderr.String())
		}
		return stdout.String()
	}
	if output := run("provider", "help", "tradeinn"); !strings.Contains(output, `"default_page_size":45`) {
		t.Fatal(output)
	}
	if output := run("search", "powertube"); !strings.Contains(output, `"amount":"739.99"`) || !strings.Contains(output, `"total_items":48`) {
		t.Fatal(output)
	}
	if output := run("search", "powertube", "-o", `jsonpath={.data.items[0].price.display}`); !strings.Contains(output, "739.99 €") {
		t.Fatal(output)
	}
	if transport.calls.Load() != 1 {
		t.Fatalf("cached search made %d requests", transport.calls.Load())
	}
	if output := run("search", "powertube", "--page", "2", "-o", "table"); !strings.Contains(output, "page 2, size 45") {
		t.Fatal(output)
	}
	if transport.calls.Load() != 2 {
		t.Fatalf("pagination made %d requests", transport.calls.Load())
	}
	t.Setenv("ECOM_MARKET_COUNTRY", "AD")
	if output := run("search", "powertube"); !strings.Contains(output, `"amount":"675.99"`) {
		t.Fatal(output)
	}
	if transport.calls.Load() != 3 {
		t.Fatalf("market change made %d requests", transport.calls.Load())
	}
}

type tradeinnRoundTripper struct {
	t     *testing.T
	calls atomic.Int64
}

func (transport *tradeinnRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls.Add(1)
	if err := request.ParseForm(); err != nil {
		return nil, err
	}
	if request.Method != http.MethodPost || request.URL.String() != "https://www.tradeinn.com/listado.php" || request.PostForm.Get("palabras") != "powertube" {
		return nil, errors.New("unexpected Tradeinn request")
	}
	fixture := "search.json"
	switch request.PostForm.Get("nextToken") {
	case "null":
	case "fixture-page-2":
		fixture = "last.json"
	default:
		return nil, errors.New("unexpected Tradeinn token")
	}
	body, err := os.ReadFile(filepath.Join("providers", "tradeinn", "testdata", fixture))
	if err != nil {
		transport.t.Error(err)
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
}

type buscoCotxeRoundTripper struct {
	t     *testing.T
	calls atomic.Int64
}

func (transport *buscoCotxeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls.Add(1)

	query := request.URL.Query()
	page := query.Get("pn")
	if request.Method != http.MethodGet || request.URL.Host != "www.buscocotxe.ad" || request.URL.Path != "/ca/search" || query.Get("search") != "BMW" || page != "1" && page != "2" {
		return nil, errors.New("unexpected BuscoCotxe request")
	}
	body, err := os.ReadFile(filepath.Join("providers", "buscocotxe", "testdata", "search_page_"+page+".html"))
	if err != nil {
		transport.t.Error(err)
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    request,
	}, nil
}
