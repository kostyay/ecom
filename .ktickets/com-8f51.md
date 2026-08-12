---
id: com-8f51
status: closed
deps:
- com-ae9c
- com-6624
created: "2026-08-12T16:57:43Z"
type: task
priority: 1
assignee: kostyay
parent: com-b67f
tests_passed: true
---
# Implement Bike-Discount search filters and paging

Implement native product search, filters, sort values, page numbers, and supported page sizes.

## Design

Expose provider-specific filter translations only inside the provider.

## Acceptance Criteria

Queries reach verified site requests; invalid filters and sizes fail before network access; result paging metadata is correct.

## Tests

Fixture request and response tests cover each input type.
