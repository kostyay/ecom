package cli

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreapp "github.com/kostyay/ecom/internal/app"
	"github.com/kostyay/ecom/internal/cache"
	"github.com/kostyay/ecom/internal/output"
	"github.com/kostyay/ecom/internal/session"
	"github.com/kostyay/ecom/internal/storage/sqlite"
	"github.com/kostyay/ecom/provider"
)

func TestCacheClearUsesOnlyExplicitProviderScope(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "cache.db")
	seedMaintenanceData(t, databasePath)
	resolveCalls := 0
	factory := coreapp.NewFactory(func(string) (provider.Provider, error) {
		resolveCalls++
		return nil, errors.New("provider resolution must not run")
	})

	result := runProviderHelpWithCache(t, factory, databasePath, "cache", "clear", "--provider", "fixture")
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("provider clear = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	var envelope struct {
		Provider string                         `json:"provider"`
		Data     output.ResponseMaintenanceData `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Provider != "fixture" || envelope.Data.Scope != "provider" || envelope.Data.EntriesDeleted != 1 || envelope.Data.BytesReleased == 0 {
		t.Errorf("provider clear output = %#v", envelope)
	}
	responses, sessions, database := openMaintenanceRepositories(t, databasePath)
	defer database.Close()
	if entries, err := responses.ListByProvider(t.Context(), "other"); err != nil || len(entries) != 1 {
		t.Errorf("other provider responses = %d, %v", len(entries), err)
	}
	market := maintenanceMarket()
	if _, err := sessions.Get(t.Context(), "fixture", market); err != nil {
		t.Errorf("session after response clear: %v", err)
	}
	if resolveCalls != 0 {
		t.Errorf("provider resolver calls = %d, want 0", resolveCalls)
	}
}

func TestCacheClearWithoutExplicitProviderClearsAllResponses(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "cache.db")
	seedMaintenanceData(t, databasePath)
	factory := coreapp.NewFactory(func(string) (provider.Provider, error) {
		return nil, errors.New("provider resolution must not run")
	})

	result := runProviderHelpWithCache(t, factory, databasePath, "cache", "clear", "-o", "table")
	if result.status != 0 || result.stderr != "" || !strings.Contains(result.stdout, "Entries deleted:  2") {
		t.Fatalf("global clear = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	responses, sessions, database := openMaintenanceRepositories(t, databasePath)
	defer database.Close()
	if entries, err := responses.ListByProvider(t.Context(), "other"); err != nil || len(entries) != 0 {
		t.Errorf("responses after global clear = %d, %v", len(entries), err)
	}
	if _, err := sessions.Get(t.Context(), "fixture", maintenanceMarket()); err != nil {
		t.Errorf("session after global response clear: %v", err)
	}
}

func TestCachePruneReportsJSONPathAndRejectsProviderScope(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "cache.db")
	responses, _, database := openMaintenanceRepositories(t, databasePath)
	now := time.Now().UTC()
	putCLIResponse(t, responses, "expired", "fixture", now.Add(-2*time.Hour), now.Add(-time.Hour))
	putCLIResponse(t, responses, "fresh", "fixture", now, now.Add(time.Hour))
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	factory := coreapp.NewFactory(func(string) (provider.Provider, error) { return nil, errors.New("unused") })

	result := runProviderHelpWithCache(t, factory, databasePath, "cache", "prune", "-o", `jsonpath={.data.entries_deleted}`)
	if result.status != 0 || result.stderr != "" || result.stdout != "1" {
		t.Fatalf("prune = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	invalid := runProviderHelpWithCache(t, factory, databasePath, "cache", "prune", "--provider", "fixture")
	if invalid.status != 1 || !strings.Contains(invalid.stderr, "--provider cannot be used") {
		t.Errorf("provider prune = status %d, stderr %q", invalid.status, invalid.stderr)
	}
}

func TestProviderSessionClearDeletesOnlyExactSession(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "cache.db")
	seedMaintenanceData(t, databasePath)
	factory := coreapp.NewFactory(func(string) (provider.Provider, error) { return nil, errors.New("unused") })

	result := runProviderHelpWithCache(t, factory, databasePath, "provider", "session", "clear", "fixture")
	if result.status != 0 || result.stderr != "" || !strings.Contains(result.stdout, `"deleted":true`) {
		t.Fatalf("session clear = status %d, stdout %q, stderr %q", result.status, result.stdout, result.stderr)
	}
	responses, sessions, database := openMaintenanceRepositories(t, databasePath)
	defer database.Close()
	if _, err := sessions.Get(t.Context(), "fixture", maintenanceMarket()); !errors.Is(err, session.ErrStateNotFound) {
		t.Errorf("session get error = %v", err)
	}
	if entries, err := responses.ListByProvider(t.Context(), "fixture"); err != nil || len(entries) != 1 {
		t.Errorf("responses after session clear = %d, %v", len(entries), err)
	}

	conflict := runProviderHelpWithCache(t, factory, databasePath, "provider", "session", "clear", "fixture", "--provider", "other")
	assertErrorCode(t, conflict, provider.ErrorCodeProviderConflict)
}

func seedMaintenanceData(t *testing.T, databasePath string) {
	t.Helper()
	responses, sessions, database := openMaintenanceRepositories(t, databasePath)
	now := time.Now().UTC()
	putCLIResponse(t, responses, "fixture-response", "fixture", now, now.Add(time.Hour))
	putCLIResponse(t, responses, "other-response", "other", now, now.Add(time.Hour))
	if _, err := sessions.Put(context.Background(), session.Record{
		Provider: "fixture", Market: maintenanceMarket(), State: session.State{}, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
}

func openMaintenanceRepositories(t *testing.T, databasePath string) (*sqlite.RawResponseRepository, *sqlite.BrowserSessionRepository, *sqlite.Database) {
	t.Helper()
	database, err := sqlite.Open(t.Context(), databasePath)
	if err != nil {
		t.Fatal(err)
	}
	responses, err := sqlite.NewRawResponseRepository(database, 1024*1024)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	sessions, err := sqlite.NewBrowserSessionRepository(database)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	return responses, sessions, database
}

func putCLIResponse(t *testing.T, repository *sqlite.RawResponseRepository, key, providerName string, storedAt, expiresAt time.Time) {
	t.Helper()
	_, err := repository.Put(t.Context(), cache.Entry{
		Key: key, Provider: providerName, Market: maintenanceMarket(), Method: "GET",
		URL: "https://example.test/" + key, StatusCode: 200,
		SafeHeaders: map[string][]string{"Content-Type": {"text/html"}}, Body: []byte("body"),
		Encoding: cache.EncodingIdentity, StoredAt: storedAt, ExpiresAt: expiresAt, AccessedAt: storedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func maintenanceMarket() provider.Market {
	return provider.Market{Country: "CA", Language: "fr", Currency: "CAD"}
}
