---
id: com-e5b7
status: closed
deps:
- com-9307
- com-780f
- com-b7e2
- com-9fa2
- com-95db
- com-f661
created: "2026-08-12T16:58:04Z"
type: task
priority: 2
assignee: kostyay
parent: com-7d02
tests_passed: true
---
# Add end-to-end CLI workflow tests

Test provider help, search, category, brand, deal, item, cache hit, refresh, stale fallback, JSONPath, and table flows through the built application.

## Design

Use the fixture provider and temporary config or databases.

## Acceptance Criteria

Tests verify stdout, stderr, exit codes, persistence, and request counts across separate CLI runs.

## Tests

End-to-end tests run offline and without a browser.
