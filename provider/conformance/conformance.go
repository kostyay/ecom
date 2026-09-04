package conformance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kostyay/ecom/provider"
)

// Suite describes one provider contract test run. Cases must contain at least
// one case for each declared operation. A variant-selection case invokes Item
// with one or more VariantSelection values.
type Suite struct {
	Registration provider.Registration
	Cases        []OperationCase
	Resources    *FixtureService
}

// OperationCase invokes one declared provider operation with offline input.
// Invoke must return the public result type for Capability. WantErrorCode asks
// the suite to validate a structured error instead of a result.
type OperationCase struct {
	Name               string
	Capability         provider.CapabilityName
	Invoke             func(context.Context, provider.Provider) (any, error)
	WantErrorCode      provider.ErrorCode
	WantPartialWarning bool
	Check              func(any) error
}

// CheckResult is one named conformance check result.
type CheckResult struct {
	Name string
	Err  error
}

// Report contains all checks. It is useful for testing invalid fixtures
// without intercepting testing.T failures.
type Report struct {
	Checks []CheckResult
}

// Passed reports whether all checks succeeded.
func (r Report) Passed() bool {
	for _, check := range r.Checks {
		if check.Err != nil {
			return false
		}
	}
	return true
}

// Run runs one complete provider conformance suite as named subtests.
func Run(t *testing.T, suite Suite) {
	t.Helper()
	for _, check := range Check(t.Context(), suite).Checks {
		t.Run(check.Name, func(t *testing.T) {
			if check.Err != nil {
				t.Error(check.Err)
			}
		})
	}
}

// Check evaluates a suite without using testing.T. It never uses the network.
func Check(ctx context.Context, suite Suite) Report {
	checker := suiteChecker{ctx: ctx, suite: suite}
	checker.checkRegistration()
	if checker.registered == nil {
		return Report{Checks: checker.checks}
	}
	checker.checkHelp()
	checker.checkUnsupportedOperations()
	checker.checkCases()
	checker.checkResourceUse()
	return Report{Checks: checker.checks}
}

type suiteChecker struct {
	ctx        context.Context
	suite      Suite
	registered provider.Provider
	help       provider.Help
	checks     []CheckResult
}

func (c *suiteChecker) add(name string, err error) {
	c.checks = append(c.checks, CheckResult{Name: name, Err: err})
}

func (c *suiteChecker) checkRegistration() {
	registry := provider.NewRegistry()
	err := registry.Register(c.suite.Registration)
	c.add("registration", err)
	if err != nil {
		return
	}
	registered, ok := registry.Lookup(c.suite.Registration.Name)
	if !ok {
		c.add("registration_lookup", errors.New("registered provider was not found"))
		return
	}
	c.registered = registered
	if registered.Name() != c.suite.Registration.Name {
		c.add("registration_name", fmt.Errorf("provider name = %q, want %q", registered.Name(), c.suite.Registration.Name))
	} else {
		c.add("registration_name", nil)
	}
	for _, capability := range c.suite.Registration.Capabilities {
		if !registered.Supports(capability) {
			c.add("registration_capability_"+string(capability), fmt.Errorf("declared capability %q is not supported", capability))
		} else {
			c.add("registration_capability_"+string(capability), nil)
		}
	}
}

func (c *suiteChecker) checkHelp() {
	result, err := c.registered.Help(c.ctx, provider.HelpRequest{})
	if err != nil {
		c.add("help", fmt.Errorf("help returned an error: %w", err))
		return
	}
	c.help = result.Help
	if err := result.Help.Validate(); err != nil {
		c.add("help_validation", err)
	} else {
		c.add("help_validation", nil)
	}
	if result.Help.Name != c.registered.Name() {
		c.add("help_name", fmt.Errorf("help name = %q, want %q", result.Help.Name, c.registered.Name()))
	} else {
		c.add("help_name", nil)
	}

	helpCapabilities := make(map[provider.CapabilityName]bool, len(result.Help.Capabilities))
	for _, capability := range result.Help.Capabilities {
		helpCapabilities[capability.Name] = capability.Supported
		if !knownCapability(capability.Name) {
			c.add("help_capability_"+string(capability.Name), fmt.Errorf("help contains unknown capability %q", capability.Name))
			continue
		}
		if capability.Supported != c.registered.Supports(capability.Name) {
			c.add("help_capability_"+string(capability.Name), fmt.Errorf("help supported = %t, registration supported = %t", capability.Supported, c.registered.Supports(capability.Name)))
		} else {
			c.add("help_capability_"+string(capability.Name), nil)
		}
	}
	for _, capability := range c.registered.Capabilities() {
		if !helpCapabilities[capability] {
			c.add("help_declared_capability_"+string(capability), fmt.Errorf("help does not mark declared capability %q as supported", capability))
		} else {
			c.add("help_declared_capability_"+string(capability), nil)
		}
	}
}

func knownCapability(capability provider.CapabilityName) bool {
	if capability == provider.CapabilityVariantSelection {
		return true
	}
	for _, operation := range commonOperations() {
		if operation.capability == capability {
			return true
		}
	}
	return false
}

