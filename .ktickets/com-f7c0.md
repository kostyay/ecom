---
id: com-f7c0
status: closed
deps:
- com-8728
- com-8a77
- com-7acb
- com-3311
created: "2026-08-12T16:57:16Z"
type: task
priority: 1
assignee: kostyay
parent: com-7ed7
tests_passed: true
---
# Add provider help command

Implement ecom provider help with JSON and table output for capabilities, filters, sorts, page sizes, market limits, transport notes, and warnings.

## Design

Do not depend on live website access.

## Acceptance Criteria

Agents and people can inspect all provider nuances before other commands.

## Tests

Command tests use a fixture provider.
