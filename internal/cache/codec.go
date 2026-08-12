package cache

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
)

const compressionThreshold = 1024

// encodeEntry prepares a transport response for storage. The response size
// limit applies before compression so compression cannot hide an oversized
// response.
func encodeEntry(entry Entry, maxResponseSize int64) (Entry, error) {
	if int64(len(entry.Body)) > maxResponseSize {
		return Entry{}, fmt.Errorf("raw response body size %d exceeds maximum %d", len(entry.Body), maxResponseSize)
	}
	if entry.Encoding != EncodingIdentity || len(entry.Body) < compressionThreshold || !isTextual(entry.SafeHeaders) {
		return entry, nil
	}

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(entry.Body); err != nil {
		return Entry{}, fmt.Errorf("compress raw response: %w", err)
	}
	if err := writer.Close(); err != nil {
		return Entry{}, fmt.Errorf("finish raw response compression: %w", err)
	}
	if compressed.Len() >= len(entry.Body) {
		return entry, nil
	}

	entry.Body = append([]byte(nil), compressed.Bytes()...)
	entry.Encoding = EncodingGzip
	return entry, nil
}

// decodeEntry restores a stored response before provider code receives it.
func decodeEntry(entry Entry, maxResponseSize int64) (Entry, error) {
	switch entry.Encoding {
	case EncodingIdentity:
		if int64(len(entry.Body)) > maxResponseSize {
			return Entry{}, fmt.Errorf("raw response body size %d exceeds maximum %d", len(entry.Body), maxResponseSize)
		}
		return entry, nil
	case EncodingGzip:
		reader, err := gzip.NewReader(bytes.NewReader(entry.Body))
		if err != nil {
			return Entry{}, fmt.Errorf("open gzip raw response: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(reader, maxResponseSize+1))
		closeErr := reader.Close()
		if readErr != nil {
			return Entry{}, fmt.Errorf("decompress raw response: %w", readErr)
		}
		if closeErr != nil {
			return Entry{}, fmt.Errorf("close gzip raw response: %w", closeErr)
		}
		if int64(len(body)) > maxResponseSize {
			return Entry{}, fmt.Errorf("decompressed raw response size exceeds maximum %d", maxResponseSize)
		}
		entry.Body = body
		entry.Encoding = EncodingIdentity
		return entry, nil
	default:
		return Entry{}, errors.New("raw response encoding is not supported")
	}
}

func isTextual(headers map[string][]string) bool {
	for name, values := range headers {
		if !strings.EqualFold(name, "Content-Type") {
			continue
		}
		for _, value := range values {
			mediaType, _, err := mime.ParseMediaType(value)
			if err != nil {
				continue
			}
			mediaType = strings.ToLower(mediaType)
			if strings.HasPrefix(mediaType, "text/") || strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") {
				return true
			}
			switch mediaType {
			case "application/json", "application/javascript", "application/xml", "image/svg+xml":
				return true
			}
		}
	}
	return false
}
