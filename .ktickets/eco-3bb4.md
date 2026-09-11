---
id: eco-3bb4
status: closed
created: "2026-09-04T19:34:44Z"
type: task
priority: 2
assignee: Kostya Ostrovsky
parent: eco-1882
tests_passed: true
---
# Implement Bike24 product search

Register a minimal Bike24 Provider that searches the public English storefront through Core transport.

## Design

Use the current /search page with searchTerm and page parameters. Parse provider-owned product links from rendered HTML.

## Acceptance Criteria

Bike24 help is available; search returns valid product summaries; unsupported operations stay undeclared.

## Tests

Run provider conformance with one saved search fixture and run the full Go test suite.

## Notes

**2026-09-04T19:40:02Z**

Implemented compiled Bike24 search with current /search?searchTerm= URL, page-number pagination, Core transport fallback, rendered-HTML parsing, and offline conformance coverage.
