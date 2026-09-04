package provider_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
)

type fakeResourceService struct {
	request  provider.ResourceRequest
	response provider.ResourceResponse
	err      error
	called   chan struct{}
}

func (f *fakeResourceService) Fetch(ctx context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
	f.request = request
	if f.called != nil {
		close(f.called)
		<-ctx.Done()
		return provider.ResourceResponse{}, ctx.Err()
	}
	return f.response, f.err
}

type resourceFixtureProvider struct {
	resources provider.ResourceService
}

func (p resourceFixtureProvider) Search(ctx context.Context, request provider.SearchRequest) (provider.ProductPage, error) {
	resources := request.Resources
	if resources == nil {
		resources = p.resources
	}
	response, err := resources.Fetch(ctx, provider.ResourceRequest{
		Method: "GET",
		URL:    "https://shop.example/search",
		Query: []provider.RequestValue{
			{Name: "q", Values: []string{request.Query}},
			{Name: "token", Values: []string{"secret"}, Sensitive: true},
		},
		Headers: []provider.RequestValue{
			{Name: "Accept", Values: []string{"application/json"}},
		},
		Transport: provider.TransportPolicy{
			Preferred: []provider.TransportMode{provider.TransportHTTP, provider.TransportBrowser},
		},
		Market:         request.Market,
		Cache:          request.Cache,
		Interactive:    request.Interactive,
		CachePartition: "fixture-session",
	})
	if err != nil {
		return provider.ProductPage{}, err
	}

	return provider.ProductPage{Items: []provider.ProductSummary{{
		ID:          string(response.Body),
		RetrievedAt: response.RetrievedAt,
	}}}, nil
}

func TestProviderUsesFakeResourceServiceOffline(t *testing.T) {
	retrievedAt := time.Date(2026, time.August, 12, 17, 0, 0, 0, time.UTC)
	service := &fakeResourceService{response: provider.ResourceResponse{
		Body:        []byte("item-1"),
		StatusCode:  200,
		FinalURL:    "https://shop.example/search?q=helmet",
		SafeHeaders: map[string][]string{"Content-Type": {"application/json"}},
		RetrievedAt: retrievedAt,
		Transport:   provider.TransportHTTP,
		Cache: provider.CacheMetadata{
			Hit:      true,
			StoredAt: retrievedAt.Add(-time.Hour),
			Age:      time.Hour,
			TTL:      24 * time.Hour,
		},
	}}
	implementation := resourceFixtureProvider{resources: service}
	request := provider.SearchRequest{
		Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"},
		Cache:  provider.CachePolicy{Refresh: true, StaleIfError: true}, Interactive: true, Resources: service,
		Query: "helmet",
	}

	result, err := implementation.Search(t.Context(), request)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "item-1" || !result.Items[0].RetrievedAt.Equal(retrievedAt) {
		t.Fatalf("Search() result = %#v", result)
	}

	wantRequest := provider.ResourceRequest{
		Method: "GET",
		URL:    "https://shop.example/search",
		Query: []provider.RequestValue{
			{Name: "q", Values: []string{"helmet"}},
			{Name: "token", Values: []string{"secret"}, Sensitive: true},
		},
		Headers: []provider.RequestValue{{Name: "Accept", Values: []string{"application/json"}}},
		Transport: provider.TransportPolicy{
			Preferred: []provider.TransportMode{provider.TransportHTTP, provider.TransportBrowser},
		},
		Market:         request.Market,
		Cache:          provider.CachePolicy{Refresh: true, StaleIfError: true},
		Interactive:    true,
		CachePartition: "fixture-session",
	}
	if !reflect.DeepEqual(service.request, wantRequest) {
		t.Fatalf("Fetch() request = %#v, want %#v", service.request, wantRequest)
	}
}

func TestProviderPassesCancellationToResourceService(t *testing.T) {
	service := &fakeResourceService{called: make(chan struct{})}
	implementation := resourceFixtureProvider{resources: service}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)

	go func() {
		_, err := implementation.Search(ctx, provider.SearchRequest{Query: "helmet"})
		result <- err
	}()

	<-service.called
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Search() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Search() did not propagate cancellation")
	}
}

var _ provider.ResourceService = (*fakeResourceService)(nil)
