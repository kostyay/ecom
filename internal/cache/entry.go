package cache

import (
	"context"
	"errors"
	"time"

	"github.com/kostyay/ecom/provider"
)

// ErrEntryNotFound means that storage has no raw response for a cache key.
var ErrEntryNotFound = errors.New("cache entry not found")

// Encoding identifies how Body is stored. Consumers must decode it before
// they give the raw response to a provider.
type Encoding string

const (
	// EncodingIdentity means that Body is stored without compression.
	EncodingIdentity Encoding = "identity"
	// EncodingGzip means that Body is stored with gzip compression.
	EncodingGzip Encoding = "gzip"
)

// Entry is one successful raw response and the safe request metadata needed
// to inspect and maintain it. It never contains normalized provider results.
type Entry struct {
	Key         string
	Provider    string
	Market      provider.Market
	Method      string
	URL         string
	StatusCode  int
	SafeHeaders map[string][]string
	Body        []byte
	Encoding    Encoding
	StoredAt    time.Time
	ExpiresAt   time.Time
	AccessedAt  time.Time
	SizeBytes   int64
}

// Repository stores successful raw responses.
type Repository interface {
	Put(context.Context, Entry) (Entry, error)
	Get(context.Context, string, time.Time) (Entry, error)
	ListByProvider(context.Context, string) ([]Entry, error)
}

// PruneResult reports raw-response rows and stored bytes removed by one prune.
type PruneResult struct {
	EntriesDeleted int64
	BytesDeleted   int64
}

// MaintenanceRepository removes raw responses without changing browser state.
type MaintenanceRepository interface {
	DeleteExpired(context.Context, time.Time, int) (PruneResult, error)
	Prune(context.Context, time.Time, int64) (PruneResult, error)
	DeleteAll(context.Context) (PruneResult, error)
	DeleteByProvider(context.Context, string) (PruneResult, error)
}
