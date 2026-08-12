// Package conformance supplies an offline contract test suite for ecom providers.
//
// A provider module normally creates a FixtureService, injects it into its
// implementation, and passes the registration and operation cases to Run.
// The suite does not import or start any ecom Core service.
package conformance
