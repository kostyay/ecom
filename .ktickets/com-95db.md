---
id: com-95db
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
# Add item command and variant selection

Implement item lookup by provider ID or owned URL and repeatable variant selection.

## Design

Do not imply one price applies to all variants.

## Acceptance Criteria

Provider URLs are validated; default output includes visible variants; exact selection works; invalid variants list valid choices.

## Tests

Command tests cover IDs, URLs, ranges, exact variants, and errors.
