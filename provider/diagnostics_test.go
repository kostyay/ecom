package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestDiagnosticCodesAreStable(t *testing.T) {
	errorCodes := map[ErrorCode]string{
		ErrorCodeCapabilityUnavailable:    "capability_unavailable",
		ErrorCodeInvalidFilter:            "invalid_filter",
		ErrorCodeVariantNotFound:          "variant_not_found",
		ErrorCodeAccessBlocked:            "access_blocked",
		ErrorCodeBrowserChallengeRequired: "browser_challenge_required",
		ErrorCodeBrowserChallengeTimeout:  "browser_challenge_timeout",
		ErrorCodeInvalidResourceRequest:   "invalid_resource_request",
		ErrorCodeRetryableHTTP:            "retryable_http",
		ErrorCodeHTTPFailure:              "http_failure",
		ErrorCodeBrowserFailure:           "browser_failure",
		ErrorCodeTransportUnavailable:     "transport_unavailable",
		ErrorCodeResponseTooLarge:         "response_too_large",
		ErrorCodeInvalidOutputTemplate:    "invalid_output_template",
	}
	for code, want := range errorCodes {
		if string(code) != want {
			t.Errorf("error code = %q, want %q", code, want)
		}
	}

	warningCodes := map[WarningCode]string{
		WarningCodePartialParsing:            "partial_parsing",
		WarningCodeCurrencyUnavailable:       "currency_unavailable",
		WarningCodeSearchSemanticsUnverified: "search_semantics_unverified",
	}
	for code, want := range warningCodes {
		if string(code) != want {
			t.Errorf("warning code = %q, want %q", code, want)
		}
	}
}

func TestCodedErrorPreservesCauseAndSafeMessage(t *testing.T) {
	cause := errors.New("private upstream response")
	coded := NewError(ErrorCodeAccessBlocked, "the provider blocked access", cause)
	err := fmt.Errorf("fetch product: %w", coded)

	if got := err.Error(); strings.Contains(got, cause.Error()) {
		t.Fatalf("Error() exposed the cause: %q", got)
	}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is did not find the wrapped cause")
	}
	if !errors.Is(err, ErrorCodeAccessBlocked) {
		t.Fatal("errors.Is did not match the stable code")
	}
	if errors.Is(err, ErrorCodeInvalidFilter) {
		t.Fatal("errors.Is matched a different stable code")
	}

	got, ok := errors.AsType[*CodedError](err)
	if !ok {
		t.Fatal("errors.As did not find CodedError")
	}
	if got != coded {
		t.Fatalf("errors.As returned %#v, want %#v", got, coded)
	}
}

func TestErrorCodeOf(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode ErrorCode
		wantOK   bool
	}{
		{
			name:     "direct",
			err:      NewError(ErrorCodeCapabilityUnavailable, "search is unavailable", nil),
			wantCode: ErrorCodeCapabilityUnavailable,
			wantOK:   true,
		},
		{
			name:     "wrapped",
			err:      fmt.Errorf("select variant: %w", NewError(ErrorCodeVariantNotFound, "variant not found", nil)),
			wantCode: ErrorCodeVariantNotFound,
			wantOK:   true,
		},
		{name: "uncoded", err: errors.New("plain error")},
		{name: "nil"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, ok := ErrorCodeOf(test.err)
			if code != test.wantCode || ok != test.wantOK {
				t.Fatalf("ErrorCodeOf() = %q, %t; want %q, %t", code, ok, test.wantCode, test.wantOK)
			}
		})
	}
}

func TestCodedErrorJSONExcludesCause(t *testing.T) {
	err := NewError(ErrorCodeBrowserChallengeRequired, "complete the browser challenge", errors.New("secret challenge details"))

	encoded, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal coded error: %v", marshalErr)
	}

	want := `{"code":"browser_challenge_required","message":"complete the browser challenge"}`
	if string(encoded) != want {
		t.Fatalf("coded error JSON = %s, want %s", encoded, want)
	}
	if strings.Contains(string(encoded), "secret") {
		t.Fatalf("coded error JSON exposed the cause: %s", encoded)
	}
}

func TestWarningJSONAndInspection(t *testing.T) {
	cause := errors.New("private parse input")
	found := 12
	parsed := 10
	warning := NewWarning(WarningCodePartialParsing, "some products could not be parsed", cause)
	warning.ItemID = "item-12"
	warning.URL = "https://shop.example/items/12"
	warning.FoundCount = &found
	warning.ParsedCount = &parsed

	if !warning.HasCode(WarningCodePartialParsing) {
		t.Fatal("HasCode did not match the stable warning code")
	}
	if warning.HasCode(WarningCodeCurrencyUnavailable) {
		t.Fatal("HasCode matched a different stable warning code")
	}
	if !errors.Is(warning.Cause(), cause) {
		t.Fatal("Cause did not preserve the internal warning cause")
	}

	encoded, err := json.Marshal(warning)
	if err != nil {
		t.Fatalf("marshal warning: %v", err)
	}
	want := `{"code":"partial_parsing","message":"some products could not be parsed","item_id":"item-12","url":"https://shop.example/items/12","found_count":12,"parsed_count":10}`
	if string(encoded) != want {
		t.Fatalf("warning JSON = %s, want %s", encoded, want)
	}
	if strings.Contains(string(encoded), "private") {
		t.Fatalf("warning JSON exposed the cause: %s", encoded)
	}
}

func TestWarningJSONRoundTrip(t *testing.T) {
	warning := Warning{
		Code:              WarningCodeCurrencyUnavailable,
		Message:           "the requested currency is unavailable",
		RequestedCurrency: "USD",
		ActualCurrency:    "EUR",
	}

	encoded, err := json.Marshal(warning)
	if err != nil {
		t.Fatalf("marshal warning: %v", err)
	}
	var decoded Warning
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal warning: %v", err)
	}
	if !reflect.DeepEqual(decoded, warning) {
		t.Fatalf("round trip mismatch\ngot:  %#v\nwant: %#v", decoded, warning)
	}
}
