package cache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/provider"
)

const normalExpiryPruneLimit = 16

// Limits contains configured cache storage limits.
type Limits struct {
	MaxSize         config.ByteSize
	MaxResponseSize config.ByteSize
}

// Clock supplies time to Service. Tests can use a fixed or controlled clock.
type Clock interface {
	Now() time.Time
}

// ClockFunc adapts a function to Clock.
type ClockFunc func() time.Time

// Now returns the current time from the adapted function.
func (function ClockFunc) Now() time.Time {
	return function()
}

// FetchFunc gets one successful raw response from a transport. The service
// adds the cache key and cache timestamps before it stores the entry.
type FetchFunc func(context.Context) (Entry, error)

// Service applies cache-use policy around a raw-response fetch.
type Service struct {
	repository Repository
	clock      Clock
	limits     Limits
}

// NewService makes a cache policy service.
func NewService(repository Repository, clock Clock, limits Limits) (*Service, error) {
	if repository == nil {
		return nil, errors.New("cache repository is required")
	}
	if clock == nil {
		return nil, errors.New("cache clock is required")
	}
	if limits.MaxSize <= 0 || limits.MaxResponseSize <= 0 {
		return nil, errors.New("cache size limits must be positive")
	}
	return &Service{repository: repository, clock: clock, limits: limits}, nil
}

// Fetch returns a valid cache entry when possible. Otherwise, it calls fetch,
// timestamps the successful response, and replaces the stored entry. An
// expired entry is returned after a fetch error only when StaleIfError is set.
func (service *Service) Fetch(
	ctx context.Context,
	key string,
	ttl time.Duration,
	policy provider.CachePolicy,
	fetch FetchFunc,
) (Entry, provider.CacheMetadata, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, provider.CacheMetadata{}, err
	}
	if strings.TrimSpace(key) == "" || key != strings.TrimSpace(key) {
		return Entry{}, provider.CacheMetadata{}, errors.New("cache key is required")
	}
	if ttl <= 0 {
		return Entry{}, provider.CacheMetadata{}, errors.New("cache TTL must be positive")
	}
	if fetch == nil {
		return Entry{}, provider.CacheMetadata{}, errors.New("cache fetch function is required")
	}

	now := service.clock.Now().UTC()
	stored, found, err := service.get(ctx, key, now)
	if err != nil {
		return Entry{}, provider.CacheMetadata{}, err
	}
	expired := found && !stored.ExpiresAt.After(now)
	if found && !expired && !policy.Refresh {
		return service.decoded(stored, now, true, false)
	}

	fresh, err := fetch(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Entry{}, provider.CacheMetadata{}, ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Entry{}, provider.CacheMetadata{}, err
		}
		if expired && policy.StaleIfError {
			return service.decoded(stored, now, true, true)
		}
		return Entry{}, provider.CacheMetadata{}, fmt.Errorf("fetch fresh cache entry: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Entry{}, provider.CacheMetadata{}, err
	}

	storedAt := service.clock.Now().UTC()
	fresh.Key = key
	fresh.StoredAt = storedAt
	fresh.ExpiresAt = storedAt.Add(ttl)
	fresh.AccessedAt = storedAt
	encoded, err := encodeEntry(fresh, int64(service.limits.MaxResponseSize))
	if err != nil {
		return Entry{}, provider.CacheMetadata{}, err
	}
	stored, err = service.repository.Put(ctx, encoded)
	if err != nil {
		return Entry{}, provider.CacheMetadata{}, fmt.Errorf("store fresh cache entry %q: %w", key, err)
	}
	service.pruneExpiredAfterWrite(ctx, storedAt)
	return service.decoded(stored, storedAt, false, false)
}

// PruneExpired deletes at most limit expired raw responses. This operation is
// suitable for normal command use because its work is bounded.
func (service *Service) PruneExpired(ctx context.Context, limit int) (PruneResult, error) {
	repository, ok := service.repository.(MaintenanceRepository)
	if !ok {
		return PruneResult{}, errors.New("cache repository does not support maintenance")
	}
	if limit <= 0 {
		return PruneResult{}, errors.New("expiry prune limit must be positive")
	}
	result, err := repository.DeleteExpired(ctx, service.clock.Now().UTC(), limit)
	if err != nil {
		return PruneResult{}, fmt.Errorf("prune expired cache entries: %w", err)
	}
	return result, nil
}

// Prune performs full maintenance. It removes all expired responses before it
// applies the configured total stored-size limit.
func (service *Service) Prune(ctx context.Context) (PruneResult, error) {
	repository, ok := service.repository.(MaintenanceRepository)
	if !ok {
		return PruneResult{}, errors.New("cache repository does not support maintenance")
	}
	result, err := repository.Prune(ctx, service.clock.Now().UTC(), int64(service.limits.MaxSize))
	if err != nil {
		return PruneResult{}, fmt.Errorf("prune cache: %w", err)
	}
	return result, nil
}

// Clear removes all raw responses without changing browser session state.
func (service *Service) Clear(ctx context.Context) (PruneResult, error) {
	repository, ok := service.repository.(MaintenanceRepository)
	if !ok {
		return PruneResult{}, errors.New("cache repository does not support maintenance")
	}
	result, err := repository.DeleteAll(ctx)
	if err != nil {
		return PruneResult{}, fmt.Errorf("clear cache: %w", err)
	}
	return result, nil
}

// ClearProvider removes raw responses for one exact provider.
func (service *Service) ClearProvider(ctx context.Context, providerName string) (PruneResult, error) {
	repository, ok := service.repository.(MaintenanceRepository)
	if !ok {
		return PruneResult{}, errors.New("cache repository does not support maintenance")
	}
	if strings.TrimSpace(providerName) == "" || providerName != strings.TrimSpace(providerName) {
		return PruneResult{}, errors.New("cache provider is required")
	}
	result, err := repository.DeleteByProvider(ctx, providerName)
	if err != nil {
		return PruneResult{}, fmt.Errorf("clear cache for provider %q: %w", providerName, err)
	}
	return result, nil
}

func (service *Service) decoded(entry Entry, now time.Time, hit, stale bool) (Entry, provider.CacheMetadata, error) {
	decoded, err := decodeEntry(entry, int64(service.limits.MaxResponseSize))
	if err != nil {
		return Entry{}, provider.CacheMetadata{}, fmt.Errorf("decode cache entry %q: %w", entry.Key, err)
	}
	return decoded, metadata(entry, now, hit, stale), nil
}

func (service *Service) pruneExpiredAfterWrite(ctx context.Context, now time.Time) {
	repository, ok := service.repository.(MaintenanceRepository)
	if !ok {
		return
	}
	// Maintenance must not turn a successful fetch and store into a failure.
	_, _ = repository.DeleteExpired(ctx, now, normalExpiryPruneLimit)
}

func (service *Service) get(ctx context.Context, key string, now time.Time) (Entry, bool, error) {
	entry, err := service.repository.Get(ctx, key, now)
	if errors.Is(err, ErrEntryNotFound) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, fmt.Errorf("get cache entry %q: %w", key, err)
	}
	return entry, true, nil
}

func metadata(entry Entry, now time.Time, hit, stale bool) provider.CacheMetadata {
	age := now.Sub(entry.StoredAt)
	if age < 0 {
		age = 0
	}
	ttl := entry.ExpiresAt.Sub(entry.StoredAt)
	if ttl < 0 {
		ttl = 0
	}
	return provider.CacheMetadata{
		Hit:      hit,
		Stale:    stale,
		StoredAt: entry.StoredAt,
		Age:      age,
		TTL:      ttl,
	}
}
