package provider

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
)

// APIVersion is the provider SDK API version supported by this package.
// Providers pass this value when they register.
const APIVersion = 1

var (
	// ErrInvalidProviderName means that a registration name is not a valid provider identifier.
	ErrInvalidProviderName = errors.New("invalid provider name")
	// ErrNilProvider means that a registration has no provider implementation.
	ErrNilProvider = errors.New("nil provider implementation")
	// ErrDuplicateProvider means that a provider name is already registered.
	ErrDuplicateProvider = errors.New("duplicate provider")
	// ErrIncompatibleAPIVersion means that a provider uses an unsupported SDK API version.
	ErrIncompatibleAPIVersion = errors.New("incompatible provider SDK API version")
	// ErrInvalidProviderImplementation means that an implementation does not satisfy its declared operations.
	ErrInvalidProviderImplementation = errors.New("invalid provider implementation")
	// ErrInvalidCapability means that a registration declares an unknown or duplicate capability.
	ErrInvalidCapability = errors.New("invalid provider capability")

	providerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	defaultRegistry     = NewRegistry()
)

// Registration describes a provider implementation that is added to a Registry.
//
// Provider modules normally call MustRegister from init. This lets a CLI enable
// the module with a blank import.
type Registration struct {
	Name           string
	SDKAPIVersion  int
	Implementation any
	Capabilities   []CapabilityName
}

// Registry stores provider implementations by their stable names.
// A Registry is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry returns an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

// Register validates and adds one provider implementation.
func (r *Registry) Register(registration Registration) error {
	if r == nil {
		return errors.New("provider registry is nil")
	}
	if !validProviderName(registration.Name) {
		return fmt.Errorf("%w %q: use at most 63 lowercase letters, digits, and single hyphens; start with a letter", ErrInvalidProviderName, registration.Name)
	}
	if isNil(registration.Implementation) {
		return fmt.Errorf("%w for %q", ErrNilProvider, registration.Name)
	}
	if registration.SDKAPIVersion != APIVersion {
		return fmt.Errorf(
			"%w: provider %q uses version %d; core supports version %d",
			ErrIncompatibleAPIVersion,
			registration.Name,
			registration.SDKAPIVersion,
			APIVersion,
		)
	}

	registered, err := newRegisteredProvider(registration)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.providers == nil {
		r.providers = make(map[string]Provider)
	}
	if _, exists := r.providers[registration.Name]; exists {
		return fmt.Errorf("%w %q", ErrDuplicateProvider, registration.Name)
	}
	r.providers[registration.Name] = registered
	return nil
}

