---
id: com-7acb
status: closed
deps:
- com-941a
created: "2026-08-12T16:57:17Z"
type: task
priority: 1
assignee: kostyay
parent: com-7ed7
tests_passed: true
---
# Implement human-readable table output

Add -o table renderers for provider help, product lists, category lists, brand lists, deals, and item details.

## Design

Truncate only presentation text, never source data.

## Acceptance Criteria

Tables are readable, stable enough for people, and do not change JSON contracts.

## Tests

Golden tests cover narrow and wide values.
