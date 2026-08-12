---
id: com-0aff
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
# Implement Bike-Discount deal discovery

Implement verified native sale pages or discount filters and expose discount sort or minimum filters when supported.

## Design

Explain the deal source in provider help.

## Acceptance Criteria

Every returned deal has a provider-shown reduction; no deal score is estimated; paging and filters work.

## Tests

Fixture tests cover discount fields, sorting, filters, and no-result pages.
