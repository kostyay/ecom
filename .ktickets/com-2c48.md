---
id: com-2c48
status: closed
created: "2026-08-12T16:56:07Z"
type: task
priority: 1
assignee: kostyay
parent: com-dee9
tests_passed: true
---
# Implement versioned provider registry

Add the SDK API version, provider registration, lookup, duplicate detection, and compatibility checks.

## Design

Do not import internal CLI packages.

## Acceptance Criteria

Blank-import providers can register with init; duplicate names and incompatible versions fail clearly; registry access is safe for concurrent reads.

## Tests

Unit tests include concurrent lookup and invalid registration cases.
