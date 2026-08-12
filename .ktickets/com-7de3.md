---
id: com-7de3
status: closed
deps:
- com-ba29
- com-5d69
created: "2026-08-12T16:56:53Z"
type: task
priority: 1
assignee: kostyay
parent: com-4a93
tests_passed: true
---
# Implement browser automation abstraction

Add a core browser interface and one Playwright-compatible or CDP-based implementation for navigation, page content, storage state, and challenge detection.

## Design

Keep provider DOM extraction possible through explicit core operations.

## Acceptance Criteria

The core can open isolated browser pages and return raw page content with stable access or challenge errors.

## Tests

Tests use a fake browser; implementation smoke test is optional.
