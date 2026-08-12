---
id: com-a681
status: closed
deps:
- com-4d03
- com-5d69
created: "2026-08-12T16:56:30Z"
type: task
priority: 1
assignee: kostyay
parent: com-6949
tests_passed: true
---
# Store portable browser session state

Add separate SQLite storage for provider and market scoped cookies and local storage JSON.

## Design

Do not store full Chrome profiles, passwords, or CDP profile data.

## Acceptance Criteria

Session state round-trips, updates atomically, and remains after response-cache clearing or pruning.

## Tests

Repository tests cover provider and market isolation.
