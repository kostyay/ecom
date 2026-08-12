package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/kostyay/ecom/internal/session"
	"github.com/kostyay/ecom/provider"
)

// BrowserSessionRepository stores portable browser state in Database.
type BrowserSessionRepository struct {
	database *Database
}

var _ session.Repository = (*BrowserSessionRepository)(nil)

// NewBrowserSessionRepository makes a repository independent from raw responses.
func NewBrowserSessionRepository(database *Database) (*BrowserSessionRepository, error) {
	if database == nil || database.sql == nil {
		return nil, errors.New("browser session repository database is required")
	}
	return &BrowserSessionRepository{database: database}, nil
}

// Put atomically inserts or replaces portable state for one provider and market.
func (repository *BrowserSessionRepository) Put(ctx context.Context, record session.Record) (session.Record, error) {
	if err := record.Validate(); err != nil {
		return session.Record{}, err
	}
	stateJSON, err := json.Marshal(record.State)
	if err != nil {
		return session.Record{}, fmt.Errorf("encode browser session state: %w", err)
	}
	record.UpdatedAt = record.UpdatedAt.UTC()

	_, err = repository.database.sql.ExecContext(ctx, `
INSERT INTO browser_sessions (
    provider, market_country, market_language, market_currency, state_json, updated_at
) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(provider, market_country, market_language, market_currency) DO UPDATE SET
    state_json = excluded.state_json,
    updated_at = excluded.updated_at`,
		record.Provider, record.Market.Country, record.Market.Language, record.Market.Currency,
		stateJSON, unixNanoseconds(record.UpdatedAt),
	)
	if err != nil {
		return session.Record{}, fmt.Errorf("store browser session for provider %q: %w", record.Provider, err)
	}
	return cloneSessionRecord(record), nil
}

// Get returns portable state for one provider and exact market.
func (repository *BrowserSessionRepository) Get(
	ctx context.Context,
	providerName string,
	market provider.Market,
) (session.Record, error) {
	lookup := session.Record{Provider: providerName, Market: market, UpdatedAt: time.Unix(0, 0)}
	if err := lookup.Validate(); err != nil {
		return session.Record{}, err
	}

	var stateJSON []byte
	var updatedAt int64
	err := repository.database.sql.QueryRowContext(ctx, `
SELECT state_json, updated_at
FROM browser_sessions
WHERE provider = ? AND market_country = ? AND market_language = ? AND market_currency = ?`,
		providerName, market.Country, market.Language, market.Currency,
	).Scan(&stateJSON, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return session.Record{}, fmt.Errorf("%w: provider %q", session.ErrStateNotFound, providerName)
	}
	if err != nil {
		return session.Record{}, fmt.Errorf("get browser session for provider %q: %w", providerName, err)
	}

	state, err := decodeSessionState(stateJSON)
	if err != nil {
		return session.Record{}, fmt.Errorf("get browser session for provider %q: %w", providerName, err)
	}
	return session.Record{
		Provider: providerName, Market: market, State: state, UpdatedAt: time.Unix(0, updatedAt).UTC(),
	}, nil
}

// Delete removes state for one provider and exact market. It is idempotent.
func (repository *BrowserSessionRepository) Delete(ctx context.Context, providerName string, market provider.Market) (bool, error) {
	lookup := session.Record{Provider: providerName, Market: market, UpdatedAt: time.Unix(0, 0)}
	if err := lookup.Validate(); err != nil {
		return false, err
	}
	result, err := repository.database.sql.ExecContext(ctx, `
DELETE FROM browser_sessions
WHERE provider = ? AND market_country = ? AND market_language = ? AND market_currency = ?`,
		providerName, market.Country, market.Language, market.Currency,
	)
	if err != nil {
		return false, fmt.Errorf("delete browser session for provider %q: %w", providerName, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count deleted browser sessions for provider %q: %w", providerName, err)
	}
	return count > 0, nil
}

func decodeSessionState(data []byte) (session.State, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state session.State
	if err := decoder.Decode(&state); err != nil {
		return session.State{}, fmt.Errorf("decode browser session state: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return session.State{}, errors.New("decode browser session state: extra JSON value")
	}
	if err := state.Validate(); err != nil {
		return session.State{}, fmt.Errorf("validate stored browser session state: %w", err)
	}
	return state, nil
}

func cloneSessionRecord(record session.Record) session.Record {
	record.State.Cookies = append([]session.Cookie(nil), record.State.Cookies...)
	for index := range record.State.Cookies {
		if expiry := record.State.Cookies[index].Expires; expiry != nil {
			copyExpiry := *expiry
			record.State.Cookies[index].Expires = &copyExpiry
		}
	}
	record.State.Origins = append([]session.Origin(nil), record.State.Origins...)
	for index := range record.State.Origins {
		record.State.Origins[index].LocalStorage = append(
			[]session.StorageEntry(nil), record.State.Origins[index].LocalStorage...,
		)
	}
	return record
}
