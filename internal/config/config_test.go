package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func TestLoadDefaults(t *testing.T) {
	settings := loadForTest(t, "", nil)

	if settings.Provider != defaultProvider {
		t.Errorf("provider = %q, want %q", settings.Provider, defaultProvider)
	}
	if settings.Market != (MarketSettings{Country: "DE", Language: "en", Currency: "EUR"}) {
		t.Errorf("market = %#v", settings.Market)
	}
	if settings.Pricing != (PricingSettings{IncludeShipping: false}) {
		t.Errorf("pricing = %#v", settings.Pricing)
	}
	if settings.Cache != (CacheSettings{
		TTL:             24 * time.Hour,
		MaxSize:         512 * mebibyte,
		MaxResponseSize: 20 * mebibyte,
	}) {
		t.Errorf("cache = %#v", settings.Cache)
	}
	if settings.Network != (NetworkSettings{
		RequestsPerSecond:    1,
		MaxConcurrentHTTP:    2,
		MaxConcurrentBrowser: 1,
		Retries:              3,
	}) {
		t.Errorf("network = %#v", settings.Network)
	}
	if settings.Browser != (BrowserSettings{InteractiveTimeout: 5 * time.Minute}) {
		t.Errorf("browser = %#v", settings.Browser)
	}
	if settings.Log != (LogSettings{Level: "info"}) {
		t.Errorf("log = %#v", settings.Log)
	}
	if settings.Providers["bike-discount"]["page_size"] != 48 {
		t.Errorf("bike-discount settings = %#v", settings.Providers["bike-discount"])
	}
}

func TestLoadYAML(t *testing.T) {
	configFile := writeConfig(t, `
provider: example
market:
  country: FR
  language: fr
  currency: CHF
pricing:
  include_shipping: true
cache:
  path: /tmp/example.db
  ttl: 30m
  max_size: 1GiB
  max_response_size: 4MiB
network:
  requests_per_second: 2.5
  max_concurrent_http: 4
  max_concurrent_browser: 2
  retries: 5
browser:
  cdp_address: http://127.0.0.1:9222
  headed: true
  interactive_timeout: 8m
providers:
  example:
    page_size: 48
    nested:
      token: opaque
log:
  level: debug
  file: /tmp/ecom.log
`)

	settings := loadForTest(t, configFile, nil)

	if settings.Provider != "example" || settings.Market.Country != "FR" || settings.Market.Language != "fr" || settings.Market.Currency != "CHF" {
		t.Errorf("provider and market = %q, %#v", settings.Provider, settings.Market)
	}
	if !settings.Pricing.IncludeShipping {
		t.Errorf("pricing = %#v", settings.Pricing)
	}
	if settings.Cache.Path != "/tmp/example.db" || settings.Cache.TTL != 30*time.Minute || settings.Cache.MaxSize != 1024*mebibyte || settings.Cache.MaxResponseSize != 4*mebibyte {
		t.Errorf("cache = %#v", settings.Cache)
	}
	if settings.Network.RequestsPerSecond != 2.5 || settings.Network.MaxConcurrentHTTP != 4 || settings.Network.MaxConcurrentBrowser != 2 || settings.Network.Retries != 5 {
		t.Errorf("network = %#v", settings.Network)
	}
	if settings.Browser.CDPAddress != "http://127.0.0.1:9222" || !settings.Browser.Headed || settings.Browser.InteractiveTimeout != 8*time.Minute {
		t.Errorf("browser = %#v", settings.Browser)
	}
	if settings.Log != (LogSettings{Level: "debug", File: "/tmp/ecom.log"}) {
		t.Errorf("log = %#v", settings.Log)
	}
	provider := settings.Providers["example"]
	if provider["page_size"] != 48 || provider["nested"] == nil {
		t.Errorf("provider data = %#v", provider)
	}
}

