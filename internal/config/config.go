// Package config loads ecom configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

const (
	configDirName = "ecom"

	defaultProvider        = "bike-discount"
	defaultCountry         = "DE"
	defaultLanguage        = "en"
	defaultCurrency        = "EUR"
	defaultCacheTTL        = 24 * time.Hour
	defaultCacheMaxSize    = ByteSize(512 * mebibyte)
	defaultMaxResponseSize = ByteSize(20 * mebibyte)
)

const mebibyte = 1024 * 1024

// Settings contains all application settings.
type Settings struct {
	Provider  string                            `mapstructure:"provider"`
	Market    MarketSettings                    `mapstructure:"market"`
	Pricing   PricingSettings                   `mapstructure:"pricing"`
	Cache     CacheSettings                     `mapstructure:"cache"`
	Network   NetworkSettings                   `mapstructure:"network"`
	Browser   BrowserSettings                   `mapstructure:"browser"`
	Providers map[string]map[string]interface{} `mapstructure:"providers"`
	Log       LogSettings                       `mapstructure:"log"`
}

// PricingSettings controls which displayed price a provider must return.
type PricingSettings struct {
	IncludeShipping bool `mapstructure:"include_shipping"`
}

// MarketSettings selects the provider market.
type MarketSettings struct {
	Country  string `mapstructure:"country"`
	Language string `mapstructure:"language"`
	Currency string `mapstructure:"currency"`
}

// CacheSettings controls raw-response storage.
type CacheSettings struct {
	Path            string        `mapstructure:"path"`
	TTL             time.Duration `mapstructure:"ttl"`
	MaxSize         ByteSize      `mapstructure:"max_size"`
	MaxResponseSize ByteSize      `mapstructure:"max_response_size"`
}

// NetworkSettings controls requests to each provider.
type NetworkSettings struct {
	RequestsPerSecond    float64 `mapstructure:"requests_per_second"`
	MaxConcurrentHTTP    int     `mapstructure:"max_concurrent_http"`
	MaxConcurrentBrowser int     `mapstructure:"max_concurrent_browser"`
	Retries              int     `mapstructure:"retries"`
}

// BrowserSettings controls browser connections.
type BrowserSettings struct {
	CDPAddress         string        `mapstructure:"cdp_address"`
	Headed             bool          `mapstructure:"headed"`
	InteractiveTimeout time.Duration `mapstructure:"interactive_timeout"`
}

// LogSettings contains structured logging settings.
type LogSettings struct {
	Level string `mapstructure:"level"`
	File  string `mapstructure:"file"`
}

// ByteSize is a number of bytes read from an IEC byte-size string.
type ByteSize int64

// String returns the byte count.
func (size ByteSize) String() string {
	return strconv.FormatInt(int64(size), 10)
}

// Load reads configuration with flag, environment, and file precedence.
func Load(v *viper.Viper, configFile string) (Settings, error) {
	v.SetEnvPrefix("ECOM")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	setDefaults(v)

	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return Settings{}, fmt.Errorf("find user config directory: %w", err)
		}
		v.AddConfigPath(filepath.Join(configDir, configDirName))
		v.SetConfigName("config")
		v.SetConfigType("yaml")
	}

	if err := v.ReadInConfig(); err != nil {
		_, missing := errors.AsType[viper.ConfigFileNotFoundError](err)
		if configFile != "" || !missing {
			return Settings{}, fmt.Errorf("read configuration: %w", err)
		}
	}

	var settings Settings
	decodeHook := mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		byteSizeDecodeHook(),
	)
	if err := v.UnmarshalExact(&settings, viper.DecodeHook(decodeHook)); err != nil {
		return Settings{}, fmt.Errorf("decode configuration: %w", err)
	}
	if err := settings.validate(); err != nil {
		return Settings{}, fmt.Errorf("validate configuration: %w", err)
	}

	return settings, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("provider", defaultProvider)
	v.SetDefault("market.country", defaultCountry)
	v.SetDefault("market.language", defaultLanguage)
	v.SetDefault("market.currency", defaultCurrency)
	v.SetDefault("pricing.include_shipping", false)
	v.SetDefault("cache.path", "")
	v.SetDefault("cache.ttl", defaultCacheTTL)
	v.SetDefault("cache.max_size", defaultCacheMaxSize)
	v.SetDefault("cache.max_response_size", defaultMaxResponseSize)
	v.SetDefault("network.requests_per_second", 1.0)
	v.SetDefault("network.max_concurrent_http", 2)
	v.SetDefault("network.max_concurrent_browser", 1)
	v.SetDefault("network.retries", 3)
	v.SetDefault("browser.cdp_address", "")
	v.SetDefault("browser.headed", false)
	v.SetDefault("browser.interactive_timeout", 5*time.Minute)
	v.SetDefault("providers", map[string]interface{}{})
	v.SetDefault("providers.bike-discount.page_size", 48)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.file", "")
}

func byteSizeDecodeHook() mapstructure.DecodeHookFuncType {
	byteSizeType := reflect.TypeFor[ByteSize]()
	return func(from, to reflect.Type, data interface{}) (interface{}, error) {
		if from.Kind() != reflect.String || to != byteSizeType {
			return data, nil
		}
		return parseByteSize(data.(string))
	}
}

func parseByteSize(value string) (ByteSize, error) {
	units := []struct {
		suffix     string
		multiplier int64
	}{
		{suffix: "GiB", multiplier: 1024 * mebibyte},
		{suffix: "MiB", multiplier: mebibyte},
		{suffix: "KiB", multiplier: 1024},
		{suffix: "B", multiplier: 1},
	}

	for _, unit := range units {
		number, found := strings.CutSuffix(value, unit.suffix)
		if !found {
			continue
		}
		parsed, err := strconv.ParseInt(number, 10, 64)
		if err != nil || parsed < 0 || parsed > int64(^uint64(0)>>1)/unit.multiplier {
			return 0, fmt.Errorf("invalid byte size %q", value)
		}
		return ByteSize(parsed * unit.multiplier), nil
	}

	return 0, fmt.Errorf("invalid byte size %q (use B, KiB, MiB, or GiB)", value)
}

func (settings Settings) validate() error {
	if settings.Market.Country == "" || settings.Market.Language == "" || settings.Market.Currency == "" {
		return errors.New("market country, language, and currency must not be empty")
	}
	if settings.Cache.TTL <= 0 || settings.Cache.MaxSize <= 0 || settings.Cache.MaxResponseSize <= 0 {
		return errors.New("cache ttl and size limits must be positive")
	}
	if settings.Network.RequestsPerSecond <= 0 || settings.Network.MaxConcurrentHTTP <= 0 || settings.Network.MaxConcurrentBrowser <= 0 {
		return errors.New("network rate and concurrency limits must be positive")
	}
	if settings.Network.Retries < 0 {
		return errors.New("network retries must not be negative")
	}
	if settings.Browser.InteractiveTimeout <= 0 {
		return errors.New("browser interactive timeout must be positive")
	}
	return nil
}
