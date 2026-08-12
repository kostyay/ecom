---
id: com-ba29
status: closed
deps:
- com-3635
- com-ea5c
created: "2026-08-12T16:56:07Z"
type: task
priority: 1
assignee: kostyay
parent: com-dee9
tests_passed: true
---
# Define core resource service contract

Define the narrow SDK interface through which providers request HTTP or browser resources and receive raw responses plus cache metadata.

## Design

Do not expose SQLite or browser implementation types.

## Acceptance Criteria

Providers can state resource and transport needs but cannot construct core clients; context cancellation is part of every call.

## Tests

Fake-service tests show that providers can be tested offline.
