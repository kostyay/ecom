# ADR 0003: Use SQLite for Cache and Session State

## Status

Accepted

## Decision

Use SQLite in WAL mode for raw response cache entries and portable browser session state. Keep response entries and session state logically separate.

The default response TTL is 24 hours. The default database limit is 512 MB, and the default single-response limit is 20 MB. Pruning removes expired response entries first and then least-recently-used entries. It does not remove session state.

## Consequences

- Concurrent CLI processes can share cached responses.
- Schema changes require automatic versioned migrations.
- Full Chrome profiles are not stored in SQLite.
- Cache removal and browser-session removal are separate operations.
