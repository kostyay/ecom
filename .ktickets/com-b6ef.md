---
id: com-b6ef
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
# Implement Bike-Discount category navigation

Implement top-level, child, recursive, and category item operations with stable opaque category IDs.

## Design

Avoid duplicate categories during recursive traversal.

## Acceptance Criteria

Category paths and parents are correct; item pages reuse common summary parsing; local text fallback can use the returned tree.

## Tests

Fixture tests cover tree levels, paging, and malformed nodes.
