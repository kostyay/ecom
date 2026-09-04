// Package cache contains provider-neutral response cache policy helpers.
package cache

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/kostyay/ecom/provider"
)

const keyVersion = "v1"

var errInvalidCacheKeyInput = errors.New("invalid cache key input")

// Key identifies one raw response without exposing the request data used to
// calculate the identity.
type Key struct {
	Version string
	Digest  string
}

// String returns the versioned cryptographic digest used by cache storage.
func (key Key) String() string {
	return key.Version + ":" + key.Digest
}

// BuildKey calculates a stable resource identity for one provider. Sensitive
// request values and cache-use policy are not part of the identity.
func BuildKey(providerName string, request provider.ResourceRequest) (Key, error) {
	canonical, err := canonicalize(providerName, request)
	if err != nil {
		return Key{}, err
	}

	encoded, err := json.Marshal(canonical)
	if err != nil {
		return Key{}, fmt.Errorf("%w: encode canonical request", errInvalidCacheKeyInput)
	}
	digest := sha256.Sum256(encoded)

	return Key{Version: keyVersion, Digest: hex.EncodeToString(digest[:])}, nil
}

type canonicalRequest struct {
	Provider       string             `json:"provider"`
	Market         canonicalMarket    `json:"market"`
	Method         string             `json:"method"`
	URL            string             `json:"url"`
	Query          []canonicalValue   `json:"query,omitempty"`
	Headers        []canonicalValue   `json:"headers,omitempty"`
	BodyDigest     string             `json:"body_digest,omitempty"`
	Transport      canonicalTransport `json:"transport"`
	DOM            []canonicalDOM     `json:"dom,omitempty"`
	CachePartition string             `json:"cache_partition,omitempty"`
}

type canonicalMarket struct {
	Country  string `json:"country"`
	Language string `json:"language"`
	Currency string `json:"currency"`
}

type canonicalValue struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

type canonicalTransport struct {
	Required  string   `json:"required,omitempty"`
	Preferred []string `json:"preferred,omitempty"`
}

type canonicalDOM struct {
	Name      string `json:"name"`
	Selector  string `json:"selector"`
	Kind      string `json:"kind"`
	Attribute string `json:"attribute,omitempty"`
	All       bool   `json:"all,omitzero"`
}

func canonicalize(providerName string, request provider.ResourceRequest) (canonicalRequest, error) {
	providerID := strings.ToLower(strings.TrimSpace(providerName))
	if providerID == "" {
		return canonicalRequest{}, fmt.Errorf("%w: provider is required", errInvalidCacheKeyInput)
	}

	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if method == "" {
		method = "GET"
	}
	if !validHTTPToken(method) {
		return canonicalRequest{}, fmt.Errorf("%w: method is malformed", errInvalidCacheKeyInput)
	}

	normalizedURL, urlQuery, err := canonicalURL(request.URL)
	if err != nil {
		return canonicalRequest{}, err
	}

	requestQuery := canonicalValues(request.Query, false)
	query := make([]canonicalValue, 0, len(urlQuery)+len(requestQuery))
	query = append(query, urlQuery...)
	query = append(query, requestQuery...)
	query = mergeCanonicalValues(query)
	headers := mergeCanonicalValues(canonicalValues(request.Headers, true))

	result := canonicalRequest{
		Provider: providerID,
		Market: canonicalMarket{
			Country:  strings.ToUpper(strings.TrimSpace(request.Market.Country)),
			Language: strings.ToLower(strings.TrimSpace(request.Market.Language)),
			Currency: strings.ToUpper(strings.TrimSpace(request.Market.Currency)),
		},
		Method:         method,
		URL:            normalizedURL,
		Query:          query,
		Headers:        headers,
		Transport:      canonicalizeTransport(request.Transport),
		DOM:            canonicalizeDOM(request.DOM),
		CachePartition: request.CachePartition,
	}
	if !request.Body.Sensitive && len(request.Body.Bytes) != 0 {
		bodyDigest := sha256.Sum256(request.Body.Bytes)
		result.BodyDigest = hex.EncodeToString(bodyDigest[:])
	}

	return result, nil
}

