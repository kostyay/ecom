---
id: com-48c5
status: closed
deps:
- com-2c48
- com-301d
- com-8a77
- com-ba29
created: "2026-08-12T16:56:07Z"
type: task
priority: 1
assignee: kostyay
parent: com-dee9
tests_passed: true
---
# Build provider conformance test kit

Create reusable tests for registration, declared capabilities, common fields, help, errors, pagination, filters, partial results, and resource-service use.

## Design

Let providers omit tests for capabilities they do not declare.

## Acceptance Criteria

Provider modules can run one conformance suite with fixtures and get clear failures for contract violations.

## Tests

Self-tests include a conforming and deliberately invalid provider.
