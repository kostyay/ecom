package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kostyay/ecom/internal/cache"
	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/internal/storage/sqlite"
	"github.com/kostyay/ecom/provider"
)

// CachedHTTPService composes raw-response caching with direct HTTP retries.
// The retry executor, and thus every request permit, runs only on a cache miss
// or an explicit refresh.
type CachedHTTPService struct {
	providerName string
	cache        *cache.Service
	http         provider.ResourceService
	ttl          time.Duration
}

// NewCachedHTTPService creates a cache-aware direct HTTP resource service.
// HTTP must already include the configured retry and request-limit policy.
func NewCachedHTTPService(providerName string, cacheService *cache.Service, httpService provider.ResourceService, ttl time.Duration) (*CachedHTTPService, error) {
	if strings.TrimSpace(providerName) == "" || providerName != strings.TrimSpace(providerName) {
		return nil, errors.New("cached HTTP provider is required")
	}
	if cacheService == nil {
		return nil, errors.New("cached HTTP cache service is required")
	}
	if httpService == nil {
		return nil, errors.New("cached HTTP retry service is required")
	}
	if ttl <= 0 {
		return nil, errors.New("cached HTTP TTL must be positive")
	}
	return &CachedHTTPService{providerName: providerName, cache: cacheService, http: httpService, ttl: ttl}, nil
}

// NewConfiguredHTTPResourceService wires the production SQLite cache, direct
// HTTP executor, provider request limits, and retry policy. The caller owns and
// closes database.
func NewConfiguredHTTPResourceService(database *sqlite.Database, client *http.Client, providerName string, settings config.Settings) (*CachedHTTPService, error) {
	scheduler := RealWaitScheduler{}
	limits, err := NewRequestLimitManager(RequestLimitsFromConfig(settings.Network), nil, scheduler)
	if err != nil {
		return nil, err
	}
	return newConfiguredHTTPResourceService(database, client, providerName, settings, limits, scheduler)
}

func newConfiguredHTTPResourceService(database *sqlite.Database, client *http.Client, providerName string, settings config.Settings, limits *RequestLimitManager, scheduler WaitScheduler) (*CachedHTTPService, error) {
	repository, err := sqlite.NewRawResponseRepository(database, settings.Cache.MaxResponseSize)
	if err != nil {
		return nil, fmt.Errorf("create HTTP cache repository: %w", err)
	}
	cacheService, err := cache.NewService(repository, scheduler, cache.Limits{
		MaxSize: settings.Cache.MaxSize, MaxResponseSize: settings.Cache.MaxResponseSize,
	})
	if err != nil {
		return nil, fmt.Errorf("create HTTP cache service: %w", err)
	}
	httpExecutor, err := NewHTTPExecutor(client, scheduler, settings.Cache.MaxResponseSize)
	if err != nil {
		return nil, err
	}
	retries, err := NewConfiguredRetryExecutor(httpExecutor, limits, providerName, settings.Network, scheduler)
	if err != nil {
		return nil, err
	}
	return NewCachedHTTPService(providerName, cacheService, retries, settings.Cache.TTL)
}

// Fetch returns the exact raw response body. It does not parse provider data.
func (service *CachedHTTPService) Fetch(ctx context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
	if err := ctx.Err(); err != nil {
		return provider.ResourceResponse{}, err
	}
	if request.Transport.Required != "" && request.Transport.Required != provider.TransportHTTP {
		return provider.ResourceResponse{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the resource requires a non-HTTP transport", nil)
	}
	if err := request.Market.Validate(); err != nil {
		return provider.ResourceResponse{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the resource market is invalid", err)
	}
	key, err := cache.BuildKey(service.providerName, request)
	if err != nil {
		return provider.ResourceResponse{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the resource cache identity is invalid", err)
	}

	var freshRetrievedAt time.Time
	entry, metadata, err := service.cache.Fetch(ctx, key.String(), service.ttl, request.Cache, func(ctx context.Context) (cache.Entry, error) {
		response, err := service.http.Fetch(ctx, request)
		if err != nil {
			return cache.Entry{}, err
		}
		entry, err := responseEntry(service.providerName, request, response)
		if err != nil {
			return cache.Entry{}, err
		}
		freshRetrievedAt = response.RetrievedAt
		return entry, nil
	})
	if err != nil {
		return provider.ResourceResponse{}, err
	}
	retrievedAt := entry.StoredAt
	if !metadata.Hit && !freshRetrievedAt.IsZero() {
		retrievedAt = freshRetrievedAt
	}
	return resourceResponse(entry, metadata, retrievedAt), nil
}

func responseEntry(providerName string, request provider.ResourceRequest, response provider.ResourceResponse) (cache.Entry, error) {
	if response.Page != nil || response.Transport != provider.TransportHTTP {
		return cache.Entry{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the direct HTTP transport returned an invalid response", nil)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return cache.Entry{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the website returned a non-success HTTP response", nil)
	}
	safeURL, err := responseStorageURL(response.FinalURL)
	if err != nil {
		return cache.Entry{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the direct HTTP transport returned an invalid final URL", err)
	}
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if method == "" {
		method = http.MethodGet
	}
	return cache.Entry{
		Provider:    providerName,
		Market:      request.Market,
		Method:      method,
		URL:         safeURL,
		StatusCode:  response.StatusCode,
		SafeHeaders: cloneHeaders(response.SafeHeaders),
		Body:        append([]byte(nil), response.Body...),
		Encoding:    cache.EncodingIdentity,
	}, nil
}

func responseStorageURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", errors.New("final URL must be an absolute safe HTTP URL")
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String(), nil
}

func resourceResponse(entry cache.Entry, metadata provider.CacheMetadata, retrievedAt time.Time) provider.ResourceResponse {
	return provider.ResourceResponse{
		Body:        append([]byte(nil), entry.Body...),
		StatusCode:  entry.StatusCode,
		FinalURL:    entry.URL,
		SafeHeaders: cloneHeaders(entry.SafeHeaders),
		RetrievedAt: retrievedAt,
		Transport:   provider.TransportHTTP,
		Cache:       metadata,
	}
}

func cloneHeaders(headers map[string][]string) map[string][]string {
	if headers == nil {
		return nil
	}
	clone := make(map[string][]string, len(headers))
	for name, values := range headers {
		clone[name] = append([]string(nil), values...)
	}
	return clone
}

var _ provider.ResourceService = (*CachedHTTPService)(nil)
