---
id: com-f2db
status: closed
deps:
- com-5d69
created: "2026-08-12T16:56:52Z"
type: task
priority: 1
assignee: kostyay
parent: com-4a93
tests_passed: true
---
# Implement per-provider request limits

Add one-per-second default rate control and separate HTTP and browser concurrency limits for each provider.

## Design

Use a fake clock or controlled scheduler in tests.

## Acceptance Criteria

Limits are provider-scoped, configurable, context-aware, and do not apply to cache hits.

## Tests

Deterministic tests cover rate and concurrency behavior.
