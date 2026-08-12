---
id: com-780f
status: closed
deps:
- com-8728
- com-301d
- com-7acb
- com-3311
created: "2026-08-12T16:57:16Z"
type: task
priority: 1
assignee: kostyay
parent: com-7ed7
tests_passed: true
---
# Add search command with filters and paging

Implement product search with repeatable filters, sort, page, provider-supported page size, refresh, and stale-if-error.

## Design

Reject --all and unsupported filter keys clearly.

## Acceptance Criteria

Search returns products only; provider validates filter and page details; all core flags reach the request pipeline.

## Tests

Command tests cover arguments, propagation, output, and errors.
