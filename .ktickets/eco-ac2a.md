---
id: eco-ac2a
status: closed
deps:
- eco-1c40
created: "2026-09-11T10:40:25Z"
type: task
priority: 2
assignee: Kostya Ostrovsky
parent: eco-6868
tests_passed: true
---
# Add BuscoCotxe to the CLI and user guide

Add the distribution import for buscocotxe and document car search commands and limits.

## Design

Read main.go again before editing and preserve all existing imports. Update README.md, docs/user-guide.md, and the compiled-provider description in docs/architecture.md where needed. Use the existing output system.

## Acceptance Criteria

The built CLI lists and selects buscocotxe. Help is available offline. Examples show text search and page size 30. Docs state that filters, custom sorts, item details, and other capabilities are unsupported, and that results can include sold cars or cars without a price.

## Tests

Use existing CLI test patterns with fixtures to check provider selection, help, JSON, table, JSONPath, page arguments, and structured unsupported-input errors. Run relevant CLI and documentation tests.

## Notes

**2026-09-11T10:40:25Z**

Triage: ready-for-agent

Plan: docs/providers/buscocotxe-plan.md
Implementation is planned and has not started.

**2026-09-11T11:13:16Z**

Search dependency is complete. Add the blank import for github.com/kostyay/ecom/providers/buscocotxe to the current main.go, preserving the existing Bike24 work. Provider name is buscocotxe. Help works offline. Defaults are page 1 / size 30; only HTTP search is supported. Catalog is fixed to Andorra/Catalan; prices remain EUR with warnings for other currencies. Search normalizes explicit empty results to the requested page number and omits TotalPages. Full repo tests, race, vet, and lint passed before this integration.

**2026-09-11T11:26:08Z**

Added the BuscoCotxe distribution import without changing the existing Bike24 import. Updated README, user guide, architecture, and the Provider plan. Added a fixture-backed real CLI test for offline help, provider selection, JSON, table, JSONPath, page 1 and page 2, and invalid_filter before resource access. The integration test found that Core cache responses remove final URL query parameters; Search now accepts the safe base search URL and still validates query parameters when present. Simplification review applied one reuse and one quality improvement; it found no efficiency issue. make lint vet test passed, and git diff --check passed.
