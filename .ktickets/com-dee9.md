---
id: com-dee9
status: closed
created: "2026-08-12T16:55:34Z"
type: epic
priority: 1
assignee: kostyay
tests_passed: true
---
# Provider SDK and domain contracts

Build the public, versioned Go Provider SDK, registry, common data model, capability model, and provider conformance foundation.

## Design

Follow ADR 0001 and docs/architecture.md sections 3 through 5.

## Acceptance Criteria

A provider module can register through init; incompatible and duplicate registrations fail clearly; common provider operations and data types compile; offline SDK tests pass.

## Tests

Unit tests cover registry validation, types, capabilities, and errors.
