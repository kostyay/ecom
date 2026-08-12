---
id: com-061b
status: closed
deps:
- com-4d03
- com-4414
created: "2026-08-12T16:56:31Z"
type: task
priority: 1
assignee: kostyay
parent: com-6949
tests_passed: true
---
# Store raw response cache entries

Add the SQLite repository for successful raw responses, metadata, timestamps, access time, encoding, and size.

## Design

Do not store normalized command results.

## Acceptance Criteria

Entries round-trip without data loss and can be selected by key or provider; invalid and oversized entries are rejected.

## Tests

Repository tests use a temporary database.
