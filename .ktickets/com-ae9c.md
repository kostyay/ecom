---
id: com-ae9c
status: closed
deps:
- com-ce67
- com-2c48
- com-301d
- com-8a77
created: "2026-08-12T16:57:43Z"
type: task
priority: 1
assignee: kostyay
parent: com-b67f
tests_passed: true
---
# Scaffold Bike-Discount provider module

Create the provider package or module, blank-import registration, configuration validation, capabilities, and machine-readable help.

## Design

Keep the module dependent only on the public SDK where practical.

## Acceptance Criteria

The provider registers with the SDK and accurately reports implemented or planned capabilities and nuances.

## Tests

Registration and help fixture tests pass.