func (c *suiteChecker) checkUnsupportedOperations() {
	for _, operation := range commonOperations() {
		if c.registered.Supports(operation.capability) {
			continue
		}
		_, err := operation.invoke(c.ctx, c.registered)
		code, ok := provider.ErrorCodeOf(err)
		if !ok || code != provider.ErrorCodeCapabilityUnavailable {
			c.add("unsupported_"+string(operation.capability), fmt.Errorf("error code = %q, want %w", string(code), provider.ErrorCodeCapabilityUnavailable))
		} else {
			c.add("unsupported_"+string(operation.capability), nil)
		}
	}
	if c.registered.Supports(provider.CapabilityItem) && !c.registered.Supports(provider.CapabilityVariantSelection) {
		_, err := c.registered.Item(c.ctx, provider.ItemRequest{Variants: []provider.VariantSelection{{Key: "conformance", Value: "unsupported"}}})
		code, ok := provider.ErrorCodeOf(err)
		if !ok || code != provider.ErrorCodeCapabilityUnavailable {
			c.add("unsupported_"+string(provider.CapabilityVariantSelection), fmt.Errorf("error code = %q, want %w", string(code), provider.ErrorCodeCapabilityUnavailable))
		} else {
			c.add("unsupported_"+string(provider.CapabilityVariantSelection), nil)
		}
	}
}

func (c *suiteChecker) checkCases() {
	caseCount := make(map[provider.CapabilityName]int)
	for index, testCase := range c.suite.Cases {
		name := strings.TrimSpace(testCase.Name)
		if name == "" {
			name = fmt.Sprintf("case_%d", index+1)
		}
		checkName := "operation_" + string(testCase.Capability) + "_" + sanitizeName(name)
		if !c.registered.Supports(testCase.Capability) {
			c.add(checkName, fmt.Errorf("case uses undeclared capability %q", testCase.Capability))
			continue
		}
		caseCount[testCase.Capability]++
		if testCase.Invoke == nil {
			c.add(checkName, errors.New("invoke is required"))
			continue
		}
		value, err := testCase.Invoke(c.ctx, c.registered)
		if testCase.WantErrorCode != "" {
			code, ok := provider.ErrorCodeOf(err)
			if !ok || code != testCase.WantErrorCode {
				c.add(checkName, fmt.Errorf("error code = %q, want %w", string(code), testCase.WantErrorCode))
			} else {
				c.add(checkName, nil)
			}
			continue
		}
		if err != nil {
			c.add(checkName, fmt.Errorf("operation returned an error: %w", err))
			continue
		}
		if err := validateResult(testCase.Capability, value, c.help, testCase.WantPartialWarning); err != nil {
			c.add(checkName, err)
			continue
		}
		if testCase.Check != nil {
			if err := testCase.Check(value); err != nil {
				c.add(checkName, fmt.Errorf("case check: %w", err))
				continue
			}
		}
		c.add(checkName, nil)
	}
	for _, capability := range c.registered.Capabilities() {
		if caseCount[capability] == 0 {
			c.add("case_required_"+string(capability), fmt.Errorf("declared capability %q requires an operation case", capability))
		}
	}
}

func (c *suiteChecker) checkResourceUse() {
	if c.suite.Resources == nil {
		return
	}
	requests, remaining := c.suite.Resources.Stats()
	if requests == 0 {
		c.add("resource_service_used", errors.New("provider did not use the supplied ResourceService"))
	} else {
		c.add("resource_service_used", nil)
	}
	if remaining != 0 {
		c.add("resource_fixtures_consumed", fmt.Errorf("%d resource fixture(s) were not consumed", remaining))
	} else {
		c.add("resource_fixtures_consumed", nil)
	}
}

type operation struct {
	capability provider.CapabilityName
	invoke     func(context.Context, provider.Provider) (any, error)
}

func commonOperations() []operation {
	return []operation{
		{provider.CapabilitySearch, func(ctx context.Context, p provider.Provider) (any, error) {
			return p.Search(ctx, provider.SearchRequest{})
		}},
		{provider.CapabilityCategories, func(ctx context.Context, p provider.Provider) (any, error) {
			return p.Categories(ctx, provider.CategoryListRequest{})
		}},
		{provider.CapabilityCategorySearch, func(ctx context.Context, p provider.Provider) (any, error) {
			return p.SearchCategories(ctx, provider.CategorySearchRequest{})
		}},
		{provider.CapabilityCategoryItems, func(ctx context.Context, p provider.Provider) (any, error) {
			return p.CategoryItems(ctx, provider.CategoryItemsRequest{})
		}},
		{provider.CapabilityBrands, func(ctx context.Context, p provider.Provider) (any, error) {
			return p.Brands(ctx, provider.BrandListRequest{})
		}},
		{provider.CapabilityBrandSearch, func(ctx context.Context, p provider.Provider) (any, error) {
			return p.SearchBrands(ctx, provider.BrandSearchRequest{})
		}},
		{provider.CapabilityBrandItems, func(ctx context.Context, p provider.Provider) (any, error) {
			return p.BrandItems(ctx, provider.BrandItemsRequest{})
		}},
		{provider.CapabilityDeals, func(ctx context.Context, p provider.Provider) (any, error) {
			return p.Deals(ctx, provider.DealsRequest{})
		}},
		{provider.CapabilityFilters, func(ctx context.Context, p provider.Provider) (any, error) {
			return p.Filters(ctx, provider.FiltersRequest{})
		}},
		{provider.CapabilityItem, func(ctx context.Context, p provider.Provider) (any, error) {
			return p.Item(ctx, provider.ItemRequest{})
		}},
	}
}

func sanitizeName(name string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, name)
}
