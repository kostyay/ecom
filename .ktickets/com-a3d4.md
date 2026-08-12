---
id: com-a3d4
status: closed
deps:
- com-e5b7
- com-6bb5
created: "2026-08-12T16:58:04Z"
type: task
priority: 2
assignee: kostyay
parent: com-7d02
tests_passed: true
---
# Add repository quality and race checks

Update Makefile or CI-ready commands for formatting, lint, unit tests, race-sensitive tests where supported, fixture tests, and build.

## Design

Do not require live websites for the standard suite.

## Acceptance Criteria

One documented command sequence checks the full repository; generated or fixture files are handled consistently.

## Tests

make lint, make test, and make build pass.
