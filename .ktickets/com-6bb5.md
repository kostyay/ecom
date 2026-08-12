---
id: com-6bb5
status: closed
deps:
- com-48c5
- com-8f51
- com-b6ef
- com-7810
- com-0aff
- com-d502
- com-f02c
created: "2026-08-12T16:57:43Z"
type: task
priority: 1
assignee: kostyay
parent: com-b67f
tests_passed: true
---
# Run Bike-Discount provider conformance suite

Connect all claimed capabilities and fixtures to the SDK conformance kit and correct contract violations.

## Design

Keep optional live checks in a separate test target.

## Acceptance Criteria

The provider passes every applicable offline conformance test and does not claim unimplemented behavior.

## Tests

Conformance, unit, and fixture integration tests pass offline.
