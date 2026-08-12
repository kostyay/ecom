---
id: com-f661
status: closed
deps:
- com-8728
- com-1571
- com-7acb
- com-3311
created: "2026-08-12T16:57:16Z"
type: task
priority: 1
assignee: kostyay
parent: com-7ed7
tests_passed: true
---
# Add cache and session maintenance commands

Implement cache clear, cache prune, provider-scoped clear, and provider session clear.

## Design

Require explicit provider where scope would otherwise be unclear.

## Acceptance Criteria

Commands call maintenance services, report affected entries, and keep response and session removal separate.

## Tests

Command tests verify scope and output.
