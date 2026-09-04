package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestOpenCreatesDatabaseAndAppliesMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.db")
	database := openTestDatabase(t, path)

	if database.Path() != path {
		t.Errorf("Path() = %q, want %q", database.Path(), path)
	}
	assertSchemaVersion(t, database, len(schemaMigrations))
	assertTableExists(t, database.sql, "raw_responses")
	assertTableExists(t, database.sql, "browser_sessions")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("database permissions = %o, want no group or other permissions", info.Mode().Perm())
	}
	assertPragma(t, database.sql, "journal_mode", "wal")
	assertPragma(t, database.sql, "foreign_keys", "1")
	assertPragma(t, database.sql, "busy_timeout", "5000")
}

func TestOpenMigratesOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	oldDatabase, err := openWithMigrations(t.Context(), path, schemaMigrations[:1])
	if err != nil {
		t.Fatalf("create old database: %v", err)
	}
	assertSchemaVersion(t, oldDatabase, 1)
	if err := oldDatabase.Close(); err != nil {
		t.Fatalf("close old database: %v", err)
	}

	database := openTestDatabase(t, path)
	assertSchemaVersion(t, database, len(schemaMigrations))
	assertTableExists(t, database.sql, "raw_responses")
	assertTableExists(t, database.sql, "browser_sessions")
}

func TestMigrationFailureRollsBackSchemaAndVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	migrations := []migration{
		{version: 1, name: "base", sql: "CREATE TABLE stable (id INTEGER PRIMARY KEY);"},
		{version: 2, name: "broken", sql: "CREATE TABLE partial (id INTEGER); INVALID SQL;"},
	}
	_, err := openWithMigrations(t.Context(), path, migrations)
	if err == nil || !strings.Contains(err.Error(), "run migration 2 (broken)") {
		t.Fatalf("openWithMigrations error = %v, want migration failure", err)
	}

	rawDatabase, err := sql.Open(driverName, dataSourceName(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rawDatabase.Close(); err != nil {
			t.Errorf("close raw database: %v", err)
		}
	})
	version, err := readSchemaVersion(t.Context(), rawDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if version != 0 {
		t.Errorf("schema version = %d, want 0", version)
	}
	assertTableMissing(t, rawDatabase, "stable")
	assertTableMissing(t, rawDatabase, "partial")
}

func TestWALAllowsReadersDuringWrite(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "state.db"))
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	readerOne, err := database.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer readerOne.Rollback()
	readerTwo, err := database.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer readerTwo.Rollback()

	for _, reader := range []*sql.Tx{readerOne, readerTwo} {
		var count int
		if err := reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM raw_responses").Scan(&count); err != nil {
			t.Fatalf("read before write: %v", err)
		}
	}

	_, err = database.sql.ExecContext(ctx, `
INSERT INTO raw_responses (
    cache_key, provider, market_country, market_language, market_currency,
    request_method, request_url, status_code, headers_json, body, encoding,
    stored_at, expires_at, accessed_at, size_bytes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"key", "provider", "DE", "en", "EUR", "GET", "https://example.test", 200,
		[]byte("{}"), []byte("body"), "identity", 1, 2, 1, 4,
	)
	if err != nil {
		t.Fatalf("write while readers are active: %v", err)
	}

	var count int
	if err := database.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM raw_responses").Scan(&count); err != nil {
		t.Fatalf("read after write: %v", err)
	}
	if count != 1 {
		t.Errorf("row count = %d, want 1", count)
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	_, err := Open(t.Context(), "")
	if err == nil || !strings.Contains(err.Error(), "path must not be empty") {
		t.Fatalf("Open error = %v, want empty path error", err)
	}
}

func openTestDatabase(t *testing.T, path string) *Database {
	t.Helper()
	database, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return database
}

func assertSchemaVersion(t *testing.T, database *Database, want int) {
	t.Helper()
	version, err := database.SchemaVersion(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if version != want {
		t.Errorf("schema version = %d, want %d", version, want)
	}
}

func assertTableExists(t *testing.T, database *sql.DB, name string) {
	t.Helper()
	var found string
	err := database.QueryRow("SELECT name FROM sqlite_schema WHERE type = 'table' AND name = ?", name).Scan(&found)
	if err != nil {
		t.Fatalf("find table %q: %v", name, err)
	}
}

func assertTableMissing(t *testing.T, database *sql.DB, name string) {
	t.Helper()
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = ?", name).Scan(&count); err != nil {
		t.Fatalf("find table %q: %v", name, err)
	}
	if count != 0 {
		t.Errorf("table %q exists after rollback", name)
	}
}

func assertPragma(t *testing.T, database *sql.DB, name, want string) {
	t.Helper()
	var value string
	if err := database.QueryRow("PRAGMA " + name).Scan(&value); err != nil {
		t.Fatalf("read PRAGMA %s: %v", name, err)
	}
	if value != want {
		t.Errorf("PRAGMA %s = %q, want %q", name, value, want)
	}
}
