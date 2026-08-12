// Package output defines provider-neutral command output.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kostyay/ecom/provider"
)

// SchemaVersion is the current stable data-envelope version.
const SchemaVersion = "1"

// Envelope is the stable result shape for provider data commands. Data keeps
// its typed Go value until the envelope is encoded. Thus, callers must not
// pre-encode data as JSON.
type Envelope struct {
	SchemaVersion     string             `json:"schema_version"`
	Provider          string             `json:"provider"`
	Market            provider.Market    `json:"market"`
	Data              any                `json:"data"`
	Page              *provider.PageInfo `json:"page,omitempty"`
	Cache             *CacheMetadata     `json:"cache,omitempty"`
	Warnings          []provider.Warning `json:"warnings"`
	TransportAttempts []TransportAttempt `json:"transport_attempts,omitempty"`
}

// Metadata contains resource metadata collected while one command runs. A
// command that has no resource metadata can use the zero value.
type Metadata struct {
	Resources []ResourceMetadata
}

// ResourceMetadata contains safe metadata for one resource used by a command.
type ResourceMetadata struct {
	Cache    provider.CacheMetadata
	Attempts []provider.TransportAttempt
}

// CacheMetadata summarizes all resources used to make a command result. Hit
// is true only when every resource was a cache hit. Stale is true when one or
// more returned resources were stale. Age is the greatest resource age and
// TTL is the smallest positive resource TTL.
type CacheMetadata struct {
	Hit           bool       `json:"hit"`
	Stale         bool       `json:"stale,omitempty"`
	StoredAt      *time.Time `json:"stored_at,omitempty"`
	AgeSeconds    int64      `json:"age_seconds"`
	TTLSeconds    int64      `json:"ttl_seconds"`
	ResourceCount int        `json:"resource_count"`
	HitCount      int        `json:"hit_count"`
	StaleCount    int        `json:"stale_count,omitempty"`
}

// TransportAttempt is the safe public form of a Core transport attempt.
type TransportAttempt struct {
	Mode       provider.TransportMode  `json:"mode"`
	Outcome    provider.AttemptOutcome `json:"outcome"`
	Code       provider.ErrorCode      `json:"code,omitempty"`
	DurationMS int64                   `json:"duration_ms,omitempty"`
}

// ListingData contains the typed entries in one result page.
type ListingData[T any] struct {
	Items        []T           `json:"items"`
	SearchMethod string        `json:"search_method,omitempty"`
	ProviderData provider.Data `json:"provider_data,omitempty"`
}

// ItemData contains a full item result.
type ItemData struct {
	Item         provider.ItemDetail `json:"item"`
	ProviderData provider.Data       `json:"provider_data,omitempty"`
}

// ResponseMaintenanceData reports one response-cache maintenance action.
type ResponseMaintenanceData struct {
	Operation      string `json:"operation"`
	Scope          string `json:"scope"`
	EntriesDeleted int64  `json:"entries_deleted"`
	BytesReleased  int64  `json:"bytes_released"`
}

// SessionMaintenanceData reports one exact browser-session deletion.
type SessionMaintenanceData struct {
	Operation string `json:"operation"`
	Deleted   bool   `json:"deleted"`
}

// New creates an envelope for help, filter, maintenance, and other results.
// Data remains a typed value and is encoded with the envelope.
func New(providerName string, market provider.Market, data any, warnings []provider.Warning, metadata Metadata) Envelope {
	return newEnvelope(providerName, market, data, nil, warnings, metadata)
}

// NewListing creates an envelope for one typed listing page.
func NewListing[T any](providerName string, market provider.Market, items []T, page provider.PageInfo, warnings []provider.Warning, providerData provider.Data, metadata Metadata) Envelope {
	data := ListingData[T]{Items: nonNil(items), ProviderData: providerData}
	return newEnvelope(providerName, market, data, &page, warnings, metadata)
}

// NewSearchedListing creates a listing and reports how text search was done.
func NewSearchedListing[T any](providerName string, market provider.Market, items []T, page provider.PageInfo, warnings []provider.Warning, providerData provider.Data, searchMethod string, metadata Metadata) Envelope {
	data := ListingData[T]{Items: nonNil(items), SearchMethod: searchMethod, ProviderData: providerData}
	return newEnvelope(providerName, market, data, &page, warnings, metadata)
}

// NewProductListing creates an envelope from a provider product page.
func NewProductListing(providerName string, market provider.Market, result provider.ProductPage, metadata Metadata) Envelope {
	return NewListing(providerName, market, result.Items, result.Page, result.Warnings, result.ProviderData, metadata)
}

