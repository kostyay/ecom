package maintenance_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kostyay/ecom/internal/cache"
	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/internal/maintenance"
	"github.com/kostyay/ecom/internal/session"
	"github.com/kostyay/ecom/internal/storage/sqlite"
	"github.com/kostyay/ecom/provider"
)

type fixedClock time.Time

func (clock fixedClock) Now() time.Time { return time.Time(clock) }

func TestServiceWithSQLiteKeepsResponseAndSessionScopesSeparate(t *testing.T) {
	ctx := t.Context()
	service, responses, sessions, _ := openService(t)
	market := testMarket()
	putResponse(t, responses, "v1:bike", "bike-discount", market)
	putResponse(t, responses, "v1:other", "other-provider", market)
	putSession(t, sessions, "bike-discount", market)

	result, err := service.ClearProviderResponses(ctx, "bike-discount")
	if err != nil || result.EntriesDeleted != 1 || result.BytesReleased <= 0 {
		t.Fatalf("ClearProviderResponses = %#v, %v", result, err)
	}
	if entries, err := responses.ListByProvider(ctx, "other-provider"); err != nil || len(entries) != 1 {
		t.Fatalf("retained provider responses = %d, %v", len(entries), err)
	}
	if _, err := sessions.Get(ctx, "bike-discount", market); err != nil {
		t.Fatalf("session after provider response clear: %v", err)
	}

	result, err = service.ClearResponses(ctx)
	if err != nil || result.EntriesDeleted != 1 || result.BytesReleased <= 0 {
		t.Fatalf("ClearResponses = %#v, %v", result, err)
	}
	if _, err := sessions.Get(ctx, "bike-discount", market); err != nil {
		t.Fatalf("session after all response clear: %v", err)
	}
	putResponse(t, responses, "v1:retained", "bike-discount", market)

	sessionResult, err := service.ClearSession(ctx, "bike-discount", market)
	if err != nil || !sessionResult.Deleted {
		t.Fatalf("ClearSession = %#v, %v", sessionResult, err)
	}
	if entries, err := responses.ListByProvider(ctx, "bike-discount"); err != nil || len(entries) != 1 {
		t.Fatalf("responses after session clear = %d, %v", len(entries), err)
	}
	sessionResult, err = service.ClearSession(ctx, "bike-discount", market)
	if err != nil || sessionResult.Deleted {
		t.Fatalf("second ClearSession = %#v, %v", sessionResult, err)
	}
}

func TestServiceWithSQLitePrunesExpiredThenLRU(t *testing.T) {
	_, responses, sessions, _ := openService(t)
	market := testMarket()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	expired := putResponseAt(t, responses, "v1:expired", "shop", market, now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	old := putResponseAt(t, responses, "v1:old", "shop", market, now.Add(-2*time.Hour), now.Add(time.Hour))
	newest := putResponseAt(t, responses, "v1:new", "shop", market, now.Add(-time.Hour), now.Add(time.Hour))

	cacheService, err := cache.NewService(responses, fixedClock(now), cache.Limits{
		MaxSize: config.ByteSize(newest.SizeBytes), MaxResponseSize: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := maintenance.NewService(cacheService, sessions)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.PruneResponses(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.EntriesDeleted != 2 || result.BytesReleased != expired.SizeBytes+old.SizeBytes {
		t.Fatalf("PruneResponses = %#v", result)
	}
	entries, err := responses.ListByProvider(t.Context(), "shop")
	if err != nil || len(entries) != 1 || entries[0].Key != "v1:new" {
		t.Fatalf("remaining responses = %#v, %v", entries, err)
	}
}

func openService(t *testing.T) (*maintenance.Service, *sqlite.RawResponseRepository, *sqlite.BrowserSessionRepository, *sqlite.Database) {
	t.Helper()
	database, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	responses, err := sqlite.NewRawResponseRepository(database, 4096)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := sqlite.NewBrowserSessionRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	cacheService, err := cache.NewService(responses, fixedClock(time.Now().UTC()), cache.Limits{MaxSize: 4096, MaxResponseSize: 4096})
	if err != nil {
		t.Fatal(err)
	}
	service, err := maintenance.NewService(cacheService, sessions)
	if err != nil {
		t.Fatal(err)
	}
	return service, responses, sessions, database
}

func putResponse(t *testing.T, repository *sqlite.RawResponseRepository, key, providerName string, market provider.Market) cache.Entry {
	t.Helper()
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	return putResponseAt(t, repository, key, providerName, market, now, now.Add(24*time.Hour))
}

func putResponseAt(t *testing.T, repository *sqlite.RawResponseRepository, key, providerName string, market provider.Market, storedAt, expiresAt time.Time) cache.Entry {
	t.Helper()
	entry, err := repository.Put(t.Context(), cache.Entry{
		Key: key, Provider: providerName, Market: market, Method: "GET",
		URL: "https://example.test/item", StatusCode: 200,
		SafeHeaders: map[string][]string{"Content-Type": {"text/html"}}, Body: []byte("body"),
		Encoding: cache.EncodingIdentity, StoredAt: storedAt, ExpiresAt: expiresAt, AccessedAt: storedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func putSession(t *testing.T, repository *sqlite.BrowserSessionRepository, providerName string, market provider.Market) {
	t.Helper()
	_, err := repository.Put(t.Context(), session.Record{
		Provider: providerName, Market: market, State: session.State{}, UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func testMarket() provider.Market {
	return provider.Market{Country: "DE", Language: "en", Currency: "EUR"}
}
