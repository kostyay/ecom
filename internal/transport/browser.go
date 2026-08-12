package transport

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/internal/session"
	"github.com/kostyay/ecom/provider"
)

// BrowserBackend owns one isolated browser run. It performs the closed set of
// operations prepared by BrowserExecutor and does not expose a live page.
type BrowserBackend interface {
	Navigate(context.Context, BrowserCommand) (BrowserResult, error)
}

// BrowserCommand is a validated browser navigation and extraction request.
type BrowserCommand struct {
	URL              string
	Headers          map[string]string
	DOM              []provider.DOMExtraction
	State            session.State
	Headed           bool
	WaitForChallenge bool
	MaxPageSize      int64
}

// BrowserResult contains the data captured before the isolated browser closes.
type BrowserResult struct {
	HTML       []byte
	DOM        map[string][]string
	FinalURL   string
	StatusCode int
	Headers    http.Header
	State      session.State
}

// BrowserNavigation contains a resource response and exported portable state.
type BrowserNavigation struct {
	Response provider.ResourceResponse
	State    session.State
}

// BrowserExecutor validates provider requests and closes the concrete browser
// boundary around BrowserBackend.
type BrowserExecutor struct {
	backend         BrowserBackend
	clock           Clock
	maxResponseSize int64
	headed          bool
}

// NewBrowserExecutor makes an isolated browser executor.
func NewBrowserExecutor(backend BrowserBackend, clock Clock, maxResponseSize config.ByteSize, headed bool) (*BrowserExecutor, error) {
	if backend == nil {
		return nil, errors.New("browser backend is required")
	}
	if clock == nil {
		return nil, errors.New("browser executor clock is required")
	}
	if maxResponseSize <= 0 {
		return nil, errors.New("browser page limit must be positive")
	}
	return &BrowserExecutor{backend: backend, clock: clock, maxResponseSize: int64(maxResponseSize), headed: headed}, nil
}

// NewConfiguredBrowserExecutor wires an isolated Chrome backend with the
// configured page limit and headed mode.
func NewConfiguredBrowserExecutor(settings config.Settings) (*BrowserExecutor, error) {
	return NewBrowserExecutor(
		NewChromedpBackend(),
		ClockFunc(time.Now),
		settings.Cache.MaxResponseSize,
		settings.Browser.Headed,
	)
}

// Fetch implements provider.ResourceService with an empty portable state.
// Session-aware transport composition uses Navigate directly.
func (executor *BrowserExecutor) Fetch(ctx context.Context, resource provider.ResourceRequest) (provider.ResourceResponse, error) {
	result, err := executor.Navigate(ctx, resource, session.State{})
	return result.Response, err
}

// Navigate opens one isolated browser, imports state, waits for a useful page,
// performs declared DOM reads, and exports updated state.
func (executor *BrowserExecutor) Navigate(ctx context.Context, resource provider.ResourceRequest, state session.State) (BrowserNavigation, error) {
	return executor.navigate(ctx, resource, state, executor.headed, false)
}

// NavigateHeadless performs one non-interactive navigation. It never opens a
// visible browser, even when the executor has a different default.
func (executor *BrowserExecutor) NavigateHeadless(ctx context.Context, resource provider.ResourceRequest, state session.State) (BrowserNavigation, error) {
	return executor.navigate(ctx, resource, state, false, false)
}

// NavigateInteractive opens a visible browser and waits while a person clears
// a detected challenge. The call does not perform challenge actions.
func (executor *BrowserExecutor) NavigateInteractive(ctx context.Context, resource provider.ResourceRequest, state session.State) (BrowserNavigation, error) {
	return executor.navigate(ctx, resource, state, true, true)
}

func (executor *BrowserExecutor) navigate(ctx context.Context, resource provider.ResourceRequest, state session.State, headed, waitForChallenge bool) (BrowserNavigation, error) {
	if err := ctx.Err(); err != nil {
		return BrowserNavigation{}, err
	}
	command, requestedURL, sensitiveNames, err := executor.command(resource, state)
	if err != nil {
		return BrowserNavigation{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the browser resource request is invalid", err)
	}
	command.Headed = headed
	command.WaitForChallenge = waitForChallenge

	result, err := executor.backend.Navigate(ctx, command)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return BrowserNavigation{}, ctxErr
		}
		if errors.Is(err, provider.ErrorCodeResponseTooLarge) {
			return BrowserNavigation{}, provider.NewError(provider.ErrorCodeResponseTooLarge, "the browser page exceeds the configured size limit", nil)
		}
		return BrowserNavigation{}, provider.NewError(provider.ErrorCodeBrowserFailure, "the browser navigation failed", errors.New("browser backend failed"))
	}
	if err := validateBrowserResult(requestedURL, result, executor.maxResponseSize); err != nil {
		return BrowserNavigation{}, err
	}
	if err := result.State.Validate(); err != nil {
		return BrowserNavigation{}, provider.NewError(provider.ErrorCodeBrowserFailure, "the browser returned invalid session state", errors.New("browser state validation failed"))
	}

	finalURL, _ := url.Parse(result.FinalURL)
	response := provider.ResourceResponse{
		Page:        &provider.PageSnapshot{HTML: append([]byte(nil), result.HTML...), DOM: cloneDOM(result.DOM)},
		StatusCode:  result.StatusCode,
		FinalURL:    safeFinalURL(finalURL, sensitiveNames),
		SafeHeaders: safeResponseHeaders(result.Headers),
		RetrievedAt: executor.clock.Now().UTC(),
		Transport:   provider.TransportBrowser,
	}
	if classifyErr := classifyBrowserResult(result); classifyErr != nil {
		return BrowserNavigation{Response: response, State: cloneState(result.State)}, classifyErr
	}
	return BrowserNavigation{Response: response, State: cloneState(result.State)}, nil
}

