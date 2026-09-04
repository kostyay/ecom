package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kostyay/ecom/internal/session"
	"github.com/kostyay/ecom/provider"
)

func TestBrowserSessionRepositoryRoundTrip(t *testing.T) {
	repository, _ := openBrowserSessionRepository(t)
	record := testBrowserSessionRecord("bike-discount", testSessionMarket())

	stored, err := repository.Put(t.Context(), record)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := repository.Get(t.Context(), record.Provider, record.Market)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(got, stored) {
		t.Errorf("Get = %#v, want %#v", got, stored)
	}

	*record.State.Cookies[0].Expires = 1
	record.State.Origins[0].LocalStorage[0].Value = "changed by caller"
	if reflect.DeepEqual(record, stored) {
		t.Fatal("Put result shares mutable session state with its input")
	}
}

func TestBrowserSessionRepositoryPutReplacesWholeState(t *testing.T) {
	repository, _ := openBrowserSessionRepository(t)
	record := testBrowserSessionRecord("bike-discount", testSessionMarket())
	if _, err := repository.Put(t.Context(), record); err != nil {
		t.Fatal(err)
	}

	replacement := record
	replacement.State = session.State{Cookies: []session.Cookie{{
		Name: "replacement", Value: "value", Domain: "bike-discount.de", Path: "/", SameSite: session.SameSiteLax,
	}}}
	replacement.UpdatedAt = record.UpdatedAt.Add(time.Hour)
	stored, err := repository.Put(t.Context(), replacement)
	if err != nil {
		t.Fatalf("Put replacement: %v", err)
	}
	got, err := repository.Get(t.Context(), record.Provider, record.Market)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, stored) {
		t.Errorf("Get replacement = %#v, want %#v", got, stored)
	}
	if len(got.State.Cookies) != 1 || got.State.Cookies[0].Name != "replacement" || len(got.State.Origins) != 0 {
		t.Errorf("replacement retained old state: %#v", got.State)
	}
}

func TestBrowserSessionRepositoryMissing(t *testing.T) {
	repository, _ := openBrowserSessionRepository(t)
	_, err := repository.Get(t.Context(), "bike-discount", testSessionMarket())
	if !errors.Is(err, session.ErrStateNotFound) {
		t.Fatalf("Get error = %v, want ErrStateNotFound", err)
	}
}

