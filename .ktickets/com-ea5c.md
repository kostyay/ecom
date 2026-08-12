---
id: com-ea5c
status: closed
deps:
- com-3635
created: "2026-08-12T16:56:07Z"
type: task
priority: 1
assignee: kostyay
parent: com-dee9
tests_passed: true
---
# Define provider errors and warnings

Add stable coded error and warning types for unsupported capabilities, invalid filters, missing variants, partial parsing, currency fallback, access blocks, and challenges.

## Design

Preserve causes and safe user messages.

## Acceptance Criteria

Providers and core services can create and inspect stable codes without string matching.

## Tests

Unit tests cover wrapping, code extraction, and JSON encoding.
