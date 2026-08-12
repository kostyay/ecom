package transport

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/provider"
)

const (
	// BaseRetryDelay is the first exponential retry delay before jitter.
	BaseRetryDelay = time.Second
	// MaximumRetryDelay bounds exponential delays and Retry-After values. This
	// prevents a website from making a command wait for an impractical period.
	MaximumRetryDelay = time.Minute
)

// Random supplies values used to add bounded variation to retry delays.
type Random interface {
	Float64() float64
}

// RandomFunc adapts a function to Random.
type RandomFunc func() float64

// Float64 returns one random value.
func (function RandomFunc) Float64() float64 { return function() }

type permitAcquirer interface {
	Acquire(context.Context, string, provider.TransportMode) (*RequestPermit, error)
}

// RetryExecutor applies HTTP request limits and retry policy to a one-attempt
// resource service. Retries is the number of attempts after the first attempt.
type RetryExecutor struct {
	next         provider.ResourceService
	limits       permitAcquirer
	providerName string
	retries      int
	scheduler    WaitScheduler
	random       Random
}

// NewRetryExecutor creates an HTTP retry executor. Each attempt, including a
// retry, obtains a separate request permit.
func NewRetryExecutor(next provider.ResourceService, limits permitAcquirer, providerName string, retries int, scheduler WaitScheduler, random Random) (*RetryExecutor, error) {
	if next == nil {
		return nil, errors.New("retry resource service is required")
	}
	if limits == nil {
		return nil, errors.New("retry request limit manager is required")
	}
	if strings.TrimSpace(providerName) == "" || strings.TrimSpace(providerName) != providerName {
		return nil, errors.New("retry provider is required")
	}
	if retries < 0 {
		return nil, errors.New("retry count must not be negative")
	}
	if scheduler == nil {
		return nil, errors.New("retry scheduler is required")
	}
	if random == nil {
		return nil, errors.New("retry random source is required")
	}
	return &RetryExecutor{
		next: next, limits: limits, providerName: providerName, retries: retries,
		scheduler: scheduler, random: random,
	}, nil
}

// NewConfiguredRetryExecutor creates an executor with the configured retry
// count and production randomness.
func NewConfiguredRetryExecutor(next provider.ResourceService, limits *RequestLimitManager, providerName string, settings config.NetworkSettings, scheduler WaitScheduler) (*RetryExecutor, error) {
	return NewRetryExecutor(next, limits, providerName, settings.Retries, scheduler, RandomFunc(rand.Float64))
}

// Fetch performs at most retries+1 attempts. Only retryable_http errors cause
// another attempt.
func (executor *RetryExecutor) Fetch(ctx context.Context, resource provider.ResourceRequest) (provider.ResourceResponse, error) {
	var lastResponse provider.ResourceResponse
	for attempt := 0; attempt <= executor.retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return lastResponse, err
		}
		if attempt > 0 {
			delay := executor.retryDelay(lastResponse, attempt)
			if err := executor.scheduler.Wait(ctx, delay); err != nil {
				return lastResponse, err
			}
		}

		permit, err := executor.limits.Acquire(ctx, executor.providerName, provider.TransportHTTP)
		if err != nil {
			return lastResponse, err
		}
		response, fetchErr := executor.fetchWithPermit(ctx, permit, resource)
		lastResponse = response
		if fetchErr == nil {
			return response, nil
		}
		if !errors.Is(fetchErr, provider.ErrorCodeRetryableHTTP) || attempt == executor.retries {
			return response, fmt.Errorf("fetch HTTP resource after %d attempt(s): %w", attempt+1, fetchErr)
		}
	}
	panic("unreachable")
}

func (executor *RetryExecutor) fetchWithPermit(ctx context.Context, permit *RequestPermit, resource provider.ResourceRequest) (provider.ResourceResponse, error) {
	defer permit.Release()
	return executor.next.Fetch(ctx, resource)
}

func (executor *RetryExecutor) retryDelay(response provider.ResourceResponse, retryNumber int) time.Duration {
	if delay, ok := retryAfterDelay(response.SafeHeaders, executor.scheduler.Now()); ok {
		return min(delay, MaximumRetryDelay)
	}
	delay := exponentialDelay(retryNumber)
	variation := executor.random.Float64()
	if variation < 0 {
		variation = 0
	} else if variation > 1 {
		variation = 1
	}
	// Keep the randomized delay between 50% and 100% of the exponential
	// delay. This gives tests and operators a strict upper bound.
	return time.Duration(float64(delay) * (0.5 + variation/2))
}

func exponentialDelay(retryNumber int) time.Duration {
	delay := BaseRetryDelay
	for retry := 1; retry < retryNumber && delay < MaximumRetryDelay; retry++ {
		if delay > MaximumRetryDelay/2 {
			return MaximumRetryDelay
		}
		delay *= 2
	}
	return min(delay, MaximumRetryDelay)
}

func retryAfterDelay(headers map[string][]string, now time.Time) (time.Duration, bool) {
	for name, values := range headers {
		if !strings.EqualFold(name, "Retry-After") {
			continue
		}
		for _, value := range values {
			value = strings.TrimSpace(value)
			if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
				if seconds >= int64(MaximumRetryDelay/time.Second) {
					return MaximumRetryDelay, true
				}
				return time.Duration(seconds) * time.Second, true
			}
			when, err := http.ParseTime(value)
			if err == nil && when.After(now) {
				return when.Sub(now), true
			}
		}
	}
	return 0, false
}

var _ provider.ResourceService = (*RetryExecutor)(nil)
