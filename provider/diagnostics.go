package provider

import "errors"

// ErrorCode is a stable machine-readable error identifier.
//
// ErrorCode implements error so callers can use errors.Is without matching
// error text.
type ErrorCode string

const (
	// ErrorCodeCapabilityUnavailable means that a provider does not support the requested operation.
	ErrorCodeCapabilityUnavailable ErrorCode = "capability_unavailable"
	// ErrorCodeInvalidFilter means that a filter name or value is not valid for the provider.
	ErrorCodeInvalidFilter ErrorCode = "invalid_filter"
	// ErrorCodeVariantNotFound means that no product variant matches the requested attributes.
	ErrorCodeVariantNotFound ErrorCode = "variant_not_found"
	// ErrorCodeAccessBlocked means that the provider blocked access to a resource.
	ErrorCodeAccessBlocked ErrorCode = "access_blocked"
	// ErrorCodeBrowserChallengeRequired means that a person must complete a browser challenge.
	ErrorCodeBrowserChallengeRequired ErrorCode = "browser_challenge_required"
	// ErrorCodeBrowserChallengeTimeout means that an interactive browser challenge was not completed in time.
	ErrorCodeBrowserChallengeTimeout ErrorCode = "browser_challenge_timeout"
	// ErrorCodeInvalidResourceRequest means that a resource request is not safe or valid.
	ErrorCodeInvalidResourceRequest ErrorCode = "invalid_resource_request"
	// ErrorCodeRetryableHTTP means that an HTTP response can be tried again by Core policy.
	ErrorCodeRetryableHTTP ErrorCode = "retryable_http"
	// ErrorCodeHTTPFailure means that an HTTP request or response failed without a more specific classification.
	ErrorCodeHTTPFailure ErrorCode = "http_failure"
	// ErrorCodeBrowserFailure means that an isolated browser operation failed.
	ErrorCodeBrowserFailure ErrorCode = "browser_failure"
	// ErrorCodeTransportUnavailable means that a requested Core transport is not configured.
	ErrorCodeTransportUnavailable ErrorCode = "transport_unavailable"
	// ErrorCodeResponseTooLarge means that a resource exceeded the configured response limit.
	ErrorCodeResponseTooLarge ErrorCode = "response_too_large"
	// ErrorCodeProviderRequired means that a command needs a provider but none was selected.
	ErrorCodeProviderRequired ErrorCode = "provider_required"
	// ErrorCodeProviderNotFound means that the selected provider is not compiled into this CLI.
	ErrorCodeProviderNotFound ErrorCode = "provider_not_found"
	// ErrorCodeProviderConflict means that positional and flag selections identify different providers.
	ErrorCodeProviderConflict ErrorCode = "provider_conflict"
	// ErrorCodeInvalidProviderResult means that a provider returned data that violates the SDK contract.
	ErrorCodeInvalidProviderResult ErrorCode = "invalid_provider_result"
	// ErrorCodeInvalidProviderConfig means that a provider-specific setting is invalid.
	ErrorCodeInvalidProviderConfig ErrorCode = "invalid_provider_config"
	// ErrorCodeInvalidOutputTemplate means that an output template cannot be parsed or executed.
	ErrorCodeInvalidOutputTemplate ErrorCode = "invalid_output_template"
)

// Error returns the stable code as text.
func (c ErrorCode) Error() string {
	return string(c)
}

// CodedError is a safe public error with a stable machine-readable code.
// The wrapped cause is available through errors.Unwrap, but it is not included
// in Error or JSON output.
type CodedError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`

	cause error
}

// NewError creates a coded error. Message must be safe to show to a user.
func NewError(code ErrorCode, message string, cause error) *CodedError {
	return &CodedError{Code: code, Message: message, cause: cause}
}

// Error returns only the safe user message.
func (e *CodedError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

// Unwrap returns the internal cause.
func (e *CodedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Is reports whether target identifies the same stable error code.
func (e *CodedError) Is(target error) bool {
	if e == nil {
		return false
	}

	switch value := target.(type) {
	case ErrorCode:
		return e.Code == value
	case *CodedError:
		return value != nil && value.Code != "" && e.Code == value.Code
	default:
		return false
	}
}

// ErrorCodeOf gets the first coded error code in an error chain.
func ErrorCodeOf(err error) (ErrorCode, bool) {
	var coded *CodedError
	if !errors.As(err, &coded) || coded == nil {
		return "", false
	}
	return coded.Code, true
}

// WarningCode is a stable machine-readable warning identifier.
type WarningCode string

const (
	// WarningCodePartialParsing means that some entries could not be parsed.
	WarningCodePartialParsing WarningCode = "partial_parsing"
	// WarningCodeCurrencyUnavailable means that the provider returned a currency other than the requested currency.
	WarningCodeCurrencyUnavailable WarningCode = "currency_unavailable"
	// WarningCodeSearchSemanticsUnverified means that a provider can issue a search request but cannot prove that the site honors its query.
	WarningCodeSearchSemanticsUnverified WarningCode = "search_semantics_unverified"
)

// Warning describes a recoverable problem in a successful result.
// Message must be safe to show to a user. Cause is retained for diagnostics,
// but it is not included in JSON output.
type Warning struct {
	Code              WarningCode `json:"code"`
	Message           string      `json:"message"`
	ItemID            string      `json:"item_id,omitempty"`
	URL               string      `json:"url,omitempty"`
	FoundCount        *int        `json:"found_count,omitempty"`
	ParsedCount       *int        `json:"parsed_count,omitempty"`
	RequestedCurrency string      `json:"requested_currency,omitempty"`
	ActualCurrency    string      `json:"actual_currency,omitempty"`

	cause error
}

// NewWarning creates a warning and retains its internal cause.
func NewWarning(code WarningCode, message string, cause error) Warning {
	return Warning{Code: code, Message: message, cause: cause}
}

// Cause returns the internal cause for logging or inspection.
func (w Warning) Cause() error {
	return w.cause
}

// HasCode reports whether the warning has code.
func (w Warning) HasCode(code WarningCode) bool {
	return w.Code == code
}
