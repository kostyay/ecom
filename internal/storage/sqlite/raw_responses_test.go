package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/internal/cache"
	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/provider"
)

func TestRawResponseRepositoryRoundTripAndAccessTime(t *testing.T) {
	repository := openRawResponseRepository(t, 4096)
	entry := testRawResponseEntry("v1:key", "bike-discount")

	stored, err := repository.Put(t.Context(), entry)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if stored.SizeBytes <= int64(len(stored.Body)) {
		t.Fatalf("stored size = %d, want complete entry size greater than body size %d", stored.SizeBytes, len(stored.Body))
	}
	headersJSON, err := json.Marshal(stored.SafeHeaders)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SizeBytes != storedEntrySize(stored, headersJSON) {
		t.Fatalf("stored size = %d, want exact stored size %d", stored.SizeBytes, storedEntrySize(stored, headersJSON))
	}

	accessedAt := entry.AccessedAt.Add(time.Hour)
	got, err := repository.Get(t.Context(), entry.Key, accessedAt)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := stored
	want.AccessedAt = accessedAt
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Get entry = %#v, want %#v", got, want)
	}

	// A read with an older clock value must not make LRU metadata go backward.
	got, err = repository.Get(t.Context(), entry.Key, entry.StoredAt)
	if err != nil {
		t.Fatalf("Get with old access time: %v", err)
	}
	if !got.AccessedAt.Equal(accessedAt) {
		t.Errorf("access time = %v, want %v", got.AccessedAt, accessedAt)
	}
}

