package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/kostyay/ecom/provider"
	"k8s.io/client-go/util/jsonpath"
)

const invalidOutputTemplateMessage = "the output template is invalid"

// WriteJSONPath applies a kubectl-style JSONPath template to the complete
// stable envelope. It writes only text that the template requests.
func WriteJSONPath(writer io.Writer, envelope Envelope, template string) error {
	if writer == nil {
		return errors.New("output writer is required")
	}

	parsed := jsonpath.New("output").AllowMissingKeys(true)
	if err := parsed.Parse(template); err != nil {
		return invalidOutputTemplateError(err)
	}

	data, err := jsonValue(envelope)
	if err != nil {
		return invalidOutputTemplateError(err)
	}

	var rendered bytes.Buffer
	if err := parsed.Execute(&rendered, data); err != nil {
		return invalidOutputTemplateError(err)
	}
	if _, err := io.Copy(writer, &rendered); err != nil {
		return fmt.Errorf("write JSONPath output: %w", err)
	}
	return nil
}

// jsonValue applies the envelope's JSON contract before template execution.
// This makes field names follow JSON tags and preserves decimal strings.
func jsonValue(envelope Envelope) (any, error) {
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode output envelope: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode output envelope: %w", err)
	}
	return value, nil
}

func invalidOutputTemplateError(cause error) error {
	return provider.NewError(
		provider.ErrorCodeInvalidOutputTemplate,
		invalidOutputTemplateMessage,
		cause,
	)
}
