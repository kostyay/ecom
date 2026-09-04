package cache

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
)

var (
	errNetwork    = errors.New("network failed")
	errRepository = errors.New("repository failed")
	errWrite      = errors.New("write failed")
)

type fakeClock struct {
	now time.Time
}

func (clock *fakeClock) Now() time.Time { return clock.now }

type fakeRepository struct {
	entry    Entry
	found    bool
	getErr   error
	putErr   error
	getCalls int
	putCalls int
	putEntry Entry
	expired  PruneResult
	lru      PruneResult
	pruneAt  time.Time
	pruneMax int64
	pruneLim int
	provider string
}

func (repository *fakeRepository) Get(_ context.Context, _ string, accessedAt time.Time) (Entry, error) {
	repository.getCalls++
	if repository.getErr != nil {
		return Entry{}, repository.getErr
	}
	if !repository.found {
		return Entry{}, ErrEntryNotFound
	}
	entry := cloneForTest(repository.entry)
	if accessedAt.After(entry.AccessedAt) {
		entry.AccessedAt = accessedAt
	}
	return entry, nil
}

func (repository *fakeRepository) Put(_ context.Context, entry Entry) (Entry, error) {
	repository.putCalls++
	repository.putEntry = cloneForTest(entry)
	if repository.putErr != nil {
		return Entry{}, repository.putErr
	}
	repository.entry = cloneForTest(entry)
	repository.found = true
	return cloneForTest(entry), nil
}

func (repository *fakeRepository) ListByProvider(context.Context, string) ([]Entry, error) {
	return nil, nil
}

func (repository *fakeRepository) DeleteExpired(_ context.Context, now time.Time, limit int) (PruneResult, error) {
	repository.pruneAt = now
	repository.pruneLim = limit
	return repository.expired, nil
}

func (repository *fakeRepository) PruneToSize(_ context.Context, maxSize int64) (PruneResult, error) {
	repository.pruneMax = maxSize
	return repository.lru, nil
}

func (repository *fakeRepository) Prune(_ context.Context, now time.Time, maxSize int64) (PruneResult, error) {
	repository.pruneAt = now
	repository.pruneLim = 0
	repository.pruneMax = maxSize
	return PruneResult{
		EntriesDeleted: repository.expired.EntriesDeleted + repository.lru.EntriesDeleted,
		BytesDeleted:   repository.expired.BytesDeleted + repository.lru.BytesDeleted,
	}, nil
}

func (repository *fakeRepository) DeleteAll(context.Context) (PruneResult, error) {
	return repository.expired, nil
}

func (repository *fakeRepository) DeleteByProvider(_ context.Context, providerName string) (PruneResult, error) {
	repository.provider = providerName
	return repository.expired, nil
}

