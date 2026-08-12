// Package maintenance provides Core cache and browser-session maintenance.
package maintenance

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/kostyay/ecom/internal/cache"
	"github.com/kostyay/ecom/internal/session"
	"github.com/kostyay/ecom/provider"
)

var providerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// ResponseResult reports response entries and estimated stored bytes removed.
type ResponseResult struct {
	EntriesDeleted int64
	BytesReleased  int64
}

// SessionResult reports if one exact browser session was deleted.
type SessionResult struct {
	Deleted bool
}

type responseService interface {
	Prune(context.Context) (cache.PruneResult, error)
	Clear(context.Context) (cache.PruneResult, error)
	ClearProvider(context.Context, string) (cache.PruneResult, error)
}

// Service is the Core entry point for storage maintenance. It does not expose SQL.
type Service struct {
	responses responseService
	sessions  session.Repository
}

// NewService makes a maintenance service with separate response and session stores.
func NewService(responses responseService, sessions session.Repository) (*Service, error) {
	if responses == nil {
		return nil, errors.New("response maintenance service is required")
	}
	if sessions == nil {
		return nil, errors.New("browser session repository is required")
	}
	return &Service{responses: responses, sessions: sessions}, nil
}

// PruneResponses removes expired and LRU response entries to meet configured limits.
func (service *Service) PruneResponses(ctx context.Context) (ResponseResult, error) {
	result, err := service.responses.Prune(ctx)
	if err != nil {
		return ResponseResult{}, fmt.Errorf("prune responses: %w", err)
	}
	return responseResult(result), nil
}

// ClearResponses removes all response entries and no browser sessions.
func (service *Service) ClearResponses(ctx context.Context) (ResponseResult, error) {
	result, err := service.responses.Clear(ctx)
	if err != nil {
		return ResponseResult{}, fmt.Errorf("clear responses: %w", err)
	}
	return responseResult(result), nil
}

// ClearProviderResponses removes response entries for one exact provider.
func (service *Service) ClearProviderResponses(ctx context.Context, providerName string) (ResponseResult, error) {
	if err := validateProviderName(providerName); err != nil {
		return ResponseResult{}, err
	}
	result, err := service.responses.ClearProvider(ctx, providerName)
	if err != nil {
		return ResponseResult{}, fmt.Errorf("clear responses for provider %q: %w", providerName, err)
	}
	return responseResult(result), nil
}

// ClearSession removes browser state for one exact provider and market.
func (service *Service) ClearSession(
	ctx context.Context,
	providerName string,
	market provider.Market,
) (SessionResult, error) {
	if err := validateProviderName(providerName); err != nil {
		return SessionResult{}, err
	}
	if err := market.Validate(); err != nil {
		return SessionResult{}, fmt.Errorf("browser session market: %w", err)
	}
	if market.Country != strings.TrimSpace(market.Country) ||
		market.Language != strings.TrimSpace(market.Language) ||
		market.Currency != strings.TrimSpace(market.Currency) {
		return SessionResult{}, errors.New("browser session market values must not have surrounding whitespace")
	}
	deleted, err := service.sessions.Delete(ctx, providerName, market)
	if err != nil {
		return SessionResult{}, fmt.Errorf("clear browser session for provider %q: %w", providerName, err)
	}
	return SessionResult{Deleted: deleted}, nil
}

func validateProviderName(name string) error {
	if !providerNamePattern.MatchString(name) || len(name) > 63 {
		return errors.New("provider name is invalid")
	}
	return nil
}

func responseResult(result cache.PruneResult) ResponseResult {
	return ResponseResult{EntriesDeleted: result.EntriesDeleted, BytesReleased: result.BytesDeleted}
}