func TestLoadEnvironmentOverridesFile(t *testing.T) {
	configFile := writeConfig(t, `
provider: from-file
market:
  country: FR
pricing:
  include_shipping: false
cache:
  ttl: 1h
network:
  retries: 2
browser:
  headed: false
log:
  level: warn
`)
	t.Setenv("ECOM_PROVIDER", "from-environment")
	t.Setenv("ECOM_MARKET_COUNTRY", "ES")
	t.Setenv("ECOM_MARKET_LANGUAGE", "es")
	t.Setenv("ECOM_MARKET_CURRENCY", "GBP")
	t.Setenv("ECOM_PRICING_INCLUDE_SHIPPING", "true")
	t.Setenv("ECOM_CACHE_PATH", "/tmp/environment.db")
	t.Setenv("ECOM_CACHE_TTL", "2h")
	t.Setenv("ECOM_CACHE_MAX_SIZE", "2GiB")
	t.Setenv("ECOM_CACHE_MAX_RESPONSE_SIZE", "8MiB")
	t.Setenv("ECOM_NETWORK_REQUESTS_PER_SECOND", "4.5")
	t.Setenv("ECOM_NETWORK_MAX_CONCURRENT_HTTP", "6")
	t.Setenv("ECOM_NETWORK_MAX_CONCURRENT_BROWSER", "3")
	t.Setenv("ECOM_NETWORK_RETRIES", "7")
	t.Setenv("ECOM_BROWSER_CDP_ADDRESS", "http://127.0.0.1:9333")
	t.Setenv("ECOM_BROWSER_HEADED", "true")
	t.Setenv("ECOM_LOG_LEVEL", "error")

	settings := loadForTest(t, configFile, nil)

	if settings.Provider != "from-environment" || settings.Market != (MarketSettings{Country: "ES", Language: "es", Currency: "GBP"}) {
		t.Errorf("provider and market = %q, %#v", settings.Provider, settings.Market)
	}
	if !settings.Pricing.IncludeShipping {
		t.Errorf("pricing = %#v", settings.Pricing)
	}
	if settings.Cache != (CacheSettings{Path: "/tmp/environment.db", TTL: 2 * time.Hour, MaxSize: 2 * 1024 * mebibyte, MaxResponseSize: 8 * mebibyte}) {
		t.Errorf("cache = %#v", settings.Cache)
	}
	if settings.Network != (NetworkSettings{RequestsPerSecond: 4.5, MaxConcurrentHTTP: 6, MaxConcurrentBrowser: 3, Retries: 7}) {
		t.Errorf("network = %#v", settings.Network)
	}
	if settings.Browser != (BrowserSettings{CDPAddress: "http://127.0.0.1:9333", Headed: true, InteractiveTimeout: 5 * time.Minute}) {
		t.Errorf("environment settings = %#v, %#v, %#v", settings.Cache, settings.Network, settings.Browser)
	}
	if settings.Log.Level != "error" {
		t.Errorf("log level = %q", settings.Log.Level)
	}
}

func TestLoadBoundFlagsOverrideEnvironmentAndFile(t *testing.T) {
	configFile := writeConfig(t, "provider: from-file\ncache:\n  ttl: 1h\n")
	t.Setenv("ECOM_PROVIDER", "from-environment")
	t.Setenv("ECOM_CACHE_TTL", "2h")

	v := viper.New()
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("provider", "", "")
	flags.Duration("cache-ttl", 0, "")
	if err := flags.Set("provider", "from-flag"); err != nil {
		t.Fatal(err)
	}
	if err := flags.Set("cache-ttl", "3h"); err != nil {
		t.Fatal(err)
	}
	if err := v.BindPFlag("provider", flags.Lookup("provider")); err != nil {
		t.Fatal(err)
	}
	if err := v.BindPFlag("cache.ttl", flags.Lookup("cache-ttl")); err != nil {
		t.Fatal(err)
	}

	settings := loadForTest(t, configFile, v)
	if settings.Provider != "from-flag" || settings.Cache.TTL != 3*time.Hour {
		t.Errorf("provider and ttl = %q, %s", settings.Provider, settings.Cache.TTL)
	}
}

func TestLoadRejectsUnknownCoreKey(t *testing.T) {
	configFile := writeConfig(t, "unknown: true\n")
	_, err := Load(viper.New(), configFile)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("Load error = %v, want unknown key error", err)
	}
}

func TestLoadAcceptsUnknownProviderKeys(t *testing.T) {
	configFile := writeConfig(t, `
providers:
  bike-discount:
    future_option: enabled
    arbitrary_list: [one, two]
`)
	settings := loadForTest(t, configFile, nil)
	provider := settings.Providers["bike-discount"]
	if provider["future_option"] != "enabled" || provider["arbitrary_list"] == nil {
		t.Errorf("provider data = %#v", provider)
	}
}

func TestLoadRejectsInvalidByteSize(t *testing.T) {
	configFile := writeConfig(t, "cache:\n  max_size: 512MB\n")
	_, err := Load(viper.New(), configFile)
	if err == nil || !strings.Contains(err.Error(), "invalid byte size") {
		t.Fatalf("Load error = %v, want invalid byte size error", err)
	}
}

func TestLoadRejectsInvalidInteractiveTimeout(t *testing.T) {
	configFile := writeConfig(t, "browser:\n  interactive_timeout: 0s\n")
	_, err := Load(viper.New(), configFile)
	if err == nil || !strings.Contains(err.Error(), "interactive timeout") {
		t.Fatalf("Load error = %v, want interactive timeout error", err)
	}
}

func loadForTest(t *testing.T, configFile string, v *viper.Viper) Settings {
	t.Helper()
	if v == nil {
		v = viper.New()
	}
	if configFile == "" {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	}
	settings, err := Load(v, configFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return settings
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
