---
id: com-4d03
status: closed
created: "2026-08-12T16:56:30Z"
type: task
priority: 1
assignee: kostyay
parent: com-6949
tests_passed: true
---
# Add SQLite connection and migrations

Select a maintained pure-Go or supported SQLite driver and add database opening, WAL, busy timeout, schema versioning, and automatic migrations.

## Design

Keep schema changes transactional.

## Acceptance Criteria

A new and an old temporary database open at the current schema; concurrent readers and a writer work without immediate lock failure.

## Tests

Migration and concurrency tests pass.
