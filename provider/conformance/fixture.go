package conformance

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/kostyay/ecom/provider"
)

// ResourceFixture is one ordered offline ResourceService response.
// CheckRequest can verify the provider's site-specific request.
type ResourceFixture struct {
	Response     provider.ResourceResponse
	Err          error
	CheckRequest func(provider.ResourceRequest) error
}

// FixtureService is an ordered, concurrency-safe offline ResourceService.
// It returns an error when a provider requests more resources than supplied.
type FixtureService struct {
	mu       sync.Mutex
	fixtures []ResourceFixture
	requests []provider.ResourceRequest
}

// NewFixtureService creates an offline service from ordered fixtures.
func NewFixtureService(fixtures ...ResourceFixture) *FixtureService {
	return &FixtureService{fixtures: append([]ResourceFixture(nil), fixtures...)}
}

// Fetch implements provider.ResourceService.
func (s *FixtureService) Fetch(ctx context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
	if err := ctx.Err(); err != nil {
		return provider.ResourceResponse{}, err
	}
	if s == nil {
		return provider.ResourceResponse{}, errors.New("fixture resource service is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := len(s.requests)
	s.requests = append(s.requests, request)
	if index >= len(s.fixtures) {
		return provider.ResourceResponse{}, fmt.Errorf("no resource fixture for request %d: %s %s", index+1, request.Method, request.URL)
	}
	fixture := s.fixtures[index]
	if fixture.CheckRequest != nil {
		if err := fixture.CheckRequest(request); err != nil {
			return provider.ResourceResponse{}, fmt.Errorf("resource request %d: %w", index+1, err)
		}
	}
	return fixture.Response, fixture.Err
}

// Requests returns a snapshot of all received resource requests.
func (s *FixtureService) Requests() []provider.ResourceRequest {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]provider.ResourceRequest(nil), s.requests...)
}

// Stats returns the request count and the number of unused fixtures.
func (s *FixtureService) Stats() (requests, remaining int) {
	if s == nil {
		return 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	requests = len(s.requests)
	remaining = max(len(s.fixtures)-requests, 0)
	return requests, remaining
}

var _ provider.ResourceService = (*FixtureService)(nil)
