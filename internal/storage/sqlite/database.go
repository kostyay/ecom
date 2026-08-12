// Package sqlite provides the application's shared SQLite storage foundation.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	// Register the pure-Go SQLite database/sql driver.
	_ "modernc.org/sqlite"
)

const (
	driverName         = "sqlite"
	defaultBusyTimeout = 5 * time.Second
	maxOpenConnections = 4
)

// Database is the shared SQLite database used by application repositories.
type Database struct {
	sql  *sql.DB
	path string
}

// Open opens path and applies all pending schema migrations.
func Open(ctx context.Context, path string) (*Database, error) {
	return openWithMigrations(ctx, path, schemaMigrations)
}

// Close closes the database and its connections.
func (database *Database) Close() error {
	return database.sql.Close()
}

// Path returns the absolute path of the database file.
func (database *Database) Path() string {
	return database.path
}

// SchemaVersion returns the current database schema version.
func (database *Database) SchemaVersion(ctx context.Context) (int, error) {
	version, err := readSchemaVersion(ctx, database.sql)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

func openWithMigrations(ctx context.Context, path string, migrations []migration) (*Database, error) {
	absolutePath, err := preparePath(path)
	if err != nil {
		return nil, err
	}

	databaseSQL, err := sql.Open(driverName, dataSourceName(absolutePath))
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	databaseSQL.SetMaxOpenConns(maxOpenConnections)
	databaseSQL.SetMaxIdleConns(maxOpenConnections)

	database := &Database{sql: databaseSQL, path: absolutePath}
	if err := database.initialize(ctx, migrations); err != nil {
		return nil, errors.Join(err, databaseSQL.Close())
	}
	return database, nil
}

func preparePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("SQLite database path must not be empty")
	}

	absolutePath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve SQLite database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o700); err != nil {
		return "", fmt.Errorf("create SQLite database directory: %w", err)
	}
	// The caller selects this explicit database path. Opening it is the purpose of this package.
	file, err := os.OpenFile(absolutePath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("create SQLite database file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close SQLite database file: %w", err)
	}
	return absolutePath, nil
}

func dataSourceName(path string) string {
	address := &url.URL{Scheme: "file", Path: path}
	query := address.Query()
	query.Add("_pragma", "busy_timeout("+strconv.FormatInt(defaultBusyTimeout.Milliseconds(), 10)+")")
	query.Add("_pragma", "foreign_keys(ON)")
	address.RawQuery = query.Encode()
	return address.String()
}

func (database *Database) initialize(ctx context.Context, migrations []migration) error {
	if err := database.sql.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to SQLite database: %w", err)
	}

	var journalMode string
	if err := database.sql.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		return fmt.Errorf("enable SQLite WAL mode: %w", err)
	}
	if journalMode != "wal" {
		return fmt.Errorf("enable SQLite WAL mode: SQLite selected %q", journalMode)
	}
	if err := applyMigrations(ctx, database.sql, migrations); err != nil {
		return fmt.Errorf("apply SQLite migrations: %w", err)
	}
	return nil
}