func canonicalizeDOM(operations []provider.DOMExtraction) []canonicalDOM {
	result := make([]canonicalDOM, 0, len(operations))
	for _, operation := range operations {
		result = append(result, canonicalDOM{
			Name: operation.Name, Selector: operation.Selector, Kind: string(operation.Kind),
			Attribute: operation.Attribute, All: operation.All,
		})
	}
	return result
}

func canonicalURL(rawURL string) (string, []canonicalValue, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" {
		return "", nil, fmt.Errorf("%w: URL is malformed", errInvalidCacheKeyInput)
	}
	if parsed.User != nil {
		return "", nil, fmt.Errorf("%w: URL user information is not permitted", errInvalidCacheKeyInput)
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", nil, fmt.Errorf("%w: URL scheme is not supported", errInvalidCacheKeyInput)
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return "", nil, fmt.Errorf("%w: URL host is malformed", errInvalidCacheKeyInput)
	}
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	parsed.Host = hostname
	if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	parsed.RawPath = normalizeEscapedPath(parsed.EscapedPath())
	parsed.Fragment = ""
	parsed.RawFragment = ""

	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", nil, fmt.Errorf("%w: URL query is malformed", errInvalidCacheKeyInput)
	}
	query := make([]canonicalValue, 0, len(values))
	for name, entries := range values {
		query = append(query, canonicalValue{Name: name, Values: append([]string(nil), entries...)})
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false

	return parsed.String(), mergeCanonicalValues(query), nil
}

func normalizeEscapedPath(escaped string) string {
	var result strings.Builder
	result.Grow(len(escaped))
	for index := 0; index < len(escaped); index++ {
		if escaped[index] != '%' || index+2 >= len(escaped) {
			result.WriteByte(escaped[index])
			continue
		}
		decoded, ok := decodeHexByte(escaped[index+1], escaped[index+2])
		if !ok {
			result.WriteByte(escaped[index])
			continue
		}
		if isUnreserved(decoded) {
			result.WriteByte(decoded)
		} else {
			const upperHex = "0123456789ABCDEF"
			result.WriteByte('%')
			result.WriteByte(upperHex[decoded>>4])
			result.WriteByte(upperHex[decoded&0x0f])
		}
		index += 2
	}
	return result.String()
}

func decodeHexByte(high, low byte) (byte, bool) {
	highValue, highOK := hexValue(high)
	lowValue, lowOK := hexValue(low)
	return highValue<<4 | lowValue, highOK && lowOK
}

func hexValue(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

func isUnreserved(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		strings.ContainsRune("-._~", rune(value))
}

func canonicalValues(values []provider.RequestValue, header bool) []canonicalValue {
	result := make([]canonicalValue, 0, len(values))
	for _, value := range values {
		if value.Sensitive {
			continue
		}
		name := value.Name
		entries := append([]string(nil), value.Values...)
		if header {
			name = strings.ToLower(strings.TrimSpace(name))
			for index := range entries {
				entries[index] = strings.TrimSpace(entries[index])
			}
		}
		result = append(result, canonicalValue{Name: name, Values: entries})
	}
	return result
}

func mergeCanonicalValues(values []canonicalValue) []canonicalValue {
	merged := make(map[string][]string, len(values))
	for _, value := range values {
		merged[value.Name] = append(merged[value.Name], value.Values...)
	}
	result := make([]canonicalValue, 0, len(merged))
	for name, entries := range merged {
		slices.Sort(entries)
		result = append(result, canonicalValue{Name: name, Values: entries})
	}
	slices.SortFunc(result, func(left, right canonicalValue) int {
		return cmp.Compare(left.Name, right.Name)
	})
	return result
}

func canonicalizeTransport(policy provider.TransportPolicy) canonicalTransport {
	result := canonicalTransport{Required: string(policy.Required)}
	if policy.Required != "" {
		return result
	}
	for _, mode := range policy.Preferred {
		result.Preferred = append(result.Preferred, string(mode))
	}
	return result
}

func validHTTPToken(value string) bool {
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={}", character) {
			return false
		}
	}
	return value != ""
}
