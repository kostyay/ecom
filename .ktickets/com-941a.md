---
id: com-941a
status: closed
deps:
- com-3635
- com-ea5c
created: "2026-08-12T16:57:16Z"
type: task
priority: 1
assignee: kostyay
parent: com-7ed7
tests_passed: true
---
# Implement stable JSON envelope

Add schema version, provider, market, data, page, cache, and warning fields and make JSON the default for data commands.

## Design

Diagnostics remain on standard error.

## Acceptance Criteria

Successful data commands emit one valid envelope; decimal price strings and null or absent fields retain their contract.

## Tests

Golden tests cover listing, item, partial, and cached results.
