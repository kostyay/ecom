---
id: com-b7e2
status: closed
deps:
- com-8728
- com-301d
- com-7acb
- com-3311
created: "2026-08-12T16:57:16Z"
type: task
priority: 1
assignee: kostyay
parent: com-7ed7
tests_passed: true
---
# Add category commands and local text fallback

Implement top-level, parent, recursive, and text category listing plus paged category item listing.

## Design

Local fallback can populate the tree through normal rate and cache policy.

## Acceptance Criteria

Category results form a tree with opaque IDs; text search uses provider search or cached case-insensitive local fallback and reports the method.

## Tests

Command tests cover both search methods and listing pages.
