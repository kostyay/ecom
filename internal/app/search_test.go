package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/kostyay/ecom/provider"
)

type resourceServiceFunc func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error)

func (function resourceServiceFunc) Fetch(ctx context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
	return function(ctx, request)
}

func TestMetadataResourceServiceAppliesCommandPolicyAndCollectsMetadata(t *testing.T) {
	market := provider.Market{Country: "DE", Language: "en", Currency: "EUR"}
	cachePolicy := provider.CachePolicy{Refresh: true, StaleIfError: true}
	wantResponse := provider.ResourceResponse{
		Cache:    provider.CacheMetadata{Hit: true, Stale: true, Age: time.Hour, TTL: 24 * time.Hour},
		Attempts: []provider.TransportAttempt{{Mode: provider.TransportHTTP, Outcome: provider.AttemptSucceeded, Duration: time.Millisecond}},
	}
	var gotRequest provider.ResourceRequest
	next := resourceServiceFunc(func(_ context.Context, request provider.ResourceRequest) (provider.ResourceResponse, error) {
		gotRequest = request
		return wantResponse, nil
	})
	service := newMetadataResourceService(next, market, cachePolicy, true)

	response, err := service.Fetch(t.Context(), provider.ResourceRequest{
		Market: provider.Market{Country: "US", Language: "en", Currency: "USD"},
	})
	if err != nil || !reflect.DeepEqual(response, wantResponse) {
		t.Fatalf("Fetch() = %#v, %v", response, err)
	}
	if gotRequest.Market != market || gotRequest.Cache != cachePolicy || !gotRequest.Interactive {
		t.Errorf("resource request policy = %#v", gotRequest)
	}
	metadata := service.Metadata()
	if len(metadata.Resources) != 1 || metadata.Resources[0].Cache != wantResponse.Cache || !reflect.DeepEqual(metadata.Resources[0].Attempts, wantResponse.Attempts) {
		t.Errorf("metadata = %#v", metadata)
	}
}

func TestMetadataResourceServiceRecordsFailedResponseMetadata(t *testing.T) {
	wantErr := errors.New("fetch failed")
	next := resourceServiceFunc(func(context.Context, provider.ResourceRequest) (provider.ResourceResponse, error) {
		return provider.ResourceResponse{Attempts: []provider.TransportAttempt{{Mode: provider.TransportHTTP, Outcome: provider.AttemptFailed}}}, wantErr
	})
	service := newMetadataResourceService(next, provider.Market{}, provider.CachePolicy{}, false)
	_, err := service.Fetch(t.Context(), provider.ResourceRequest{})
	if !errors.Is(err, wantErr) || len(service.Metadata().Resources) != 1 {
		t.Errorf("Fetch() error = %v; metadata = %#v", err, service.Metadata())
	}
}
