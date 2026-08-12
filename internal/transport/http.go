// Package transport implements the Core-owned website transports.
package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/provider"
)

var errUnsafeRedirect = errors.New("unsafe HTTP redirect")
var errTooManyRedirects = errors.New("too many HTTP redirects")

// Clock supplies response retrieval times.
type Clock interface {
	Now() time.Time
}

// ClockFunc adapts a function to Clock.
type ClockFunc func() time.Time

// Now returns the current time.
func (function ClockFunc) Now() time.Time { return function() }

// HTTPExecutor performs one direct HTTP attempt. Higher layers apply cache,
// request limits, retries, and browser fallback.
type HTTPExecutor struct {
	client          *http.Client
	clock           Clock
	maxResponseSize int64
}

// NewHTTPExecutor creates a direct HTTP executor. The supplied client is
// copied so the executor can install its redirect policy without changing it.
func NewHTTPExecutor(client *http.Client, clock Clock, maxResponseSize config.ByteSize) (*HTTPExecutor, error) {
	if clock == nil {
		return nil, errors.New("HTTP executor clock is required")
	}
	if maxResponseSize <= 0 {
		return nil, errors.New("HTTP response limit must be positive")
	}
	if client == nil {
		client = &http.Client{}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = safeRedirect
	return &HTTPExecutor{client: &clientCopy, clock: clock, maxResponseSize: int64(maxResponseSize)}, nil
}

// Fetch performs exactly one HTTP exchange, apart from safe same-origin
// redirects handled by net/http.
func (executor *HTTPExecutor) Fetch(ctx context.Context, resource provider.ResourceRequest) (provider.ResourceResponse, error) {
	if err := ctx.Err(); err != nil {
		return provider.ResourceResponse{}, err
	}
	request, sensitiveQueryNames, err := buildRequest(ctx, resource)
	if err != nil {
		return provider.ResourceResponse{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the HTTP resource request is invalid", err)
	}

	response, err := executor.client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return provider.ResourceResponse{}, ctxErr
		}
		return provider.ResourceResponse{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the HTTP request failed", errors.New("HTTP client request failed"))
	}
	defer func() { _ = response.Body.Close() }()

	body, err := readLimited(ctx, response.Body, response.ContentLength, executor.maxResponseSize)
	if err != nil {
		return provider.ResourceResponse{}, err
	}
	retrievedAt := executor.clock.Now().UTC()
	result := provider.ResourceResponse{
		Body:        body,
		StatusCode:  response.StatusCode,
		FinalURL:    safeFinalURL(response.Request.URL, sensitiveQueryNames),
		SafeHeaders: safeResponseHeaders(response.Header),
		RetrievedAt: retrievedAt,
		Transport:   provider.TransportHTTP,
	}
	if statusErr := classifyResponse(response, body); statusErr != nil {
		return result, statusErr
	}
	return result, nil
}

func buildRequest(ctx context.Context, resource provider.ResourceRequest) (*http.Request, map[string]struct{}, error) {
	method := strings.ToUpper(strings.TrimSpace(resource.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !validMethod(method) {
		return nil, nil, errors.New("unsupported HTTP method")
	}
	parsed, err := url.Parse(resource.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, nil, errors.New("HTTP URL must be absolute")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, nil, errors.New("HTTP URL scheme must be http or https")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return nil, nil, errors.New("HTTP URL must not contain user information or a fragment")
	}

	query := parsed.Query()
	sensitiveQueryNames := make(map[string]struct{})
	for _, value := range resource.Query {
		if !validQueryName(value.Name) || len(value.Values) == 0 {
			return nil, nil, errors.New("invalid HTTP query parameter")
		}
		for _, item := range value.Values {
			query.Add(value.Name, item)
		}
		if value.Sensitive {
			sensitiveQueryNames[value.Name] = struct{}{}
		}
	}
	parsed.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, method, parsed.String(), bytes.NewReader(resource.Body.Bytes))
	if err != nil {
		return nil, nil, errors.New("invalid HTTP request")
	}
	for _, header := range resource.Headers {
		if !validRequestHeader(header) {
			return nil, nil, errors.New("invalid HTTP request header")
		}
		for _, value := range header.Values {
			request.Header.Add(header.Name, value)
		}
	}
	return request, sensitiveQueryNames, nil
}

func validMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

func validQueryName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "&=#\x00\r\n")
}

func validRequestHeader(value provider.RequestValue) bool {
	if !validHeaderName(value.Name) || len(value.Values) == 0 {
		return false
	}
	switch strings.ToLower(value.Name) {
	case "host", "content-length", "transfer-encoding", "connection", "proxy-authorization", "proxy-authenticate":
		return false
	}
	for _, item := range value.Values {
		if strings.ContainsAny(item, "\r\n\x00") {
			return false
		}
	}
	return true
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for index := range len(name) {
		if !isTokenByte(name[index]) {
			return false
		}
	}
	return true
}

