---
id: eco-ed12
status: closed
created: "2026-09-11T10:40:25Z"
type: epic
priority: 2
assignee: Kostya Ostrovsky
tests_passed: true
---
# BuscoCotxe car search Provider

Add the buscocotxe Provider for public car text search. Return Product summaries with page-number pagination.

## Design

Follow docs/providers/buscocotxe-plan.md. Use the public SDK, Core-owned HTTP, and the existing HTML parser dependency. Declare only search. Filters and item details are outside the first version.

## Acceptance Criteria

All child tasks pass. Search returns valid car IDs, URLs, names, displayed EUR prices when present, and reliable page metadata. Offline Help states all limits.

## Tests

Run parser fixtures, request-policy checks, and SDK conformance without live network access.

## Notes

**2026-09-11T10:40:25Z**

Triage: ready-for-agent

Plan: docs/providers/buscocotxe-plan.md
Implementation is planned and has not started.

**2026-09-11T10:46:21Z**

Carfinder reuse review (2026-09-11): found the source at /data/personal/carfinder (~/personal/carfinder), not ~/work/carfinder. Reuse the index parser rules and existing fixtures through Go and the Core SDK. All 15 existing index/detail parser tests passed in an isolated environment. The old parser also read the four previously saved search responses. See the new Carfinder reuse section in docs/providers/buscocotxe-plan.md. Scope remains search only; no implementation has started.

**2026-09-11T10:53:25Z**

Fixture task eco-89da is complete. Nine fixtures and an offline integrity check are available. The plan now records the confirmed last-page behavior. Provider code has not started; eco-7027 is the next task.

**2026-09-11T11:05:15Z**

Parser task eco-7027 is complete with passing fixture, race, vet, lint, and formatting checks. The next task is eco-1c40 for Search, registration, and offline Help. The Provider is not yet selectable in the CLI.

**2026-09-11T11:13:16Z**

All three child tasks are complete: sanitized fixtures, Go parser, and registered Search with offline Help. Full repository tests, race tests, vet, lint, and offline SDK conformance pass. The Provider implementation is complete. CLI distribution import, user documentation, and separate live release checks remain in epic eco-6868.