func TestCacheServiceCompressesSQLiteAndRestoresProviderBody(t *testing.T) {
	repository := openRawResponseRepository(t, 8192)
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	body := []byte(strings.Repeat("product response ", 256))
	service, err := cache.NewService(repository, cache.ClockFunc(func() time.Time { return now }), cache.Limits{
		MaxSize:         config.ByteSize(64 * 1024),
		MaxResponseSize: config.ByteSize(len(body)),
	})
	if err != nil {
		t.Fatal(err)
	}

	got, _, err := service.Fetch(t.Context(), "v1:compressed", time.Hour, provider.CachePolicy{}, func(context.Context) (cache.Entry, error) {
		entry := testRawResponseEntry("unused", "bike-discount")
		entry.Body = append([]byte(nil), body...)
		return entry, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Encoding != cache.EncodingIdentity || !reflect.DeepEqual(got.Body, body) {
		t.Fatal("fresh service response differs from transport response")
	}
	stored, err := repository.ListByProvider(t.Context(), "bike-discount")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored entry count = %d, want 1", len(stored))
	}
	if stored[0].Encoding != cache.EncodingGzip || len(stored[0].Body) >= len(body) {
		t.Fatalf("stored entry encoding/body size = %q/%d", stored[0].Encoding, len(stored[0].Body))
	}

	got, metadata, err := service.Fetch(t.Context(), "v1:compressed", time.Hour, provider.CachePolicy{}, func(context.Context) (cache.Entry, error) {
		t.Fatal("cache hit called fetch")
		return cache.Entry{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.Hit || got.Encoding != cache.EncodingIdentity || !reflect.DeepEqual(got.Body, body) {
		t.Fatal("cache hit did not restore the exact provider body")
	}
}

func TestRawResponseRepositoryUpsert(t *testing.T) {
	repository := openRawResponseRepository(t, 4096)
	entry := testRawResponseEntry("v1:key", "bike-discount")
	if _, err := repository.Put(t.Context(), entry); err != nil {
		t.Fatal(err)
	}

	replacement := testRawResponseEntry(entry.Key, "other-provider")
	replacement.Body = []byte("replacement")
	replacement.StatusCode = 204
	replacement.StoredAt = entry.StoredAt.Add(time.Hour)
	replacement.AccessedAt = replacement.StoredAt
	replacement.ExpiresAt = replacement.StoredAt.Add(24 * time.Hour)
	stored, err := repository.Put(t.Context(), replacement)
	if err != nil {
		t.Fatalf("Put replacement: %v", err)
	}
	got, err := repository.Get(t.Context(), entry.Key, replacement.AccessedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, stored) {
		t.Errorf("Get replacement = %#v, want %#v", got, stored)
	}
	oldProvider, err := repository.ListByProvider(t.Context(), entry.Provider)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldProvider) != 0 {
		t.Errorf("old provider entries = %d, want 0", len(oldProvider))
	}
}

func TestRawResponseRepositoryMissing(t *testing.T) {
	repository := openRawResponseRepository(t, 4096)
	_, err := repository.Get(t.Context(), "v1:missing", time.Now())
	if !errors.Is(err, cache.ErrEntryNotFound) {
		t.Fatalf("Get error = %v, want ErrEntryNotFound", err)
	}
}

func TestRawResponseRepositoryListsOneProvider(t *testing.T) {
	repository := openRawResponseRepository(t, 4096)
	entries := []cache.Entry{
		testRawResponseEntry("v1:b", "bike-discount"),
		testRawResponseEntry("v1:a", "bike-discount"),
		testRawResponseEntry("v1:c", "other-provider"),
	}
	for _, entry := range entries {
		if _, err := repository.Put(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}

	got, err := repository.ListByProvider(t.Context(), "bike-discount")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "v1:a" || got[1].Key != "v1:b" {
		t.Errorf("provider entries = %#v, want keys v1:a and v1:b", got)
	}
}

func TestRawResponseRepositoryRejectsInvalidEntries(t *testing.T) {
	valid := testRawResponseEntry("v1:key", "bike-discount")
	tests := []struct {
		name   string
		change func(*cache.Entry)
		want   string
	}{
		{name: "key", change: func(entry *cache.Entry) { entry.Key = "" }, want: "cache key"},
		{name: "provider", change: func(entry *cache.Entry) { entry.Provider = " " }, want: "provider"},
		{name: "market", change: func(entry *cache.Entry) { entry.Market.Currency = "eur" }, want: "market"},
		{name: "method", change: func(entry *cache.Entry) { entry.Method = "get" }, want: "method"},
		{name: "URL user", change: func(entry *cache.Entry) { entry.URL = "https://user:secret@example.test/product" }, want: "safe HTTP URL"},
		{name: "URL query", change: func(entry *cache.Entry) { entry.URL = "https://example.test/product?token=secret" }, want: "query values"},
		{name: "status", change: func(entry *cache.Entry) { entry.StatusCode = 404 }, want: "successful"},
		{name: "encoding", change: func(entry *cache.Entry) { entry.Encoding = "brotli" }, want: "encoding"},
		{name: "storage time", change: func(entry *cache.Entry) { entry.StoredAt = time.Time{} }, want: "timestamps"},
		{name: "expiry", change: func(entry *cache.Entry) { entry.ExpiresAt = entry.StoredAt }, want: "expiry"},
		{name: "access time", change: func(entry *cache.Entry) { entry.AccessedAt = entry.StoredAt.Add(-time.Second) }, want: "access time"},
		{name: "sensitive header", change: func(entry *cache.Entry) { entry.SafeHeaders = map[string][]string{"Set-Cookie": {"secret"}} }, want: "sensitive"},
		{name: "invalid header name", change: func(entry *cache.Entry) { entry.SafeHeaders = map[string][]string{"Bad Header": {"value"}} }, want: "name is invalid"},
		{name: "invalid header", change: func(entry *cache.Entry) { entry.SafeHeaders = map[string][]string{"X-Test": {"bad\r\nvalue"}} }, want: "invalid value"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := openRawResponseRepository(t, 4096)
			entry := cloneTestEntry(valid)
			test.change(&entry)
			_, err := repository.Put(t.Context(), entry)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Put error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestRawResponseRepositoryRejectsOversizedBodyAndAccountsForEntry(t *testing.T) {
	t.Run("body", func(t *testing.T) {
		repository := openRawResponseRepository(t, 64)
		entry := testRawResponseEntry("v1:key", "bike-discount")
		entry.Body = make([]byte, 65)
		_, err := repository.Put(t.Context(), entry)
		if err == nil || !strings.Contains(err.Error(), "body size") {
			t.Fatalf("Put error = %v, want body size error", err)
		}
	})

	t.Run("complete entry accounting", func(t *testing.T) {
		repository := openRawResponseRepository(t, 128)
		entry := testRawResponseEntry("v1:key", "bike-discount")
		entry.Body = []byte("small")
		entry.SafeHeaders = map[string][]string{"X-Large": {strings.Repeat("x", 128)}}
		stored, err := repository.Put(t.Context(), entry)
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		if stored.SizeBytes <= int64(len(stored.Body)+128) {
			t.Fatalf("stored size = %d, want all stored fields included", stored.SizeBytes)
		}
	})
}

func TestRawResponseRepositoryDeleteExpiredUsesStableExpiryOrder(t *testing.T) {
	repository := openRawResponseRepository(t, 4096)
	now := testRawResponseEntry("v1:z", "shop").StoredAt.Add(48 * time.Hour)
	for index, key := range []string{"v1:b", "v1:a", "v1:c"} {
		entry := testRawResponseEntry(key, "shop")
		entry.ExpiresAt = now.Add(time.Duration(index-2) * time.Hour)
		if key == "v1:a" {
			entry.ExpiresAt = now.Add(-2 * time.Hour) // Tie with v1:b; key decides.
		}
		if _, err := repository.Put(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}

	result, err := repository.DeleteExpired(t.Context(), now, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.EntriesDeleted != 1 || result.BytesDeleted <= 0 {
		t.Fatalf("DeleteExpired result = %#v", result)
	}
	assertRawResponseKeys(t, repository, []string{"v1:b", "v1:c"})

	result, err = repository.DeleteExpired(t.Context(), now, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.EntriesDeleted != 2 {
		t.Fatalf("full DeleteExpired result = %#v, want two entries", result)
	}
	assertRawResponseKeys(t, repository, nil)
}

func TestRawResponseRepositoryPruneToSizeUsesStableLRUOrder(t *testing.T) {
	repository := openRawResponseRepository(t, 4096)
	base := testRawResponseEntry("v1:z", "shop").StoredAt
	var total int64
	var oneSize int64
	for _, key := range []string{"v1:b", "v1:a", "v1:c"} {
		entry := testRawResponseEntry(key, "shop")
		entry.AccessedAt = base
		if key == "v1:c" {
			entry.AccessedAt = base.Add(time.Hour)
		}
		stored, err := repository.Put(t.Context(), entry)
		if err != nil {
			t.Fatal(err)
		}
		total += stored.SizeBytes
		oneSize = stored.SizeBytes
	}

	result, err := repository.PruneToSize(t.Context(), total-oneSize)
	if err != nil {
		t.Fatal(err)
	}
	if result.EntriesDeleted != 1 || result.BytesDeleted != oneSize {
		t.Fatalf("PruneToSize result = %#v, want one %d-byte entry", result, oneSize)
	}
	assertRawResponseKeys(t, repository, []string{"v1:b", "v1:c"})

	result, err = repository.PruneToSize(t.Context(), oneSize)
	if err != nil {
		t.Fatal(err)
	}
	if result.EntriesDeleted != 1 {
		t.Fatalf("second PruneToSize result = %#v", result)
	}
	assertRawResponseKeys(t, repository, []string{"v1:c"})
}

func TestRawResponsePruningKeepsBrowserSessions(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "state.db"))
	repository, err := NewRawResponseRepository(database, 4096)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := NewBrowserSessionRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	record := testBrowserSessionRecord("bike-discount", testSessionMarket())
	if _, err := sessions.Put(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	entry := testRawResponseEntry("v1:expired", "bike-discount")
	if _, err := repository.Put(t.Context(), entry); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.DeleteExpired(t.Context(), entry.ExpiresAt, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PruneToSize(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Get(t.Context(), record.Provider, record.Market); err != nil {
		t.Fatalf("browser session after response pruning: %v", err)
	}
}

func TestRawResponseRepositoryPruneIsAtomicAndCountsBytes(t *testing.T) {
	repository := openRawResponseRepository(t, 4096)
	now := testRawResponseEntry("v1:base", "shop").StoredAt.Add(48 * time.Hour)
	expired := testRawResponseEntry("v1:expired", "shop")
	freshOld := testRawResponseEntry("v1:old", "shop")
	freshOld.StoredAt = now.Add(-time.Hour)
	freshOld.ExpiresAt = now.Add(time.Hour)
	freshOld.AccessedAt = now.Add(-time.Hour)
	freshNew := freshOld
	freshNew.Key = "v1:new"
	freshNew.AccessedAt = now

	stored := make(map[string]cache.Entry)
	for _, entry := range []cache.Entry{expired, freshOld, freshNew} {
		got, err := repository.Put(t.Context(), entry)
		if err != nil {
			t.Fatal(err)
		}
		stored[entry.Key] = got
	}
	result, err := repository.Prune(t.Context(), now, stored["v1:new"].SizeBytes)
	if err != nil {
		t.Fatal(err)
	}
	wantBytes := stored["v1:expired"].SizeBytes + stored["v1:old"].SizeBytes
	if result != (cache.PruneResult{EntriesDeleted: 2, BytesDeleted: wantBytes}) {
		t.Fatalf("Prune = %#v, want 2 entries and %d bytes", result, wantBytes)
	}
	assertRawResponseKeys(t, repository, []string{"v1:new"})

	result, err = repository.Prune(t.Context(), now, stored["v1:new"].SizeBytes)
	if err != nil || result != (cache.PruneResult{}) {
		t.Fatalf("second Prune = %#v, %v", result, err)
	}
}

func TestRawResponseRepositoryPruneRollsBackAllSteps(t *testing.T) {
	repository := openRawResponseRepository(t, 4096)
	now := testRawResponseEntry("v1:base", "shop").StoredAt.Add(48 * time.Hour)
	for _, key := range []string{"v1:expired", "v1:fresh"} {
		entry := testRawResponseEntry(key, "shop")
		if key == "v1:fresh" {
			entry.StoredAt = now.Add(-time.Hour)
			entry.ExpiresAt = now.Add(time.Hour)
			entry.AccessedAt = now.Add(-time.Hour)
		}
		if _, err := repository.Put(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}
	repository.beforeCommit = func() error { return errors.New("test rollback") }
	if _, err := repository.Prune(t.Context(), now, 1); err == nil {
		t.Fatal("Prune rollback error = nil")
	}
	assertRawResponseKeys(t, repository, []string{"v1:expired", "v1:fresh"})
}

func TestRawResponseRepositoryClearScopesAndKeepsSessions(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "state.db"))
	repository, err := NewRawResponseRepository(database, 4096)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := NewBrowserSessionRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	record := testBrowserSessionRecord("bike-discount", testSessionMarket())
	if _, err := sessions.Put(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	stored := make(map[string]cache.Entry)
	for _, pair := range [][2]string{{"v1:bike", "bike-discount"}, {"v1:other", "other-provider"}} {
		entry, err := repository.Put(t.Context(), testRawResponseEntry(pair[0], pair[1]))
		if err != nil {
			t.Fatal(err)
		}
		stored[pair[0]] = entry
	}
	result, err := repository.DeleteByProvider(t.Context(), "bike-discount")
	if err != nil || result != (cache.PruneResult{EntriesDeleted: 1, BytesDeleted: stored["v1:bike"].SizeBytes}) {
		t.Fatalf("DeleteByProvider = %#v, %v", result, err)
	}
	if entries, err := repository.ListByProvider(t.Context(), "other-provider"); err != nil || len(entries) != 1 {
		t.Fatalf("other provider entries = %d, %v", len(entries), err)
	}
	result, err = repository.DeleteAll(t.Context())
	if err != nil || result != (cache.PruneResult{EntriesDeleted: 1, BytesDeleted: stored["v1:other"].SizeBytes}) {
		t.Fatalf("DeleteAll = %#v, %v", result, err)
	}
	if _, err := sessions.Get(t.Context(), record.Provider, record.Market); err != nil {
		t.Fatalf("session after response clear: %v", err)
	}
	result, err = repository.DeleteAll(t.Context())
	if err != nil || result != (cache.PruneResult{}) {
		t.Fatalf("second DeleteAll = %#v, %v", result, err)
	}
}

func TestRawResponseMaintenanceHonorsCancellation(t *testing.T) {
	repository := openRawResponseRepository(t, 4096)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := repository.DeleteAll(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("DeleteAll error = %v, want context.Canceled", err)
	}
}

func assertRawResponseKeys(t *testing.T, repository *RawResponseRepository, want []string) {
	t.Helper()
	entries, err := repository.ListByProvider(t.Context(), "shop")
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(entries))
	for index, entry := range entries {
		got[index] = entry.Key
	}
	if !slices.Equal(got, want) {
		t.Fatalf("raw response keys = %v, want %v", got, want)
	}
}

func openRawResponseRepository(t *testing.T, maxSize config.ByteSize) *RawResponseRepository {
	t.Helper()
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "state.db"))
	repository, err := NewRawResponseRepository(database, maxSize)
	if err != nil {
		t.Fatalf("NewRawResponseRepository: %v", err)
	}
	return repository
}

func testRawResponseEntry(key, providerName string) cache.Entry {
	storedAt := time.Date(2026, 8, 12, 12, 0, 0, 123456789, time.UTC)
	return cache.Entry{
		Key:      key,
		Provider: providerName,
		Market: provider.Market{
			Country: "DE", Language: "en", Currency: "EUR",
		},
		Method:      "GET",
		URL:         "https://example.test/product",
		StatusCode:  200,
		SafeHeaders: map[string][]string{"Content-Type": {"text/html"}, "ETag": {`"abc"`}},
		Body:        []byte("raw body"),
		Encoding:    cache.EncodingIdentity,
		StoredAt:    storedAt,
		ExpiresAt:   storedAt.Add(24 * time.Hour),
		AccessedAt:  storedAt,
	}
}

func cloneTestEntry(entry cache.Entry) cache.Entry {
	entry.Body = append([]byte(nil), entry.Body...)
	entry.SafeHeaders = map[string][]string{}
	for name, values := range testRawResponseEntry(entry.Key, entry.Provider).SafeHeaders {
		entry.SafeHeaders[name] = append([]string(nil), values...)
	}
	return entry
}
