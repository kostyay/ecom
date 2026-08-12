---
id: com-8a77
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
# Define provider help and filter schemas

Add machine-readable help, filter definitions, sort modes, market restrictions, page-size rules, and transport notes.

## Design

Allow provider-specific text and fields in designated areas.

## Acceptance Criteria

Provider help contains all data required by agents and can be rendered without provider-specific type assertions.

## Tests

JSON golden tests cover complete and minimal help.
