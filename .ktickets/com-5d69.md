---
id: com-5d69
status: closed
created: "2026-08-12T16:56:30Z"
type: task
priority: 1
assignee: kostyay
parent: com-6949
tests_passed: true
---
# Extend configuration model and defaults

Add provider, market, cache, network, browser, and per-provider settings with the approved defaults and precedence.

## Design

Unknown core keys fail; provider blocks remain available for provider validation.

## Acceptance Criteria

YAML, environment variables, and flags load exact documented values; defaults are DE, en, EUR, 24h, 512 MiB, 20 MiB, and approved network limits.

## Tests

Unit tests cover each source and precedence.
