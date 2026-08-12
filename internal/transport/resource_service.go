package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/internal/storage/sqlite"
	"github.com/kostyay/ecom/provider"
)

var defaultTransportOrder = []provider.TransportMode{
	provider.TransportHTTP,
	provider.TransportBrowser,
	provider.TransportCDP,
}

// ResourceService selects one or more Core-owned transports. The concrete
// services own their request permits. This selector does not acquire another
// permit.
type ResourceService struct {
	services map[provider.TransportMode]provider.ResourceService
	clock    Clock
}

// NewResourceService creates a provider-facing ordered transport selector.
func NewResourceService(httpService, browserService, cdpService provider.ResourceService, clock Clock) (*ResourceService, error) {
	if httpService == nil {
		return nil, errors.New("HTTP resource service is required")
	}
	if browserService == nil {
		return nil, errors.New("browser resource service is required")
	}
	if cdpService == nil {
		return nil, errors.New("CDP resource service is required")
	}
	if clock == nil {
		return nil, errors.New("resource service clock is required")
	}
	return &ResourceService{
		services: map[provider.TransportMode]provider.ResourceService{
			provider.TransportHTTP:    httpService,
			provider.TransportBrowser: browserService,
			provider.TransportCDP:     cdpService,
		},
		clock: clock,
	}, nil
}

// NewConfiguredResourceService wires all transports with one database and one
// request-limit manager. Browser and CDP therefore share their concurrency
// pool, and all network attempts share the provider start-rate limit.
func NewConfiguredResourceService(database *sqlite.Database, client *http.Client, providerName string, settings config.Settings) (*ResourceService, error) {
	scheduler := RealWaitScheduler{}
	limits, err := NewRequestLimitManager(RequestLimitsFromConfig(settings.Network), nil, scheduler)
	if err != nil {
		return nil, err
	}
	httpService, err := newConfiguredHTTPResourceService(database, client, providerName, settings, limits, scheduler)
	if err != nil {
		return nil, fmt.Errorf("configure HTTP transport: %w", err)
	}
	browserService, err := newConfiguredBrowserResourceService(database, providerName, settings, limits)
	if err != nil {
		return nil, fmt.Errorf("configure browser transport: %w", err)
	}
	cdpService, err := newConfiguredCDPResourceService(providerName, settings, limits)
	if err != nil {
		return nil, fmt.Errorf("configure CDP transport: %w", err)
	}
	return NewResourceService(httpService, browserService, cdpService, ClockFunc(time.Now))
}

// Fetch tries transports in policy order and only crosses transport boundaries
// for classified errors which make the next transport useful.
func (service *ResourceService) Fetch(ctx context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
	order, err := transportOrder(request.Transport)
	if err != nil {
		return provider.ResourceResponse{}, provider.NewError(
			provider.ErrorCodeInvalidResourceRequest,
			"the resource transport policy is invalid",
			err,
		)
	}

	var attempts []provider.TransportAttempt
	var lastUseful provider.ResourceResponse
	var lastUsefulErr error
	for index, mode := range order {
		if err := ctx.Err(); err != nil {
			lastUseful.Attempts = cloneAttempts(attempts)
			return lastUseful, err
		}

		attemptRequest := cloneResourceRequest(request)
		attemptRequest.Transport = provider.TransportPolicy{Required: mode}
		startedAt := service.clock.Now()
		response, fetchErr := service.services[mode].Fetch(ctx, attemptRequest)
		attempt := provider.TransportAttempt{Mode: mode, Duration: elapsed(service.clock.Now(), startedAt)}
		if fetchErr == nil {
			attempt.Outcome = provider.AttemptSucceeded
			attempts = append(attempts, attempt)
			response.Attempts = cloneAttempts(attempts)
			return response, nil
		}
		if err := ctx.Err(); err != nil {
			attempt.Outcome = provider.AttemptFailed
			attempts = append(attempts, attempt)
			if usefulResponse(response) {
				lastUseful = response
			}
			lastUseful.Attempts = cloneAttempts(attempts)
			return lastUseful, err
		}

		code, coded := provider.ErrorCodeOf(fetchErr)
		if coded {
			attempt.Code = code
		}
		if errors.Is(fetchErr, provider.ErrorCodeTransportUnavailable) {
			attempt.Outcome = provider.AttemptUnavailable
		} else {
			attempt.Outcome = provider.AttemptFailed
			lastUsefulErr = fetchErr
		}
		attempts = append(attempts, attempt)
		if usefulResponse(response) {
			lastUseful = response
		}

		lastAttempt := index == len(order)-1
		if !lastAttempt && (errors.Is(fetchErr, provider.ErrorCodeTransportUnavailable) || eligibleFallback(mode, fetchErr)) {
			continue
		}
		lastUseful.Attempts = cloneAttempts(attempts)
		if errors.Is(fetchErr, provider.ErrorCodeTransportUnavailable) && lastUsefulErr != nil {
			return lastUseful, lastUsefulErr
		}
		return lastUseful, fetchErr
	}
	panic("transport order is never empty")
}

