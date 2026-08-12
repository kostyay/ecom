---
id: com-301d
status: closed
deps:
- com-3635
- com-ea5c
created: "2026-08-12T16:56:07Z"
type: task
priority: 1
assignee: kostyay
parent: com-dee9
tests_passed: true
---
# Define provider operations and capabilities

Define provider interfaces and capability declarations for help, search, categories, brands, deals, filters, paging, and item details.

## Design

Keep site request formats provider-specific.

## Acceptance Criteria

A provider declares only supported operations; unsupported calls map to capability_unavailable; operation inputs use common types.

## Tests

Compile-time fixtures exercise full and minimal providers.