func TestServiceFetchStateTable(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	ttl := 24 * time.Hour
	old := testServiceEntry(now.Add(-time.Hour), ttl, []byte("old"), EncodingIdentity)
	expired := testServiceEntry(now.Add(-ttl), ttl, []byte("stale"), EncodingIdentity)
	fresh := testServiceEntry(time.Time{}, ttl, []byte("fresh"), EncodingIdentity)

	tests := []struct {
		name         string
		stored       *Entry
		policy       provider.CachePolicy
		fetchEntry   Entry
		fetchErr     error
		wantBody     string
		wantEncoding Encoding
		wantMetadata provider.CacheMetadata
		wantFetches  int
		wantPuts     int
		wantErr      error
	}{
		{
			name:     "valid hit",
			stored:   &old,
			wantBody: "old", wantEncoding: EncodingIdentity,
			wantMetadata: provider.CacheMetadata{Hit: true, StoredAt: old.StoredAt, Age: time.Hour, TTL: ttl},
		},
		{
			name:       "miss",
			fetchEntry: fresh,
			wantBody:   "fresh", wantEncoding: EncodingIdentity, wantPuts: 1, wantFetches: 1,
			wantMetadata: provider.CacheMetadata{StoredAt: now, TTL: ttl},
		},
		{
			name:   "expired entry is replaced at exact boundary",
			stored: &expired, fetchEntry: fresh,
			wantBody: "fresh", wantEncoding: EncodingIdentity, wantPuts: 1, wantFetches: 1,
			wantMetadata: provider.CacheMetadata{StoredAt: now, TTL: ttl},
		},
		{
			name:   "refresh bypasses valid entry",
			stored: &old, policy: provider.CachePolicy{Refresh: true}, fetchEntry: fresh,
			wantBody: "fresh", wantEncoding: EncodingIdentity, wantPuts: 1, wantFetches: 1,
			wantMetadata: provider.CacheMetadata{StoredAt: now, TTL: ttl},
		},
		{
			name:     "failed miss",
			fetchErr: errNetwork, wantFetches: 1, wantErr: errNetwork,
		},
		{
			name:   "failed expired entry without stale policy",
			stored: &expired, fetchErr: errNetwork, wantFetches: 1, wantErr: errNetwork,
		},
		{
			name:   "failed expired entry uses requested stale value",
			stored: &expired, policy: provider.CachePolicy{StaleIfError: true}, fetchErr: errNetwork,
			wantBody: "stale", wantEncoding: EncodingIdentity, wantFetches: 1,
			wantMetadata: provider.CacheMetadata{Hit: true, Stale: true, StoredAt: expired.StoredAt, Age: ttl, TTL: ttl},
		},
		{
			name:   "failed refresh does not use valid entry as stale",
			stored: &old, policy: provider.CachePolicy{Refresh: true, StaleIfError: true}, fetchErr: errNetwork,
			wantFetches: 1, wantErr: errNetwork,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{}
			if test.stored != nil {
				repository.entry = cloneForTest(*test.stored)
				repository.found = true
			}
			clock := &fakeClock{now: now}
			service := mustService(t, repository, clock)
			fetchCalls := 0
			entry, metadata, err := service.Fetch(t.Context(), "key", ttl, test.policy, func(context.Context) (Entry, error) {
				fetchCalls++
				return cloneForTest(test.fetchEntry), test.fetchErr
			})

			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Fetch() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil {
				if string(entry.Body) != test.wantBody || entry.Encoding != test.wantEncoding {
					t.Errorf("Fetch() entry body/encoding = %q/%q, want %q/%q", entry.Body, entry.Encoding, test.wantBody, test.wantEncoding)
				}
				if !reflect.DeepEqual(metadata, test.wantMetadata) {
					t.Errorf("Fetch() metadata = %#v, want %#v", metadata, test.wantMetadata)
				}
			}
			if fetchCalls != test.wantFetches || repository.putCalls != test.wantPuts {
				t.Errorf("calls fetch/put = %d/%d, want %d/%d", fetchCalls, repository.putCalls, test.wantFetches, test.wantPuts)
			}
			if test.fetchErr != nil && test.stored != nil && (!repository.found || string(repository.entry.Body) != string(test.stored.Body)) {
				t.Errorf("failed fetch changed prior entry to %#v", repository.entry)
			}
		})
	}
}

func TestServiceFetchClockSkewClampsAge(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	entry := testServiceEntry(now.Add(time.Hour), 24*time.Hour, []byte("future"), EncodingIdentity)
	repository := &fakeRepository{entry: entry, found: true}
	service := mustService(t, repository, &fakeClock{now: now})

	_, metadata, err := service.Fetch(t.Context(), "key", 24*time.Hour, provider.CachePolicy{}, unexpectedFetch(t))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if metadata.Age != 0 || metadata.TTL != 24*time.Hour || !metadata.Hit {
		t.Fatalf("Fetch() metadata = %#v", metadata)
	}
}

func TestServiceFetchRepositoryReadError(t *testing.T) {
	repository := &fakeRepository{getErr: errRepository}
	service := mustService(t, repository, &fakeClock{now: time.Now()})
	_, _, err := service.Fetch(t.Context(), "key", time.Hour, provider.CachePolicy{}, unexpectedFetch(t))
	if !errors.Is(err, errRepository) {
		t.Fatalf("Fetch() error = %v, want repository error", err)
	}
	if repository.putCalls != 0 {
		t.Fatalf("Put() calls = %d, want 0", repository.putCalls)
	}
}

