package maintenance

import (
	"context"
	"errors"
	"testing"

	"github.com/kostyay/ecom/internal/cache"
	"github.com/kostyay/ecom/internal/session"
	"github.com/kostyay/ecom/provider"
)

var errTestRepository = errors.New("repository failed")

type fakeResponses struct {
	pruneResult    cache.PruneResult
	clearResult    cache.PruneResult
	providerResult cache.PruneResult
	err            error
	provider       string
}

func (fake *fakeResponses) Prune(context.Context) (cache.PruneResult, error) {
	return fake.pruneResult, fake.err
}
func (fake *fakeResponses) Clear(context.Context) (cache.PruneResult, error) {
	return fake.clearResult, fake.err
}
func (fake *fakeResponses) ClearProvider(_ context.Context, name string) (cache.PruneResult, error) {
	fake.provider = name
	return fake.providerResult, fake.err
}

type fakeSessions struct {
	deleted  bool
	err      error
	provider string
	market   provider.Market
}

func (*fakeSessions) Put(context.Context, session.Record) (session.Record, error) {
	return session.Record{}, nil
}
func (*fakeSessions) Get(context.Context, string, provider.Market) (session.Record, error) {
	return session.Record{}, nil
}
func (fake *fakeSessions) Delete(_ context.Context, name string, market provider.Market) (bool, error) {
	fake.provider, fake.market = name, market
	return fake.deleted, fake.err
}

func TestServiceResponseOperations(t *testing.T) {
	responses := &fakeResponses{
		pruneResult:    cache.PruneResult{EntriesDeleted: 2, BytesDeleted: 20},
		clearResult:    cache.PruneResult{EntriesDeleted: 3, BytesDeleted: 30},
		providerResult: cache.PruneResult{EntriesDeleted: 1, BytesDeleted: 10},
	}
	service := newTestService(t, responses, &fakeSessions{})

	pruned, err := service.PruneResponses(context.Background())
	if err != nil || pruned != (ResponseResult{EntriesDeleted: 2, BytesReleased: 20}) {
		t.Fatalf("PruneResponses = %#v, %v", pruned, err)
	}
	cleared, err := service.ClearResponses(context.Background())
	if err != nil || cleared != (ResponseResult{EntriesDeleted: 3, BytesReleased: 30}) {
		t.Fatalf("ClearResponses = %#v, %v", cleared, err)
	}
	providerResult, err := service.ClearProviderResponses(context.Background(), "bike-discount")
	if err != nil || providerResult != (ResponseResult{EntriesDeleted: 1, BytesReleased: 10}) || responses.provider != "bike-discount" {
		t.Fatalf("ClearProviderResponses = %#v, %v, provider %q", providerResult, err, responses.provider)
	}
}

func TestServiceClearsExactSessionAndReportsIdempotence(t *testing.T) {
	market := provider.Market{Country: "DE", Language: "en", Currency: "EUR"}
	sessions := &fakeSessions{deleted: true}
	service := newTestService(t, &fakeResponses{}, sessions)
	result, err := service.ClearSession(context.Background(), "bike-discount", market)
	if err != nil || !result.Deleted || sessions.provider != "bike-discount" || sessions.market != market {
		t.Fatalf("ClearSession = %#v, %v; scope %q/%#v", result, err, sessions.provider, sessions.market)
	}
	sessions.deleted = false
	result, err = service.ClearSession(context.Background(), "bike-discount", market)
	if err != nil || result.Deleted {
		t.Fatalf("second ClearSession = %#v, %v", result, err)
	}
}

func TestServiceValidatesScopes(t *testing.T) {
	responses := &fakeResponses{}
	sessions := &fakeSessions{}
	service := newTestService(t, responses, sessions)
	if _, err := service.ClearProviderResponses(context.Background(), "Bike Discount"); err == nil {
		t.Fatal("ClearProviderResponses invalid provider error = nil")
	}
	if responses.provider != "" {
		t.Fatal("invalid provider reached response repository")
	}
	if _, err := service.ClearSession(context.Background(), "bike-discount", provider.Market{Country: "DE", Language: " en", Currency: "EUR"}); err == nil {
		t.Fatal("ClearSession invalid market error = nil")
	}
	if sessions.provider != "" {
		t.Fatal("invalid market reached session repository")
	}
}

func TestServicePreservesCancellationAndRepositoryErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	responses := &fakeResponses{err: context.Canceled}
	service := newTestService(t, responses, &fakeSessions{})
	if _, err := service.PruneResponses(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("PruneResponses error = %v", err)
	}
	responses.err = errTestRepository
	if _, err := service.ClearResponses(context.Background()); !errors.Is(err, errTestRepository) {
		t.Fatalf("ClearResponses error = %v", err)
	}
}

func TestNewServiceValidatesDependencies(t *testing.T) {
	if _, err := NewService(nil, &fakeSessions{}); err == nil {
		t.Fatal("NewService nil responses error = nil")
	}
	if _, err := NewService(&fakeResponses{}, nil); err == nil {
		t.Fatal("NewService nil sessions error = nil")
	}
}

func newTestService(t *testing.T, responses responseService, sessions session.Repository) *Service {
	t.Helper()
	service, err := NewService(responses, sessions)
	if err != nil {
		t.Fatal(err)
	}
	return service
}
