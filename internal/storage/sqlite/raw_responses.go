package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kostyay/ecom/internal/cache"
	"github.com/kostyay/ecom/internal/config"
)

const rawResponseColumns = `cache_key, provider, market_country, market_language,
market_currency, request_method, request_url, status_code, headers_json, body,
encoding, stored_at, expires_at, accessed_at, size_bytes`

var sensitiveResponseHeaders = map[string]struct{}{
	"authorization":       {},
	"cookie":              {},
	"proxy-authorization": {},
	"set-cookie":          {},
}

// RawResponseRepository stores raw cache responses in Database.
type RawResponseRepository struct {
	database        *Database
	maxResponseSize int64
	beforeCommit    func() error
}

var _ cache.Repository = (*RawResponseRepository)(nil)
var _ cache.MaintenanceRepository = (*RawResponseRepository)(nil)

// NewRawResponseRepository makes a raw-response repository with one maximum
// size for both the response body and the complete stored entry.
func NewRawResponseRepository(database *Database, maxResponseSize config.ByteSize) (*RawResponseRepository, error) {
	if database == nil || database.sql == nil {
		return nil, errors.New("raw response repository database is required")
	}
	if maxResponseSize <= 0 {
		return nil, errors.New("raw response maximum size must be positive")
	}
	return &RawResponseRepository{database: database, maxResponseSize: int64(maxResponseSize)}, nil
}

