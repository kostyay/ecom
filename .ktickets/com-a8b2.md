---
id: com-a8b2
status: closed
deps:
- com-4b5d
- com-8479
- com-72bf
- com-c8c6
created: "2026-08-12T16:56:53Z"
type: task
priority: 1
assignee: kostyay
parent: com-4a93
tests_passed: true
---
# Implement ordered transport selection

Select direct HTTP, isolated browser, and configured CDP in the approved order based on provider needs and classified failures.

## Design

Return attempt metadata without leaking secrets.

## Acceptance Criteria

Fallback occurs only for eligible conditions; all attempts share limits, context, market, and diagnostics.

## Tests

Tests cover each path and terminal error.