func TestBrowserSessionRepositoryIsolatesProviderAndExactMarket(t *testing.T) {
	repository, _ := openBrowserSessionRepository(t)
	market := testSessionMarket()
	otherMarket := market
	otherMarket.Language = "de"
	records := []session.Record{
		testBrowserSessionRecord("bike-discount", market),
		testBrowserSessionRecord("other-provider", market),
		testBrowserSessionRecord("bike-discount", otherMarket),
	}
	for index := range records {
		records[index].State.Cookies[0].Value = records[index].Provider + records[index].Market.Language
		if _, err := repository.Put(t.Context(), records[index]); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range records {
		got, err := repository.Get(t.Context(), want.Provider, want.Market)
		if err != nil {
			t.Fatal(err)
		}
		if got.State.Cookies[0].Value != want.State.Cookies[0].Value {
			t.Errorf("Get(%q, %#v) cookie = %q, want %q", want.Provider, want.Market, got.State.Cookies[0].Value, want.State.Cookies[0].Value)
		}
	}
}

func TestBrowserSessionRepositoryRejectsInvalidRecords(t *testing.T) {
	valid := testBrowserSessionRecord("bike-discount", testSessionMarket())
	tests := []struct {
		name   string
		change func(*session.Record)
		want   string
	}{
		{name: "provider", change: func(record *session.Record) { record.Provider = "Bike Discount" }, want: "provider"},
		{name: "market", change: func(record *session.Record) { record.Market.Currency = "eur" }, want: "market"},
		{name: "time", change: func(record *session.Record) { record.UpdatedAt = time.Time{} }, want: "update time"},
		{name: "cookie name", change: func(record *session.Record) { record.State.Cookies[0].Name = "bad;name" }, want: "name"},
		{name: "cookie domain", change: func(record *session.Record) { record.State.Cookies[0].Domain = "https://example.test" }, want: "domain"},
		{name: "cookie path", change: func(record *session.Record) { record.State.Cookies[0].Path = "products" }, want: "path"},
		{name: "same site", change: func(record *session.Record) { record.State.Cookies[0].SameSite = "sometimes" }, want: "same-site"},
		{name: "expiry", change: func(record *session.Record) { invalid := int64(-1); record.State.Cookies[0].Expires = &invalid }, want: "expiry"},
		{name: "origin URL", change: func(record *session.Record) { record.State.Origins[0].Origin = "https://example.test/path" }, want: "origin"},
		{name: "storage name", change: func(record *session.Record) { record.State.Origins[0].LocalStorage[0].Name = "" }, want: "local-storage"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, _ := openBrowserSessionRepository(t)
			record := cloneSessionTestRecord(valid)
			test.change(&record)
			_, err := repository.Put(t.Context(), record)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Put error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestBrowserSessionRepositoryDeleteUsesExactScope(t *testing.T) {
	repository, _ := openBrowserSessionRepository(t)
	market := testSessionMarket()
	otherMarket := market
	otherMarket.Currency = "USD"
	for _, record := range []session.Record{
		testBrowserSessionRecord("bike-discount", market),
		testBrowserSessionRecord("bike-discount", otherMarket),
		testBrowserSessionRecord("other-provider", market),
	} {
		if _, err := repository.Put(t.Context(), record); err != nil {
			t.Fatal(err)
		}
	}

	deleted, err := repository.Delete(t.Context(), "bike-discount", market)
	if err != nil || !deleted {
		t.Fatal(err)
	}
	if _, err := repository.Get(t.Context(), "bike-discount", market); !errors.Is(err, session.ErrStateNotFound) {
		t.Fatalf("deleted state error = %v, want ErrStateNotFound", err)
	}
	for _, scope := range []struct {
		provider string
		market   provider.Market
	}{{"bike-discount", otherMarket}, {"other-provider", market}} {
		if _, err := repository.Get(t.Context(), scope.provider, scope.market); err != nil {
			t.Errorf("Get retained state: %v", err)
		}
	}
	deleted, err = repository.Delete(t.Context(), "bike-discount", market)
	if err != nil || deleted {
		t.Errorf("second Delete = %t, %v", deleted, err)
	}
}

func TestBrowserSessionRemainsAfterRawResponsesAreRemoved(t *testing.T) {
	repository, database := openBrowserSessionRepository(t)
	record := testBrowserSessionRecord("bike-discount", testSessionMarket())
	if _, err := repository.Put(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.ExecContext(t.Context(), "DELETE FROM raw_responses"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(t.Context(), record.Provider, record.Market); err != nil {
		t.Fatalf("Get after raw-response removal: %v", err)
	}
}

func TestBrowserSessionDeleteKeepsRawResponses(t *testing.T) {
	repository, database := openBrowserSessionRepository(t)
	record := testBrowserSessionRecord("bike-discount", testSessionMarket())
	if _, err := repository.Put(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	responses, err := NewRawResponseRepository(database, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := responses.Put(t.Context(), testRawResponseEntry("v1:item", record.Provider)); err != nil {
		t.Fatal(err)
	}
	deleted, err := repository.Delete(t.Context(), record.Provider, record.Market)
	if err != nil || !deleted {
		t.Fatalf("Delete = %t, %v", deleted, err)
	}
	entries, err := responses.ListByProvider(t.Context(), record.Provider)
	if err != nil || len(entries) != 1 {
		t.Fatalf("raw responses after session delete = %d, %v", len(entries), err)
	}
}

func TestBrowserSessionDeleteHonorsCancellation(t *testing.T) {
	repository, _ := openBrowserSessionRepository(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	deleted, err := repository.Delete(ctx, "bike-discount", testSessionMarket())
	if !errors.Is(err, context.Canceled) || deleted {
		t.Fatalf("Delete = %t, %v; want false, context.Canceled", deleted, err)
	}
}

func TestBrowserSessionRepositoryRejectsUnknownStoredJSONFields(t *testing.T) {
	repository, database := openBrowserSessionRepository(t)
	market := testSessionMarket()
	_, err := database.sql.ExecContext(t.Context(), `
INSERT INTO browser_sessions (
    provider, market_country, market_language, market_currency, state_json, updated_at
) VALUES (?, ?, ?, ?, ?, ?)`, "bike-discount", market.Country, market.Language, market.Currency,
		[]byte(`{"cookies":[],"origins":[],"passwords":[]}`), time.Now().UnixNano())
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.Get(t.Context(), "bike-discount", market)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Get error = %v, want unknown field error", err)
	}
}

func openBrowserSessionRepository(t *testing.T) (*BrowserSessionRepository, *Database) {
	t.Helper()
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "state.db"))
	repository, err := NewBrowserSessionRepository(database)
	if err != nil {
		t.Fatalf("NewBrowserSessionRepository: %v", err)
	}
	return repository, database
}

func testSessionMarket() provider.Market {
	return provider.Market{Country: "DE", Language: "en", Currency: "EUR"}
}

func testBrowserSessionRecord(providerName string, market provider.Market) session.Record {
	expires := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC).Unix()
	return session.Record{
		Provider: providerName,
		Market:   market,
		State: session.State{
			Cookies: []session.Cookie{{
				Name: "session-id", Value: "secret", Domain: ".bike-discount.de", Path: "/",
				Expires: &expires, HTTPOnly: true, Secure: true, SameSite: session.SameSiteLax,
			}},
			Origins: []session.Origin{{
				Origin:       "https://www.bike-discount.de",
				LocalStorage: []session.StorageEntry{{Name: "market", Value: `{"country":"DE"}`}},
			}},
		},
		UpdatedAt: time.Date(2026, 8, 12, 12, 0, 0, 123456789, time.FixedZone("test", 2*60*60)),
	}
}

func cloneSessionTestRecord(record session.Record) session.Record {
	return cloneSessionRecord(record)
}
