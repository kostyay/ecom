# ADR 0002: Put Transport Policy in the Core

## Status

Accepted

## Decision

The Core owns direct HTTP, browser automation, CDP connections, session state, caching, request limits, retries, market application, logs, and diagnostics.

The transport sequence is direct HTTP, isolated browser automation when necessary, and a configured user Chrome session through CDP when the isolated browser is blocked. A manual challenge requires an explicit interactive command.

## Consequences

- All providers get the same load controls and cache behavior.
- Providers request resources through the Provider SDK.
- Providers cannot bypass shared transport policy with independent clients.