// NewItem creates an envelope from a provider item result.
func NewItem(providerName string, market provider.Market, result provider.ItemResult, metadata Metadata) Envelope {
	data := ItemData{Item: result.Item, ProviderData: result.ProviderData}
	return newEnvelope(providerName, market, data, nil, result.Warnings, metadata)
}

func newEnvelope(providerName string, market provider.Market, data any, page *provider.PageInfo, warnings []provider.Warning, metadata Metadata) Envelope {
	cache, attempts := aggregateMetadata(metadata.Resources)
	return Envelope{
		SchemaVersion:     SchemaVersion,
		Provider:          providerName,
		Market:            market,
		Data:              data,
		Page:              page,
		Cache:             cache,
		Warnings:          nonNil(warnings),
		TransportAttempts: attempts,
	}
}

func aggregateMetadata(resources []ResourceMetadata) (*CacheMetadata, []TransportAttempt) {
	if len(resources) == 0 {
		return nil, nil
	}

	cache := &CacheMetadata{ResourceCount: len(resources)}
	var attempts []TransportAttempt
	for _, resource := range resources {
		if resource.Cache.Hit {
			cache.HitCount++
		}
		if resource.Cache.Stale {
			cache.Stale = true
			cache.StaleCount++
		}
		if !resource.Cache.StoredAt.IsZero() && (cache.StoredAt == nil || resource.Cache.StoredAt.Before(*cache.StoredAt)) {
			storedAt := resource.Cache.StoredAt
			cache.StoredAt = &storedAt
		}
		cache.AgeSeconds = max(cache.AgeSeconds, durationSeconds(resource.Cache.Age))
		ttl := durationSeconds(resource.Cache.TTL)
		if ttl > 0 && (cache.TTLSeconds == 0 || ttl < cache.TTLSeconds) {
			cache.TTLSeconds = ttl
		}

		for _, attempt := range resource.Attempts {
			attempts = append(attempts, TransportAttempt{
				Mode:       attempt.Mode,
				Outcome:    attempt.Outcome,
				Code:       attempt.Code,
				DurationMS: attempt.Duration.Milliseconds(),
			})
		}
	}
	cache.Hit = cache.HitCount == cache.ResourceCount
	return cache, attempts
}

func durationSeconds(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	return int64(value / time.Second)
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

// WriteJSON writes exactly one JSON document followed by one newline.
func WriteJSON(writer io.Writer, envelope Envelope) error {
	if writer == nil {
		return errors.New("output writer is required")
	}
	if err := json.NewEncoder(writer).Encode(envelope); err != nil {
		return fmt.Errorf("write JSON output: %w", err)
	}
	return nil
}

// Write renders an envelope in the selected output format. JSON remains the
// default machine-readable format. JSONPath rendering is implemented by the
// JSONPath output component.
func Write(writer io.Writer, envelope Envelope, selection Selection) error {
	switch selection.Mode {
	case "", ModeJSON:
		return WriteJSON(writer, envelope)
	case ModeTable:
		return WriteTable(writer, envelope)
	case ModeJSONPath:
		return WriteJSONPath(writer, envelope, selection.Template)
	default:
		return fmt.Errorf("unsupported output mode %q", selection.Mode)
	}
}

// Mode selects a data-command renderer.
type Mode string

const (
	// ModeJSON writes the stable JSON envelope.
	ModeJSON Mode = "json"
	// ModeTable writes a human-readable table.
	ModeTable Mode = "table"
	// ModeJSONPath applies a kubectl-style JSONPath template.
	ModeJSONPath Mode = "jsonpath"
)

// Selection is a parsed -o or --output value. An empty value selects JSON.
type Selection struct {
	Mode     Mode
	Template string
}

// ParseMode parses the common data-command output option. Template execution
// is implemented separately from this stable envelope foundation.
func ParseMode(value string) (Selection, error) {
	value = strings.TrimSpace(value)
	switch value {
	case "", string(ModeJSON):
		return Selection{Mode: ModeJSON}, nil
	case string(ModeTable):
		return Selection{Mode: ModeTable}, nil
	}

	template, found := strings.CutPrefix(value, string(ModeJSONPath)+"=")
	if !found || template == "" {
		return Selection{}, fmt.Errorf("unsupported output format %q", value)
	}
	return Selection{Mode: ModeJSONPath, Template: template}, nil
}
