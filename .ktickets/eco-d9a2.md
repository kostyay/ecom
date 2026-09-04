---
id: eco-d9a2
status: closed
created: "2026-09-04T13:39:26Z"
type: epic
priority: 1
tests_passed: true
---
# Apply Go 1.27 review fixes

Apply the fixes found during the Go 1.27 convention review of the current Wallapop and modernization changes.

## Design

Keep fixes independent and use the smallest standard-library solution that preserves the stable output contract.

## Acceptance Criteria

All child tickets are closed, and make quality passes.

## Tests

Run make quality after all child tickets are complete.

## Notes

**2026-09-04T13:39:37Z**

Triage: ready-for-agent

**2026-09-04T13:51:20Z**

All five child tickets are closed. Final make quality passes, including formatting, lint, vet, tests, fixtures, documentation tests, race tests, and build.