func transportOrder(policy provider.TransportPolicy) ([]provider.TransportMode, error) {
	if policy.Required != "" {
		if len(policy.Preferred) != 0 {
			return nil, errors.New("required and preferred transports cannot be combined")
		}
		if !knownTransport(policy.Required) {
			return nil, errors.New("required transport is unknown")
		}
		return []provider.TransportMode{policy.Required}, nil
	}
	if len(policy.Preferred) == 0 {
		return append([]provider.TransportMode(nil), defaultTransportOrder...), nil
	}
	result := make([]provider.TransportMode, 0, len(policy.Preferred))
	seen := make(map[provider.TransportMode]struct{}, len(policy.Preferred))
	for _, mode := range policy.Preferred {
		if !knownTransport(mode) {
			return nil, errors.New("preferred transport is unknown")
		}
		if _, found := seen[mode]; found {
			return nil, errors.New("preferred transports contain a duplicate")
		}
		seen[mode] = struct{}{}
		result = append(result, mode)
	}
	return result, nil
}

func knownTransport(mode provider.TransportMode) bool {
	switch mode {
	case provider.TransportHTTP, provider.TransportBrowser, provider.TransportCDP:
		return true
	default:
		return false
	}
}

func eligibleFallback(mode provider.TransportMode, err error) bool {
	switch mode {
	case provider.TransportHTTP:
		return errors.Is(err, provider.ErrorCodeAccessBlocked) ||
			errors.Is(err, provider.ErrorCodeBrowserChallengeRequired)
	case provider.TransportBrowser:
		return errors.Is(err, provider.ErrorCodeAccessBlocked) ||
			errors.Is(err, provider.ErrorCodeBrowserFailure) ||
			errors.Is(err, provider.ErrorCodeBrowserChallengeTimeout)
	default:
		return false
	}
}

func usefulResponse(response provider.ResourceResponse) bool {
	return len(response.Body) != 0 || response.Page != nil || response.StatusCode != 0 ||
		response.FinalURL != "" || !response.RetrievedAt.IsZero()
}

func elapsed(endedAt, startedAt time.Time) time.Duration {
	duration := endedAt.Sub(startedAt)
	if duration < 0 {
		return 0
	}
	return duration
}

func cloneAttempts(attempts []provider.TransportAttempt) []provider.TransportAttempt {
	return append([]provider.TransportAttempt(nil), attempts...)
}

func cloneResourceRequest(request provider.ResourceRequest) provider.ResourceRequest {
	clone := request
	clone.Query = cloneRequestValues(request.Query)
	clone.Headers = cloneRequestValues(request.Headers)
	clone.Body.Bytes = append([]byte(nil), request.Body.Bytes...)
	clone.Transport.Preferred = append([]provider.TransportMode(nil), request.Transport.Preferred...)
	clone.DOM = append([]provider.DOMExtraction(nil), request.DOM...)
	return clone
}

func cloneRequestValues(values []provider.RequestValue) []provider.RequestValue {
	clone := make([]provider.RequestValue, len(values))
	for index, value := range values {
		clone[index] = value
		clone[index].Values = append([]string(nil), value.Values...)
	}
	return clone
}

var _ provider.ResourceService = (*ResourceService)(nil)
