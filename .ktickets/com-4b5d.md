---
id: com-4b5d
status: closed
deps:
- com-f736
- com-6294
- com-4414
created: "2026-08-12T16:56:53Z"
type: task
priority: 1
assignee: kostyay
parent: com-4a93
tests_passed: true
---
# Compose cache-aware HTTP fetch pipeline

Combine cache lookup, rate limit, retries, raw-response storage, refresh, stale-if-error, and cache metadata.

## Design

Keep parsing outside the pipeline.

## Acceptance Criteria

Valid hits avoid the network; successful fresh responses enter cache; failures follow the approved stale rules.

## Tests

Integration tests use fake clocks, httptest, and temporary SQLite.