// Put inserts or replaces an entry. SizeBytes is calculated from the exact
// values stored in SQLite and replaces any caller-supplied value.
func (repository *RawResponseRepository) Put(ctx context.Context, entry cache.Entry) (cache.Entry, error) {
	headersJSON, err := validateAndEncodeEntry(entry, repository.maxResponseSize)
	if err != nil {
		return cache.Entry{}, err
	}
	entry.StoredAt = entry.StoredAt.UTC()
	entry.ExpiresAt = entry.ExpiresAt.UTC()
	entry.AccessedAt = entry.AccessedAt.UTC()
	entry.SizeBytes = storedEntrySize(entry, headersJSON)

	_, err = repository.database.sql.ExecContext(ctx, `
INSERT INTO raw_responses (`+rawResponseColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(cache_key) DO UPDATE SET
    provider = excluded.provider,
    market_country = excluded.market_country,
    market_language = excluded.market_language,
    market_currency = excluded.market_currency,
    request_method = excluded.request_method,
    request_url = excluded.request_url,
    status_code = excluded.status_code,
    headers_json = excluded.headers_json,
    body = excluded.body,
    encoding = excluded.encoding,
    stored_at = excluded.stored_at,
    expires_at = excluded.expires_at,
    accessed_at = excluded.accessed_at,
    size_bytes = excluded.size_bytes`, entryArguments(entry, headersJSON)...)
	if err != nil {
		return cache.Entry{}, fmt.Errorf("store raw response %q: %w", entry.Key, err)
	}
	return cloneEntry(entry), nil
}

// DeleteExpired removes up to limit responses in expiry order. A zero limit
// removes all expired responses. Equal expiry times use the cache key for a
// stable order.
func (repository *RawResponseRepository) DeleteExpired(
	ctx context.Context,
	now time.Time,
	limit int,
) (cache.PruneResult, error) {
	if now.IsZero() {
		return cache.PruneResult{}, errors.New("raw response prune time is required")
	}
	if limit < 0 {
		return cache.PruneResult{}, errors.New("raw response prune limit must not be negative")
	}

	result, err := deleteExpired(ctx, repository.database.sql, now, limit)
	if err != nil {
		return cache.PruneResult{}, fmt.Errorf("delete expired raw responses: %w", err)
	}
	return result, nil
}

// PruneToSize removes least-recently-used responses until stored response
// size is at or below maxSize. Equal access times use the cache key for a
// stable order.
func (repository *RawResponseRepository) PruneToSize(ctx context.Context, maxSize int64) (cache.PruneResult, error) {
	if maxSize <= 0 {
		return cache.PruneResult{}, errors.New("raw response cache maximum size must be positive")
	}
	result, err := pruneToSize(ctx, repository.database.sql, maxSize)
	if err != nil {
		return cache.PruneResult{}, fmt.Errorf("delete LRU raw responses: %w", err)
	}
	return result, nil
}

// Prune atomically removes expired responses, then removes least-recently-used
// responses until the configured size limit is met.
func (repository *RawResponseRepository) Prune(
	ctx context.Context,
	now time.Time,
	maxSize int64,
) (result cache.PruneResult, err error) {
	if now.IsZero() {
		return cache.PruneResult{}, errors.New("raw response prune time is required")
	}
	if maxSize <= 0 {
		return cache.PruneResult{}, errors.New("raw response cache maximum size must be positive")
	}
	transaction, err := repository.database.sql.BeginTx(ctx, nil)
	if err != nil {
		return cache.PruneResult{}, fmt.Errorf("begin raw response prune: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, transaction.Rollback())
		}
	}()

	expired, err := deleteExpired(ctx, transaction, now, 0)
	if err != nil {
		return cache.PruneResult{}, fmt.Errorf("delete expired raw responses: %w", err)
	}
	lru, err := pruneToSize(ctx, transaction, maxSize)
	if err != nil {
		return cache.PruneResult{}, fmt.Errorf("delete LRU raw responses: %w", err)
	}
	result = addPruneResults(expired, lru)
	if repository.beforeCommit != nil {
		if err := repository.beforeCommit(); err != nil {
			return cache.PruneResult{}, fmt.Errorf("finish raw response prune: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return cache.PruneResult{}, fmt.Errorf("commit raw response prune: %w", err)
	}
	return result, nil
}

// DeleteAll atomically removes every raw response and reports stored bytes.
func (repository *RawResponseRepository) DeleteAll(ctx context.Context) (cache.PruneResult, error) {
	result, err := deletedSizes(ctx, repository.database.sql, "DELETE FROM raw_responses RETURNING size_bytes")
	if err != nil {
		return cache.PruneResult{}, fmt.Errorf("delete all raw responses: %w", err)
	}
	return result, nil
}

// DeleteByProvider atomically removes raw responses for one exact provider.
func (repository *RawResponseRepository) DeleteByProvider(ctx context.Context, providerName string) (cache.PruneResult, error) {
	if strings.TrimSpace(providerName) == "" || providerName != strings.TrimSpace(providerName) {
		return cache.PruneResult{}, errors.New("raw response provider is required")
	}
	result, err := deletedSizes(ctx, repository.database.sql,
		"DELETE FROM raw_responses WHERE provider = ? RETURNING size_bytes", providerName)
	if err != nil {
		return cache.PruneResult{}, fmt.Errorf("delete raw responses for provider %q: %w", providerName, err)
	}
	return result, nil
}

func deleteExpired(ctx context.Context, database queryer, now time.Time, limit int) (cache.PruneResult, error) {
	query := `
DELETE FROM raw_responses
WHERE cache_key IN (
    SELECT cache_key FROM raw_responses
    WHERE expires_at <= ?
    ORDER BY expires_at, cache_key`
	arguments := []any{unixNanoseconds(now)}
	if limit > 0 {
		query += " LIMIT ?"
		arguments = append(arguments, limit)
	}
	query += ") RETURNING size_bytes"
	return deletedSizes(ctx, database, query, arguments...)
}

func pruneToSize(ctx context.Context, database queryer, maxSize int64) (cache.PruneResult, error) {
	return deletedSizes(ctx, database, `
DELETE FROM raw_responses
WHERE cache_key IN (
    SELECT cache_key
    FROM (
        SELECT
            cache_key,
            SUM(size_bytes) OVER () AS total_size,
            COALESCE(SUM(size_bytes) OVER (
                ORDER BY accessed_at, cache_key
                ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
            ), 0) AS removed_before
        FROM raw_responses
    )
    WHERE total_size - removed_before > ?
)
RETURNING size_bytes`, maxSize)
}

func addPruneResults(first, second cache.PruneResult) cache.PruneResult {
	return cache.PruneResult{
		EntriesDeleted: first.EntriesDeleted + second.EntriesDeleted,
		BytesDeleted:   first.BytesDeleted + second.BytesDeleted,
	}
}

// Get returns an entry and moves its access time forward to accessedAt.
func (repository *RawResponseRepository) Get(ctx context.Context, key string, accessedAt time.Time) (cache.Entry, error) {
	if strings.TrimSpace(key) == "" {
		return cache.Entry{}, errors.New("raw response cache key is required")
	}
	if accessedAt.IsZero() {
		return cache.Entry{}, errors.New("raw response access time is required")
	}

	row := repository.database.sql.QueryRowContext(ctx, `
UPDATE raw_responses
SET accessed_at = MAX(accessed_at, ?)
WHERE cache_key = ?
RETURNING `+rawResponseColumns, unixNanoseconds(accessedAt), key)
	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return cache.Entry{}, fmt.Errorf("%w: %s", cache.ErrEntryNotFound, key)
	}
	if err != nil {
		return cache.Entry{}, fmt.Errorf("get raw response %q: %w", key, err)
	}
	return entry, nil
}

// ListByProvider returns provider entries in stable cache-key order. This
// inspection operation does not change their access times.
func (repository *RawResponseRepository) ListByProvider(ctx context.Context, providerName string) (entries []cache.Entry, err error) {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return nil, errors.New("raw response provider is required")
	}
	rows, err := repository.database.sql.QueryContext(ctx, `
SELECT `+rawResponseColumns+`
FROM raw_responses
WHERE provider = ?
ORDER BY cache_key`, providerName)
	if err != nil {
		return nil, fmt.Errorf("list raw responses for provider %q: %w", providerName, err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	entries = make([]cache.Entry, 0)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scan raw response for provider %q: %w", providerName, err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list raw responses for provider %q: %w", providerName, err)
	}
	return entries, nil
}

func validateAndEncodeEntry(entry cache.Entry, maxResponseSize int64) ([]byte, error) {
	if strings.TrimSpace(entry.Key) == "" || entry.Key != strings.TrimSpace(entry.Key) {
		return nil, errors.New("raw response cache key is required")
	}
	if strings.TrimSpace(entry.Provider) == "" || entry.Provider != strings.TrimSpace(entry.Provider) {
		return nil, errors.New("raw response provider is required")
	}
	if err := entry.Market.Validate(); err != nil {
		return nil, fmt.Errorf("raw response market: %w", err)
	}
	if !validMethod(entry.Method) {
		return nil, errors.New("raw response method must be a valid uppercase HTTP method")
	}
	if err := validateSafeURL(entry.URL); err != nil {
		return nil, err
	}
	if entry.StatusCode < http.StatusOK || entry.StatusCode >= http.StatusMultipleChoices {
		return nil, errors.New("raw response status must be successful")
	}
	if entry.Encoding != cache.EncodingIdentity && entry.Encoding != cache.EncodingGzip {
		return nil, errors.New("raw response encoding is not supported")
	}
	if entry.StoredAt.IsZero() || entry.ExpiresAt.IsZero() || entry.AccessedAt.IsZero() {
		return nil, errors.New("raw response timestamps are required")
	}
	if !entry.ExpiresAt.After(entry.StoredAt) {
		return nil, errors.New("raw response expiry must be after storage time")
	}
	if entry.AccessedAt.Before(entry.StoredAt) {
		return nil, errors.New("raw response access time must not be before storage time")
	}
	if int64(len(entry.Body)) > maxResponseSize {
		return nil, fmt.Errorf("raw response body size %d exceeds maximum %d", len(entry.Body), maxResponseSize)
	}
	if err := validateSafeHeaders(entry.SafeHeaders); err != nil {
		return nil, err
	}
	headersJSON, err := json.Marshal(entry.SafeHeaders)
	if err != nil {
		return nil, fmt.Errorf("encode raw response safe headers: %w", err)
	}
	return headersJSON, nil
}

func validMethod(method string) bool {
	if method == "" || method != strings.ToUpper(method) {
		return false
	}
	for index := range len(method) {
		if !isHTTPTokenByte(method[index]) {
			return false
		}
	}
	return true
}

func isHTTPTokenByte(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'A' && value <= 'Z' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(value))
}

func validateSafeURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return errors.New("raw response URL must be an absolute safe HTTP URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("raw response URL must not contain query values or a fragment")
	}
	return nil
}

func validateSafeHeaders(headers map[string][]string) error {
	for name, values := range headers {
		if !validHeaderName(name) {
			return errors.New("raw response safe header name is invalid")
		}
		if _, sensitive := sensitiveResponseHeaders[strings.ToLower(name)]; sensitive {
			return fmt.Errorf("raw response header %q is sensitive", name)
		}
		for _, value := range values {
			if strings.ContainsAny(value, "\r\n\x00") {
				return fmt.Errorf("raw response header %q has an invalid value", name)
			}
		}
	}
	return nil
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for index := range len(name) {
		value := name[index]
		if value >= 'a' && value <= 'z' {
			continue
		}
		if !isHTTPTokenByte(value) {
			return false
		}
	}
	return true
}

func storedEntrySize(entry cache.Entry, headersJSON []byte) int64 {
	return int64(len(entry.Key) + len(entry.Provider) + len(entry.Market.Country) +
		len(entry.Market.Language) + len(entry.Market.Currency) + len(entry.Method) +
		len(entry.URL) + len(headersJSON) + len(entry.Body) + len(entry.Encoding) + 5*8)
}

func entryArguments(entry cache.Entry, headersJSON []byte) []any {
	return []any{
		entry.Key, entry.Provider, entry.Market.Country, entry.Market.Language,
		entry.Market.Currency, entry.Method, entry.URL, entry.StatusCode, headersJSON,
		entry.Body, string(entry.Encoding), unixNanoseconds(entry.StoredAt),
		unixNanoseconds(entry.ExpiresAt), unixNanoseconds(entry.AccessedAt), entry.SizeBytes,
	}
}

type scanner interface {
	Scan(...any) error
}

func scanEntry(source scanner) (cache.Entry, error) {
	var entry cache.Entry
	var headersJSON []byte
	var encoding string
	var storedAt, expiresAt, accessedAt int64
	err := source.Scan(
		&entry.Key, &entry.Provider, &entry.Market.Country, &entry.Market.Language,
		&entry.Market.Currency, &entry.Method, &entry.URL, &entry.StatusCode,
		&headersJSON, &entry.Body, &encoding, &storedAt, &expiresAt, &accessedAt,
		&entry.SizeBytes,
	)
	if err != nil {
		return cache.Entry{}, err
	}
	if err := json.Unmarshal(headersJSON, &entry.SafeHeaders); err != nil {
		return cache.Entry{}, fmt.Errorf("decode safe headers: %w", err)
	}
	entry.Encoding = cache.Encoding(encoding)
	entry.StoredAt = time.Unix(0, storedAt).UTC()
	entry.ExpiresAt = time.Unix(0, expiresAt).UTC()
	entry.AccessedAt = time.Unix(0, accessedAt).UTC()
	return entry, nil
}

func unixNanoseconds(value time.Time) int64 {
	return value.UTC().UnixNano()
}

func cloneEntry(entry cache.Entry) cache.Entry {
	entry.Body = append([]byte(nil), entry.Body...)
	if entry.SafeHeaders != nil {
		headers := make(map[string][]string, len(entry.SafeHeaders))
		for name, values := range entry.SafeHeaders {
			headers[name] = append([]string(nil), values...)
		}
		entry.SafeHeaders = headers
	}
	return entry
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func deletedSizes(ctx context.Context, database queryer, query string, arguments ...any) (result cache.PruneResult, err error) {
	rows, err := database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return cache.PruneResult{}, err
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()
	for rows.Next() {
		var size int64
		if err := rows.Scan(&size); err != nil {
			return cache.PruneResult{}, err
		}
		result.EntriesDeleted++
		result.BytesDeleted += size
	}
	if err := rows.Err(); err != nil {
		return cache.PruneResult{}, err
	}
	return result, nil
}
