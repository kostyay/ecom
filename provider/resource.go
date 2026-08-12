package provider

import (
	"context"
	"time"
)

// ResourceService gets raw website resources for a provider. The Core supplies
// the implementation and applies its cache, rate limit, retry, and transport
// policies. Provider implementations must use the call context for each
// request.
type ResourceService interface {
	Fetch(context.Context, ResourceRequest) (ResourceResponse, error)
}

// ResourceRequest describes one website resource without exposing a concrete
// HTTP client or browser implementation.
type ResourceRequest struct {
	Method      string
	URL         string
	Query       []RequestValue
	Headers     []RequestValue
	Body        RequestBody
	Transport   TransportPolicy
	Market      Market
	Cache       CachePolicy
	Interactive bool
	DOM         []DOMExtraction

	// CachePartition is a non-secret identifier that separates responses which
	// can differ because of sensitive request values. For example, it can
	// identify a provider account or session without containing a credential.
	CachePartition string
}

// RequestValue is one query parameter or request header. Sensitive values are
// sent to the website but must not be included in cache keys, logs, or errors.
// A provider must not put a secret directly in ResourceRequest.URL.
type RequestValue struct {
	Name      string
	Values    []string
	Sensitive bool
}

// RequestBody is an optional raw request body. When Sensitive is true, the Core
// must not include Bytes in cache keys, logs, or errors. CachePartition must
// separate responses when a sensitive body changes the response.
type RequestBody struct {
	Bytes     []byte
	Sensitive bool
}

// TransportPolicy states a provider's transport need or preference. Required
// selects the only acceptable transport. Preferred gives the complete ordered
// list of acceptable transports; the Core does not append omitted modes. When
// both fields are empty, the Core uses its normal transport sequence.
type TransportPolicy struct {
	Required  TransportMode
	Preferred []TransportMode
}

// CachePolicy contains the cache choices for one command. Refresh bypasses a
// valid entry. StaleIfError permits an expired entry after a fresh request
// fails.
type CachePolicy struct {
	Refresh      bool
	StaleIfError bool
}

// ResourceResponse contains a raw response from a Core transport. Body is set
// for a byte response. Page is set for a rendered browser page.
type ResourceResponse struct {
	Body        []byte
	Page        *PageSnapshot
	StatusCode  int
	FinalURL    string
	SafeHeaders map[string][]string
	RetrievedAt time.Time
	Transport   TransportMode
	Cache       CacheMetadata
	// Attempts lists the safe Core transport attempts made for this response.
	// It never contains request URLs, headers, bodies, or session data.
	Attempts []TransportAttempt `json:"attempts,omitempty"`
}

// TransportAttempt is safe diagnostic metadata for one Core transport attempt.
type TransportAttempt struct {
	Mode     TransportMode  `json:"mode"`
	Outcome  AttemptOutcome `json:"outcome"`
	Code     ErrorCode      `json:"code,omitempty"`
	Duration time.Duration  `json:"duration,omitempty"`
}

// AttemptOutcome identifies the result of one Core transport attempt.
type AttemptOutcome string

const (
	// AttemptSucceeded means that the transport returned a useful response.
	AttemptSucceeded AttemptOutcome = "succeeded"
	// AttemptFailed means that the transport returned a terminal or fallback-eligible error.
	AttemptFailed AttemptOutcome = "failed"
	// AttemptUnavailable means that the transport was not configured or could not be used.
	AttemptUnavailable AttemptOutcome = "unavailable"
)

// PageSnapshot is provider-readable content from a rendered page. It does not
// expose a browser or a live page handle.
type PageSnapshot struct {
	HTML []byte              `json:"html"`
	DOM  map[string][]string `json:"dom,omitempty"`
}

// DOMExtraction is one closed, declarative browser DOM operation. Providers
// can request rendered values without receiving a live browser page or an
// unrestricted script execution interface.
type DOMExtraction struct {
	Name      string            `json:"name"`
	Selector  string            `json:"selector"`
	Kind      DOMExtractionKind `json:"kind"`
	Attribute string            `json:"attribute,omitempty"`
	All       bool              `json:"all,omitempty"`
}

// DOMExtractionKind selects the value read from matched elements.
type DOMExtractionKind string

const (
	// DOMText reads visible textContent.
	DOMText DOMExtractionKind = "text"
	// DOMHTML reads element innerHTML.
	DOMHTML DOMExtractionKind = "html"
	// DOMAttribute reads one named element attribute.
	DOMAttribute DOMExtractionKind = "attribute"
)

// CacheMetadata describes the cache entry used for a resource response.
type CacheMetadata struct {
	Hit      bool
	Stale    bool
	StoredAt time.Time
	Age      time.Duration
	TTL      time.Duration
}