func TestServiceFetchCancellation(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	expired := testServiceEntry(now.Add(-24*time.Hour), 24*time.Hour, []byte("stale"), EncodingIdentity)
	repository := &fakeRepository{entry: expired, found: true}
	service := mustService(t, repository, &fakeClock{now: now})
	ctx, cancel := context.WithCancel(t.Context())

	_, _, err := service.Fetch(ctx, "key", 24*time.Hour, provider.CachePolicy{StaleIfError: true}, func(context.Context) (Entry, error) {
		cancel()
		return Entry{}, context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch() error = %v, want context.Canceled", err)
	}
	if repository.putCalls != 0 {
		t.Fatalf("Put() calls = %d, want 0", repository.putCalls)
	}
}

func TestServiceFetchDoesNotHideFetchCancellationWithStaleEntry(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	expired := testServiceEntry(now.Add(-24*time.Hour), 24*time.Hour, []byte("stale"), EncodingIdentity)
	repository := &fakeRepository{entry: expired, found: true}
	service := mustService(t, repository, &fakeClock{now: now})

	_, _, err := service.Fetch(t.Context(), "key", 24*time.Hour, provider.CachePolicy{StaleIfError: true}, func(context.Context) (Entry, error) {
		return Entry{}, context.DeadlineExceeded
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Fetch() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestServiceFetchCanceledBeforeRead(t *testing.T) {
	repository := &fakeRepository{}
	service := mustService(t, repository, &fakeClock{now: time.Now()})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err := service.Fetch(ctx, "key", time.Hour, provider.CachePolicy{}, unexpectedFetch(t))
	if !errors.Is(err, context.Canceled) || repository.getCalls != 0 {
		t.Fatalf("Fetch() error/get calls = %v/%d, want context.Canceled/0", err, repository.getCalls)
	}
}

func TestServiceFetchWriteFailurePreservesPriorEntry(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	prior := testServiceEntry(now.Add(-24*time.Hour), 24*time.Hour, []byte("prior"), EncodingIdentity)
	repository := &fakeRepository{entry: prior, found: true, putErr: errWrite}
	service := mustService(t, repository, &fakeClock{now: now})

	_, _, err := service.Fetch(t.Context(), "key", 6*time.Hour, provider.CachePolicy{}, func(context.Context) (Entry, error) {
		return testServiceEntry(time.Time{}, time.Hour, []byte("replacement"), EncodingGzip), nil
	})
	if !errors.Is(err, errWrite) {
		t.Fatalf("Fetch() error = %v, want write error", err)
	}
	if string(repository.entry.Body) != "prior" {
		t.Fatalf("stored body = %q, want prior", repository.entry.Body)
	}
	if repository.putEntry.Key != "key" || !repository.putEntry.StoredAt.Equal(now) || !repository.putEntry.ExpiresAt.Equal(now.Add(6*time.Hour)) {
		t.Fatalf("Put() entry cache values = %#v", repository.putEntry)
	}
}

func TestNewServiceValidation(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	if _, err := NewService(nil, clock, testLimits()); err == nil {
		t.Fatal("NewService(nil) error = nil")
	}
	if _, err := NewService(&fakeRepository{}, nil, testLimits()); err == nil {
		t.Fatal("NewService(nil clock) error = nil")
	}
	if _, err := NewService(&fakeRepository{}, clock, Limits{}); err == nil {
		t.Fatal("NewService(empty limits) error = nil")
	}
}

func TestServiceCompressesStorageAndReturnsExactIdentityResponse(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	body := []byte(strings.Repeat("product data ", 256))
	repository := &fakeRepository{}
	service := mustService(t, repository, &fakeClock{now: now})

	got, _, err := service.Fetch(t.Context(), "key", time.Hour, provider.CachePolicy{}, func(context.Context) (Entry, error) {
		return testServiceEntry(time.Time{}, time.Hour, body, EncodingIdentity), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Encoding != EncodingIdentity || !reflect.DeepEqual(got.Body, body) {
		t.Fatal("Fetch response did not restore the exact transport body")
	}
	if repository.putEntry.Encoding != EncodingGzip || len(repository.putEntry.Body) >= len(body) {
		t.Fatalf("stored body encoding/size = %q/%d, want gzip smaller than %d", repository.putEntry.Encoding, len(repository.putEntry.Body), len(body))
	}
	if repository.pruneLim != normalExpiryPruneLimit || !repository.pruneAt.Equal(now) {
		t.Fatalf("bounded prune limit/time = %d/%v", repository.pruneLim, repository.pruneAt)
	}
}

func TestServiceUsesConfiguredPruneLimit(t *testing.T) {
	repository := &fakeRepository{
		expired: PruneResult{EntriesDeleted: 2, BytesDeleted: 20},
		lru:     PruneResult{EntriesDeleted: 1, BytesDeleted: 10},
	}
	limits := Limits{MaxSize: 1234, MaxResponseSize: 567}
	service, err := NewService(repository, &fakeClock{now: time.Now()}, limits)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Prune(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if repository.pruneLim != 0 || repository.pruneMax != int64(limits.MaxSize) {
		t.Fatalf("prune limit/max size = %d/%d", repository.pruneLim, repository.pruneMax)
	}
	if result != (PruneResult{EntriesDeleted: 3, BytesDeleted: 30}) {
		t.Fatalf("Prune result = %#v", result)
	}
}

func TestServiceClearsAllAndProviderResponses(t *testing.T) {
	repository := &fakeRepository{expired: PruneResult{EntriesDeleted: 2, BytesDeleted: 20}}
	service := mustService(t, repository, &fakeClock{now: time.Now()})
	result, err := service.Clear(t.Context())
	if err != nil || result != repository.expired {
		t.Fatalf("Clear = %#v, %v", result, err)
	}
	result, err = service.ClearProvider(t.Context(), "bike-discount")
	if err != nil || result != repository.expired || repository.provider != "bike-discount" {
		t.Fatalf("ClearProvider = %#v, %v; provider %q", result, err, repository.provider)
	}
	if _, err := service.ClearProvider(t.Context(), " bike-discount"); err == nil {
		t.Fatal("ClearProvider invalid scope error = nil")
	}
}

func TestServiceAppliesConfiguredResponseLimit(t *testing.T) {
	repository := &fakeRepository{}
	service, err := NewService(repository, &fakeClock{now: time.Now()}, Limits{MaxSize: 1024, MaxResponseSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = service.Fetch(t.Context(), "key", time.Hour, provider.CachePolicy{}, func(context.Context) (Entry, error) {
		return testServiceEntry(time.Time{}, time.Hour, []byte("large"), EncodingIdentity), nil
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum 4") {
		t.Fatalf("Fetch error = %v, want configured response size error", err)
	}
	if repository.putCalls != 0 {
		t.Fatalf("Put calls = %d, want 0", repository.putCalls)
	}
}

func TestServiceRejectsMalformedStoredGzip(t *testing.T) {
	now := time.Now().UTC()
	entry := testServiceEntry(now, time.Hour, []byte("not gzip"), EncodingGzip)
	repository := &fakeRepository{entry: entry, found: true}
	service := mustService(t, repository, &fakeClock{now: now})
	_, _, err := service.Fetch(t.Context(), "key", time.Hour, provider.CachePolicy{}, unexpectedFetch(t))
	if err == nil || !strings.Contains(err.Error(), "decode cache entry") {
		t.Fatalf("Fetch error = %v, want decode error", err)
	}
}

func testServiceEntry(storedAt time.Time, ttl time.Duration, body []byte, encoding Encoding) Entry {
	return Entry{
		Key: "key", Provider: "shop", Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"},
		Method: "GET", URL: "https://shop.example/item", StatusCode: 200,
		SafeHeaders: map[string][]string{"Content-Type": {"text/html"}}, Body: append([]byte(nil), body...), Encoding: encoding,
		StoredAt: storedAt, ExpiresAt: storedAt.Add(ttl), AccessedAt: storedAt,
	}
}

func mustService(t *testing.T, repository Repository, clock Clock) *Service {
	t.Helper()
	service, err := NewService(repository, clock, testLimits())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func testLimits() Limits {
	return Limits{MaxSize: 512 * 1024 * 1024, MaxResponseSize: 20 * 1024 * 1024}
}

func unexpectedFetch(t *testing.T) FetchFunc {
	t.Helper()
	return func(context.Context) (Entry, error) {
		t.Fatal("fetch function was called")
		return Entry{}, nil
	}
}

func cloneForTest(entry Entry) Entry {
	entry.Body = append([]byte(nil), entry.Body...)
	return entry
}
