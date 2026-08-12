package provider_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kostyay/ecom/provider"
)

type testProvider struct {
	value int
}

func (p *testProvider) Help(context.Context, provider.HelpRequest) (provider.HelpResult, error) {
	return provider.HelpResult{Help: provider.Help{Name: "example", Description: fmt.Sprint(p.value)}}, nil
}

var globalTestSequence atomic.Uint64

func TestRegistryRegisterAndLookup(t *testing.T) {
	registry := provider.NewRegistry()
	implementation := &testProvider{value: 42}

	err := registry.Register(provider.Registration{
		Name:           "bike-discount",
		SDKAPIVersion:  provider.APIVersion,
		Implementation: implementation,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	got, ok := registry.Lookup("bike-discount")
	if !ok {
		t.Fatal("Lookup() did not find the provider")
	}
	if got.Name() != "bike-discount" {
		t.Fatalf("Lookup().Name() = %q, want %q", got.Name(), "bike-discount")
	}
	if _, ok := registry.Lookup("missing"); ok {
		t.Fatal("Lookup() found an unregistered provider")
	}
}

func TestRegistryResolve(t *testing.T) {
	registry := provider.NewRegistry()
	if err := registry.Register(provider.Registration{
		Name: "example", SDKAPIVersion: provider.APIVersion, Implementation: &testProvider{},
	}); err != nil {
		t.Fatal(err)
	}

	selected, err := registry.Resolve("example")
	if err != nil || selected.Name() != "example" {
		t.Fatalf("Resolve(known) = %#v, %v", selected, err)
	}
	for _, name := range []string{"", "   "} {
		if _, err := registry.Resolve(name); !errors.Is(err, provider.ErrorCodeProviderRequired) {
			t.Errorf("Resolve(%q) error = %v, want provider_required", name, err)
		}
	}
	if _, err := registry.Resolve("missing"); !errors.Is(err, provider.ErrorCodeProviderNotFound) {
		t.Errorf("Resolve(missing) error = %v, want provider_not_found", err)
	}
}

func TestRegistryZeroValueIsUsable(t *testing.T) {
	var registry provider.Registry
	implementation := &testProvider{}
	if err := registry.Register(provider.Registration{
		Name: "zero-value", SDKAPIVersion: provider.APIVersion, Implementation: implementation,
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if got, ok := registry.Lookup("zero-value"); !ok || got.Name() != "zero-value" {
		t.Fatalf("Lookup() = %#v, %t; want registered provider", got, ok)
	}
}

func TestRegistryRejectsInvalidRegistrations(t *testing.T) {
	var nilImplementation *testProvider
	tests := []struct {
		name         string
		registration provider.Registration
		want         error
	}{
		{
			name: "empty name",
			registration: provider.Registration{
				SDKAPIVersion: provider.APIVersion, Implementation: &testProvider{},
			},
			want: provider.ErrInvalidProviderName,
		},
		{
			name: "uppercase name",
			registration: provider.Registration{
				Name: "Bike-Discount", SDKAPIVersion: provider.APIVersion, Implementation: &testProvider{},
			},
			want: provider.ErrInvalidProviderName,
		},
		{
			name: "name with repeated hyphen",
			registration: provider.Registration{
				Name: "bike--discount", SDKAPIVersion: provider.APIVersion, Implementation: &testProvider{},
			},
			want: provider.ErrInvalidProviderName,
		},
		{
			name: "nil interface",
			registration: provider.Registration{
				Name: "nil-interface", SDKAPIVersion: provider.APIVersion,
			},
			want: provider.ErrNilProvider,
		},
		{
			name: "typed nil",
			registration: provider.Registration{
				Name: "typed-nil", SDKAPIVersion: provider.APIVersion, Implementation: nilImplementation,
			},
			want: provider.ErrNilProvider,
		},
		{
			name: "incompatible version",
			registration: provider.Registration{
				Name: "wrong-version", SDKAPIVersion: provider.APIVersion + 1, Implementation: &testProvider{},
			},
			want: provider.ErrIncompatibleAPIVersion,
		},
		{
			name: "missing help interface",
			registration: provider.Registration{
				Name: "missing-help", SDKAPIVersion: provider.APIVersion, Implementation: struct{}{},
			},
			want: provider.ErrInvalidProviderImplementation,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := provider.NewRegistry().Register(test.registration)
			if !errors.Is(err, test.want) {
				t.Fatalf("Register() error = %v, want errors.Is(_, %v)", err, test.want)
			}
		})
	}
}

func TestRegistryRejectsDuplicateAndKeepsFirstProvider(t *testing.T) {
	registry := provider.NewRegistry()
	first := &testProvider{value: 1}
	second := &testProvider{value: 2}

	if err := registry.Register(provider.Registration{Name: "example", SDKAPIVersion: provider.APIVersion, Implementation: first}); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	err := registry.Register(provider.Registration{Name: "example", SDKAPIVersion: provider.APIVersion, Implementation: second})
	if !errors.Is(err, provider.ErrDuplicateProvider) {
		t.Fatalf("second Register() error = %v, want errors.Is(_, ErrDuplicateProvider)", err)
	}

	got, ok := registry.Lookup("example")
	if !ok || providerDescription(t, got) != "1" {
		t.Fatalf("Lookup() = %#v, %t; want first provider", got, ok)
	}
}

func TestRegistrySupportsConcurrentRegistrationAndLookup(t *testing.T) {
	registry := provider.NewRegistry()
	const providerCount = 64

	var waitGroup sync.WaitGroup
	for index := range providerCount {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			name := fmt.Sprintf("provider-%d", index)
			if err := registry.Register(provider.Registration{
				Name: name, SDKAPIVersion: provider.APIVersion, Implementation: &testProvider{value: index},
			}); err != nil {
				t.Errorf("Register(%q) error = %v", name, err)
			}
		}()
	}

	for range providerCount {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for index := range providerCount {
				registry.Lookup(fmt.Sprintf("provider-%d", index))
			}
		}()
	}
	waitGroup.Wait()

	for index := range providerCount {
		name := fmt.Sprintf("provider-%d", index)
		if _, ok := registry.Lookup(name); !ok {
			t.Errorf("Lookup(%q) did not find the provider", name)
		}
	}
}

func TestPackageRegistrySupportsInitStyleRegistration(t *testing.T) {
	implementation := &testProvider{value: 7}
	name := fmt.Sprintf("registry-test-provider-%d", globalTestSequence.Add(1))
	provider.MustRegister(provider.Registration{
		Name:           name,
		SDKAPIVersion:  provider.APIVersion,
		Implementation: implementation,
	})

	got, ok := provider.Lookup(name)
	if !ok || got.Name() != name {
		t.Fatalf("Lookup() = %#v, %t; want registered provider", got, ok)
	}
}

func providerDescription(t *testing.T, implementation provider.Provider) string {
	t.Helper()
	result, err := implementation.Help(context.Background(), provider.HelpRequest{})
	if err != nil {
		t.Fatalf("Help() error = %v", err)
	}
	return result.Help.Description
}

func TestMustRegisterPanicExplainsFailure(t *testing.T) {
	defer func() {
		value := recover()
		if value == nil {
			t.Fatal("MustRegister() did not panic")
		}
		message := fmt.Sprint(value)
		if !strings.Contains(message, "incompatible provider SDK API version") || !strings.Contains(message, "wrong-version") {
			t.Fatalf("MustRegister() panic = %q", message)
		}
	}()

	provider.MustRegister(provider.Registration{
		Name:           "wrong-version",
		SDKAPIVersion:  provider.APIVersion + 1,
		Implementation: &testProvider{},
	})
}
