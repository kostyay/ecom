package cache

import (
	"bytes"
	"strings"
	"testing"
)

func TestEntryCodec(t *testing.T) {
	text := []byte(strings.Repeat("product response ", 256))
	tests := []struct {
		name         string
		contentType  string
		body         []byte
		wantEncoding Encoding
	}{
		{name: "compressible text", contentType: "text/html; charset=utf-8", body: text, wantEncoding: EncodingGzip},
		{name: "JSON", contentType: "application/json", body: text, wantEncoding: EncodingGzip},
		{name: "non-text", contentType: "image/jpeg", body: text, wantEncoding: EncodingIdentity},
		{name: "small text", contentType: "text/plain", body: []byte("small"), wantEncoding: EncodingIdentity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := Entry{Body: append([]byte(nil), test.body...), Encoding: EncodingIdentity, SafeHeaders: map[string][]string{"Content-Type": {test.contentType}}}
			encoded, err := encodeEntry(original, int64(len(test.body)))
			if err != nil {
				t.Fatal(err)
			}
			if encoded.Encoding != test.wantEncoding {
				t.Fatalf("encoding = %q, want %q", encoded.Encoding, test.wantEncoding)
			}
			decoded, err := decodeEntry(encoded, int64(len(test.body)))
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Encoding != EncodingIdentity || !bytes.Equal(decoded.Body, test.body) {
				t.Fatalf("decoded response differs from original")
			}
		})
	}
}

func TestDecodeEntryRejectsMalformedGzip(t *testing.T) {
	_, err := decodeEntry(Entry{Body: []byte("not gzip"), Encoding: EncodingGzip}, 1024)
	if err == nil || !strings.Contains(err.Error(), "gzip") {
		t.Fatalf("decodeEntry error = %v, want gzip error", err)
	}
}

func TestDecodeEntryCapsDecompressedSize(t *testing.T) {
	original := Entry{
		Body: []byte(strings.Repeat("x", 2048)), Encoding: EncodingIdentity,
		SafeHeaders: map[string][]string{"Content-Type": {"text/plain"}},
	}
	encoded, err := encodeEntry(original, int64(len(original.Body)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = decodeEntry(encoded, 1024)
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("decodeEntry error = %v, want size error", err)
	}
}