func isTokenByte(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(value))
}

func safeRedirect(next *http.Request, previous []*http.Request) error {
	if len(previous) >= 10 {
		return errTooManyRedirects
	}
	if len(previous) > 0 && !sameOrigin(previous[0].URL, next.URL) {
		return errUnsafeRedirect
	}
	return nil
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Hostname(), right.Hostname()) && effectivePort(left) == effectivePort(right)
}

func effectivePort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if value.Scheme == "https" {
		return "443"
	}
	return "80"
}

func readLimited(ctx context.Context, body io.Reader, contentLength, limit int64) ([]byte, error) {
	if contentLength > limit {
		return nil, provider.NewError(provider.ErrorCodeResponseTooLarge, "the HTTP response exceeds the configured size limit", nil)
	}
	result, err := io.ReadAll(io.LimitReader(body, limit+1))
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		return nil, provider.NewError(provider.ErrorCodeHTTPFailure, "the HTTP response body could not be read", err)
	}
	if int64(len(result)) > limit {
		return nil, provider.NewError(provider.ErrorCodeResponseTooLarge, "the HTTP response exceeds the configured size limit", nil)
	}
	return result, nil
}

func classifyResponse(response *http.Response, body []byte) error {
	if isBrowserChallenge(response.Header, body) {
		return provider.NewError(provider.ErrorCodeBrowserChallengeRequired, "the website requires a browser challenge", nil)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || isAccessBlock(response.Header, body) {
		return provider.NewError(provider.ErrorCodeAccessBlocked, "the website blocked HTTP access", nil)
	}
	switch response.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return provider.NewError(provider.ErrorCodeRetryableHTTP, "the website returned a temporary HTTP failure", nil)
	}
	if response.StatusCode >= http.StatusBadRequest {
		return provider.NewError(provider.ErrorCodeHTTPFailure, "the website returned HTTP status "+strconv.Itoa(response.StatusCode), nil)
	}
	return nil
}

func isAccessBlock(headers http.Header, body []byte) bool {
	if !strings.Contains(strings.ToLower(headers.Get("Content-Type")), "text/html") {
		return false
	}
	lowerBody := strings.ToLower(string(body))
	return titleText(lowerBody) == "access denied" &&
		(strings.Contains(lowerBody, "you don't have permission to access") || strings.Contains(lowerBody, "request blocked"))
}

func isBrowserChallenge(headers http.Header, body []byte) bool {
	if strings.EqualFold(strings.TrimSpace(headers.Get("Cf-Mitigated")), "challenge") {
		return true
	}
	contentType := strings.ToLower(headers.Get("Content-Type"))
	if !strings.Contains(contentType, "text/html") {
		return false
	}
	lowerBody := strings.ToLower(string(body))
	title := titleText(lowerBody)
	cloudflare := strings.Contains(lowerBody, "/cdn-cgi/challenge-platform/") || strings.Contains(lowerBody, "cf-chl-")
	captcha := strings.Contains(lowerBody, "hcaptcha.com/") || strings.Contains(lowerBody, "recaptcha/api") || strings.Contains(lowerBody, "turnstile")
	return (strings.Contains(title, "just a moment") && cloudflare) ||
		((strings.Contains(title, "verify you are human") || strings.Contains(title, "security check")) && (cloudflare || captcha))
}

func titleText(lowerHTML string) string {
	start := strings.Index(lowerHTML, "<title")
	if start < 0 {
		return ""
	}
	openEnd := strings.IndexByte(lowerHTML[start:], '>')
	if openEnd < 0 {
		return ""
	}
	start += openEnd + 1
	end := strings.Index(lowerHTML[start:], "</title>")
	if end < 0 {
		return ""
	}
	return strings.Join(strings.Fields(lowerHTML[start:start+end]), " ")
}

func safeFinalURL(value *url.URL, sensitiveNames map[string]struct{}) string {
	safeURL := *value
	query := safeURL.Query()
	for name := range sensitiveNames {
		if _, found := query[name]; found {
			query[name] = []string{"[REDACTED]"}
		}
	}
	safeURL.RawQuery = query.Encode()
	return safeURL.String()
}

func safeResponseHeaders(headers http.Header) map[string][]string {
	allowed := map[string]struct{}{
		"Accept-Ranges": {}, "Age": {}, "Cache-Control": {}, "Content-Encoding": {},
		"Content-Language": {}, "Content-Length": {}, "Content-Type": {}, "ETag": {},
		"Expires": {}, "Last-Modified": {}, "Retry-After": {}, "Vary": {},
	}
	result := make(map[string][]string)
	for name, values := range headers {
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		if _, ok := allowed[canonical]; ok {
			result[canonical] = append([]string(nil), values...)
		}
	}
	return result
}

var _ provider.ResourceService = (*HTTPExecutor)(nil)
