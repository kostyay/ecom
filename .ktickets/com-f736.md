---
id: com-f736
status: closed
deps:
- com-8418
- com-f2db
created: "2026-08-12T16:56:53Z"
type: task
priority: 1
assignee: kostyay
parent: com-4a93
tests_passed: true
---
# Implement retry and Retry-After policy

Retry 429, 502, 503, and 504 up to the configured count with exponential delay and random variation, and honor Retry-After.

## Design

Make delay and randomness injectable for tests.

## Acceptance Criteria

Only eligible requests and responses retry; cancellation stops delays; final errors preserve response context.

## Tests

Tests cover status codes, headers, limits, and cancellation.