func (executor *BrowserExecutor) command(resource provider.ResourceRequest, state session.State) (BrowserCommand, *url.URL, map[string]struct{}, error) {
	if err := state.Validate(); err != nil {
		return BrowserCommand{}, nil, nil, errors.New("portable browser state is invalid")
	}
	for _, header := range resource.Headers {
		name := strings.ToLower(strings.TrimSpace(header.Name))
		if header.Sensitive || name == "authorization" || name == "cookie" {
			return BrowserCommand{}, nil, nil, errors.New("browser navigation does not accept credential headers")
		}
	}
	request, sensitiveNames, err := buildRequest(context.Background(), resource)
	if err != nil {
		return BrowserCommand{}, nil, nil, err
	}
	if request.Method != http.MethodGet || len(resource.Body.Bytes) != 0 {
		return BrowserCommand{}, nil, nil, errors.New("browser navigation supports GET requests without a body")
	}
	headers := make(map[string]string, len(request.Header))
	for name, values := range request.Header {
		headers[name] = strings.Join(values, ", ")
	}
	if err := validateDOM(resource.DOM); err != nil {
		return BrowserCommand{}, nil, nil, err
	}
	return BrowserCommand{
		URL: request.URL.String(), Headers: headers, DOM: append([]provider.DOMExtraction(nil), resource.DOM...),
		State: cloneState(state), Headed: executor.headed, MaxPageSize: executor.maxResponseSize,
	}, request.URL, sensitiveNames, nil
}

func validateDOM(operations []provider.DOMExtraction) error {
	names := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		if strings.TrimSpace(operation.Name) == "" || operation.Name != strings.TrimSpace(operation.Name) || len(operation.Name) > 128 {
			return errors.New("DOM extraction name is invalid")
		}
		if _, exists := names[operation.Name]; exists {
			return errors.New("DOM extraction names must be unique")
		}
		names[operation.Name] = struct{}{}
		if strings.TrimSpace(operation.Selector) == "" || len(operation.Selector) > 4096 || strings.ContainsRune(operation.Selector, '\x00') {
			return errors.New("DOM extraction selector is invalid")
		}
		switch operation.Kind {
		case provider.DOMText, provider.DOMHTML:
			if operation.Attribute != "" {
				return errors.New("DOM text and HTML extraction must not specify an attribute")
			}
		case provider.DOMAttribute:
			if !validHeaderName(operation.Attribute) || len(operation.Attribute) > 128 {
				return errors.New("DOM attribute extraction requires a safe attribute name")
			}
		default:
			return errors.New("DOM extraction kind is invalid")
		}
	}
	return nil
}

func validateBrowserResult(requestedURL *url.URL, result BrowserResult, limit int64) error {
	finalURL, err := url.Parse(result.FinalURL)
	if err != nil || finalURL.Scheme == "" || finalURL.Host == "" || finalURL.User != nil || finalURL.Fragment != "" ||
		(finalURL.Scheme != "http" && finalURL.Scheme != "https") {
		return provider.NewError(provider.ErrorCodeBrowserFailure, "the browser returned an unsafe final URL", nil)
	}
	if !sameOrigin(requestedURL, finalURL) {
		return provider.NewError(provider.ErrorCodeBrowserFailure, "the browser redirected to a different origin", nil)
	}
	if result.StatusCode < 100 || result.StatusCode > 599 {
		return provider.NewError(provider.ErrorCodeBrowserFailure, "the browser returned an invalid HTTP status", nil)
	}
	size := int64(len(result.HTML))
	for _, values := range result.DOM {
		for _, value := range values {
			size += int64(len(value))
			if size > limit {
				return provider.NewError(provider.ErrorCodeResponseTooLarge, "the browser page exceeds the configured size limit", nil)
			}
		}
	}
	if size > limit {
		return provider.NewError(provider.ErrorCodeResponseTooLarge, "the browser page exceeds the configured size limit", nil)
	}
	return nil
}

func classifyBrowserResult(result BrowserResult) error {
	if isBrowserChallenge(result.Headers, result.HTML) {
		return provider.NewError(provider.ErrorCodeBrowserChallengeRequired, "the website requires a browser challenge", nil)
	}
	if result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden || isAccessBlock(result.Headers, result.HTML) {
		return provider.NewError(provider.ErrorCodeAccessBlocked, "the website blocked browser access", nil)
	}
	if result.StatusCode >= http.StatusBadRequest {
		return provider.NewError(provider.ErrorCodeHTTPFailure, "the website returned HTTP status "+strconv.Itoa(result.StatusCode), nil)
	}
	return nil
}

func cloneDOM(input map[string][]string) map[string][]string {
	if input == nil {
		return nil
	}
	result := make(map[string][]string, len(input))
	for name, values := range input {
		result[name] = append([]string(nil), values...)
	}
	return result
}

func cloneState(state session.State) session.State {
	state.Cookies = append([]session.Cookie(nil), state.Cookies...)
	for index := range state.Cookies {
		if state.Cookies[index].Expires != nil {
			expires := *state.Cookies[index].Expires
			state.Cookies[index].Expires = &expires
		}
	}
	state.Origins = append([]session.Origin(nil), state.Origins...)
	for index := range state.Origins {
		state.Origins[index].LocalStorage = append([]session.StorageEntry(nil), state.Origins[index].LocalStorage...)
	}
	return state
}

var _ provider.ResourceService = (*BrowserExecutor)(nil)
