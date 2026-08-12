package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type migration struct {
	version int
	name    string
	sql     string
}

var schemaMigrations = []migration{
	{
		version: 1,
		name:    "create raw response storage",
		sql: `
CREATE TABLE raw_responses (
    cache_key TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    market_country TEXT NOT NULL,
    market_language TEXT NOT NULL,
    market_currency TEXT NOT NULL,
    request_method TEXT NOT NULL,
    request_url TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    headers_json BLOB NOT NULL,
    body BLOB NOT NULL,
    encoding TEXT NOT NULL,
    stored_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    accessed_at INTEGER NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0)
);
CREATE INDEX raw_responses_expiry_idx ON raw_responses (expires_at);
CREATE INDEX raw_responses_access_idx ON raw_responses (accessed_at);
CREATE INDEX raw_responses_provider_idx ON raw_responses (provider);
`,
	},
	{
		version: 2,
		name:    "create browser session storage",
		sql: `
CREATE TABLE browser_sessions (
    provider TEXT NOT NULL,
    market_country TEXT NOT NULL,
    market_language TEXT NOT NULL,
    market_currency TEXT NOT NULL,
    state_json BLOB NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (provider, market_country, market_language, market_currency)
);
`,
	},
}

func applyMigrations(ctx context.Context, database *sql.DB, migrations []migration) (err error) {
	if err := validateMigrations(migrations); err != nil {
		return err
	}

	connection, err := database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve migration connection: %w", err)
	}
	defer func() {
		err = errors.Join(err, connection.Close())
	}()

	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, rollbackErr := connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
			err = errors.Join(err, rollbackErr)
		}
	}()

	currentVersion, err := readSchemaVersion(ctx, connection)
	if err != nil {
		return fmt.Errorf("read current schema version: %w", err)
	}
	if currentVersion > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than supported version %d", currentVersion, len(migrations))
	}

	for _, migration := range migrations {
		if migration.version <= currentVersion {
			continue
		}
		if _, err := connection.ExecContext(ctx, migration.sql); err != nil {
			return fmt.Errorf("run migration %d (%s): %w", migration.version, migration.name, err)
		}
		pragma := fmt.Sprintf("PRAGMA user_version = %d", migration.version)
		if _, err := connection.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("record migration %d (%s): %w", migration.version, migration.name, err)
		}
	}

	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit migration transaction: %w", err)
	}
	committed = true
	return nil
}

func validateMigrations(migrations []migration) error {
	for index, migration := range migrations {
		expectedVersion := index + 1
		if migration.version != expectedVersion {
			return fmt.Errorf("migration %q has version %d, expected %d", migration.name, migration.version, expectedVersion)
		}
		if migration.name == "" || migration.sql == "" {
			return fmt.Errorf("migration %d must have a name and SQL", migration.version)
		}
	}
	return nil
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readSchemaVersion(ctx context.Context, querier rowQuerier) (int, error) {
	var version int
	if err := querier.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}
