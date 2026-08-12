---
id: com-72bf
status: closed
deps:
- com-7de3
- com-5d69
created: "2026-08-12T16:56:52Z"
type: task
priority: 1
assignee: kostyay
parent: com-4a93
tests_passed: true
---
# Add configured Chrome CDP transport

Connect to the configured CDP address as the third transport option and keep its profile state outside SQLite.

## Design

Do not copy or clear the user's Chrome profile.

## Acceptance Criteria

A provider request can use CDP after isolated browser failure; connection and target failures return stable errors.

## Tests

Tests use a fake CDP connector.
