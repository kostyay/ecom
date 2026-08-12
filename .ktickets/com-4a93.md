---
id: com-4a93
status: closed
created: "2026-08-12T16:55:34Z"
type: epic
priority: 1
assignee: kostyay
tests_passed: true
---
# Core transport services

Implement core-owned HTTP, browser, CDP, rate-limit, retry, and transport-selection services.

## Design

Follow ADR 0002 and docs/architecture.md sections 9 and 10.

## Acceptance Criteria

Providers can request resources only through core services; direct HTTP and browser fallbacks follow policy; limits, retries, challenges, and cancellation work.

## Tests

Tests use fake HTTP and browser services; live access is not required.