// Lookup returns the provider implementation registered with name.
func (r *Registry) Lookup(name string) (Provider, bool) {
	if r == nil {
		return nil, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	implementation, exists := r.providers[name]
	return implementation, exists
}

// Resolve returns the selected provider or a stable error that is safe to
// return from a command. Lookup remains available for optional probes.
func (r *Registry) Resolve(name string) (Provider, error) {
	if strings.TrimSpace(name) == "" {
		return nil, NewError(ErrorCodeProviderRequired, "select a provider with --provider or configuration", nil)
	}
	implementation, found := r.Lookup(name)
	if !found {
		return nil, NewError(ErrorCodeProviderNotFound, fmt.Sprintf("provider %q is not available", name), nil)
	}
	return implementation, nil
}

// Register validates and adds one provider to the package registry.
func Register(registration Registration) error {
	return defaultRegistry.Register(registration)
}

// MustRegister adds one provider to the package registry. It panics if the
// registration is invalid. Provider modules can call MustRegister from init so
// an invalid compiled distribution fails during startup.
func MustRegister(registration Registration) {
	if err := Register(registration); err != nil {
		panic(fmt.Sprintf("register provider: %v", err))
	}
}

// Lookup returns a provider implementation from the package registry.
func Lookup(name string) (Provider, bool) {
	return defaultRegistry.Lookup(name)
}

// Resolve returns a provider from the package registry with stable selection
// errors.
func Resolve(name string) (Provider, error) {
	return defaultRegistry.Resolve(name)
}

func newRegisteredProvider(registration Registration) (*registeredProvider, error) {
	helper, ok := registration.Implementation.(HelpProvider)
	if !ok {
		return nil, fmt.Errorf("%w for %q: implementation must implement HelpProvider", ErrInvalidProviderImplementation, registration.Name)
	}

	registered := &registeredProvider{
		name:         registration.Name,
		capabilities: append([]CapabilityName(nil), registration.Capabilities...),
		supported:    make(map[CapabilityName]struct{}, len(registration.Capabilities)),
		help:         helper.Help,
	}
	if validator, ok := registration.Implementation.(ConfigValidator); ok {
		registered.validateConfig = validator.ValidateConfig
	}
	for _, capability := range registration.Capabilities {
		if _, exists := registered.supported[capability]; exists {
			return nil, fmt.Errorf("%w for %q: duplicate capability %q", ErrInvalidCapability, registration.Name, capability)
		}
		if err := registerCapability(registered, registration.Implementation, capability); err != nil {
			if errors.Is(err, ErrInvalidCapability) {
				return nil, fmt.Errorf("%w for %q", err, registration.Name)
			}
			return nil, fmt.Errorf("%w for %q: %w", ErrInvalidProviderImplementation, registration.Name, err)
		}
		registered.supported[capability] = struct{}{}
	}
	if registered.Supports(CapabilityVariantSelection) && !registered.Supports(CapabilityItem) {
		return nil, fmt.Errorf(
			"%w for %q: capability %q requires declared capability %q",
			ErrInvalidCapability,
			registration.Name,
			CapabilityVariantSelection,
			CapabilityItem,
		)
	}
	return registered, nil
}

func registerCapability(registered *registeredProvider, implementation any, capability CapabilityName) error {
	switch capability {
	case CapabilitySearch:
		operation, ok := implementation.(SearchProvider)
		if !ok {
			return missingCapabilityInterface(capability, "SearchProvider")
		}
		registered.search = operation.Search
	case CapabilityCategories:
		operation, ok := implementation.(CategoryListProvider)
		if !ok {
			return missingCapabilityInterface(capability, "CategoryListProvider")
		}
		registered.categories = operation.Categories
	case CapabilityCategorySearch:
		operation, ok := implementation.(CategorySearchProvider)
		if !ok {
			return missingCapabilityInterface(capability, "CategorySearchProvider")
		}
		registered.searchCategories = operation.SearchCategories
	case CapabilityCategoryItems:
		operation, ok := implementation.(CategoryItemsProvider)
		if !ok {
			return missingCapabilityInterface(capability, "CategoryItemsProvider")
		}
		registered.categoryItems = operation.CategoryItems
	case CapabilityBrands:
		operation, ok := implementation.(BrandListProvider)
		if !ok {
			return missingCapabilityInterface(capability, "BrandListProvider")
		}
		registered.brands = operation.Brands
	case CapabilityBrandSearch:
		operation, ok := implementation.(BrandSearchProvider)
		if !ok {
			return missingCapabilityInterface(capability, "BrandSearchProvider")
		}
		registered.searchBrands = operation.SearchBrands
	case CapabilityBrandItems:
		operation, ok := implementation.(BrandItemsProvider)
		if !ok {
			return missingCapabilityInterface(capability, "BrandItemsProvider")
		}
		registered.brandItems = operation.BrandItems
	case CapabilityDeals:
		operation, ok := implementation.(DealsProvider)
		if !ok {
			return missingCapabilityInterface(capability, "DealsProvider")
		}
		registered.deals = operation.Deals
	case CapabilityFilters:
		operation, ok := implementation.(FiltersProvider)
		if !ok {
			return missingCapabilityInterface(capability, "FiltersProvider")
		}
		registered.filters = operation.Filters
	case CapabilityItem:
		operation, ok := implementation.(ItemProvider)
		if !ok {
			return missingCapabilityInterface(capability, "ItemProvider")
		}
		registered.item = operation.Item
	case CapabilityVariantSelection:
		if _, ok := implementation.(ItemProvider); !ok {
			return missingCapabilityInterface(capability, "ItemProvider")
		}
	default:
		return fmt.Errorf("%w %q", ErrInvalidCapability, capability)
	}
	return nil
}

func missingCapabilityInterface(capability CapabilityName, interfaceName string) error {
	return fmt.Errorf("capability %q requires %s", capability, interfaceName)
}

func validProviderName(name string) bool {
	return len(name) <= 63 && providerNamePattern.MatchString(name)
}

func isNil(value any) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	kind := reflected.Kind()
	switch kind {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
