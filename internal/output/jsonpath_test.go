package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/kostyay/ecom/provider"
)

func TestWriteJSONPath(t *testing.T) {
	price := &provider.Money{Amount: "79.95", Currency: "EUR", Display: "€79.95"}
	envelope := NewListing(
		"bike-discount",
		testMarket,
		[]provider.ProductSummary{
			{ID: "p-1", Name: "First", Price: price},
			{ID: "p-2", Name: "Second"},
		},
		provider.PageInfo{Number: 2, Size: 24},
		nil,
		nil,
		Metadata{},
	)

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{name: "simple field", template: `{.provider}`, want: "bike-discount"},
		{name: "wildcard", template: `{.data.items[*].name}`, want: "First Second"},
		{name: "range and newlines", template: `{range .data.items[*]}{.id}{"\n"}{end}`, want: "p-1\np-2\n"},
		{name: "quoted text", template: `{"provider: "}{.provider}`, want: "provider: bike-discount"},
		{name: "array index", template: `{.data.items[1].name}`, want: "Second"},
		{name: "missing field", template: `{.data.missing}`, want: ""},
		{name: "missing nested value", template: `{.data.items[*].price.display}`, want: "€79.95"},
		{name: "omitted nil value", template: `{.data.items[1].price}`, want: ""},
		{name: "money amount stays a decimal string", template: `{.data.items[0].price.amount}`, want: "79.95"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := WriteJSONPath(&output, envelope, test.template); err != nil {
				t.Fatal(err)
			}
			if output.String() != test.want {
				t.Errorf("output = %q, want %q", output.String(), test.want)
			}
		})
	}
}

func TestWriteJSONPathRendersExplicitNull(t *testing.T) {
	envelope := New("bike-discount", testMarket, map[string]any{"value": nil}, nil, Metadata{})
	var output bytes.Buffer
	if err := WriteJSONPath(&output, envelope, `{.data.value}`); err != nil {
		t.Fatal(err)
	}
	if output.String() != "null" {
		t.Errorf("output = %q, want kubectl null output", output.String())
	}
}

func TestWriteJSONPathReportsTemplateErrorsSafely(t *testing.T) {
	envelope := New("bike-discount", testMarket, map[string]any{"secret": "do-not-leak"}, nil, Metadata{})

	for _, test := range []struct {
		name     string
		template string
	}{
		{name: "parse", template: `{.data[}`},
		{name: "execution type", template: `{.provider[0]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			err := WriteJSONPath(&output, envelope, test.template)
			if !errors.Is(err, provider.ErrorCodeInvalidOutputTemplate) {
				t.Fatalf("error = %v, want %s", err, provider.ErrorCodeInvalidOutputTemplate)
			}
			if err.Error() != invalidOutputTemplateMessage {
				t.Errorf("safe error = %q, want %q", err, invalidOutputTemplateMessage)
			}
			if strings.Contains(err.Error(), "do-not-leak") {
				t.Errorf("error leaks envelope data: %q", err)
			}
			if output.Len() != 0 {
				t.Errorf("failed template wrote partial output %q", output.String())
			}
		})
	}
}

func TestWriteJSONPathReportsEnvelopeTypeError(t *testing.T) {
	envelope := New("bike-discount", testMarket, func() {}, nil, Metadata{})
	err := WriteJSONPath(&bytes.Buffer{}, envelope, `{.data}`)
	if !errors.Is(err, provider.ErrorCodeInvalidOutputTemplate) {
		t.Fatalf("error = %v, want %s", err, provider.ErrorCodeInvalidOutputTemplate)
	}
}

func TestWriteJSONPathReportsWriterFailure(t *testing.T) {
	want := errors.New("write stopped")
	err := WriteJSONPath(failingWriter{err: want}, New("bike-discount", testMarket, struct{}{}, nil, Metadata{}), `{.provider}`)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped writer error", err)
	}
	if errors.Is(err, provider.ErrorCodeInvalidOutputTemplate) {
		t.Fatal("writer error was classified as an invalid template")
	}
}

func TestWriteJSONPathDoesNotAddNewline(t *testing.T) {
	var output bytes.Buffer
	if err := Write(&output, New("bike-discount", testMarket, struct{}{}, nil, Metadata{}), Selection{
		Mode: ModeJSONPath, Template: `{.provider}`,
	}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "bike-discount" {
		t.Errorf("output = %q, want no added newline", output.String())
	}
}
