---
id: com-7ed7
status: closed
created: "2026-08-12T16:55:34Z"
type: epic
priority: 1
assignee: kostyay
tests_passed: true
---
# Provider-neutral CLI and output

Add provider selection, discovery commands, stable JSON, tables, kubectl-style JSONPath, and cache or session maintenance commands.

## Design

Follow ADR 0004 and docs/architecture.md sections 6 and 7.

## Acceptance Criteria

All planned commands exist; stable output and errors work; provider help reports nuances; paging, filters, variants, refresh, and stale flags reach providers and core services.

## Tests

Command tests validate stdout, stderr, exit status, and all output modes.
