---
id: eco-c5e2
status: closed
created: "2026-09-04T13:39:54Z"
type: chore
priority: 3
parent: eco-d9a2
tests_passed: true
---
# Complete the JSON omitzero conversion

Apply the Go 1.24 json_omitzero guideline to JSON-tagged Boolean, numeric, struct, duration, and time fields where the zero value means that the field is absent.

## Design

Make a mechanical tag-only change. Do not migrate existing JSON codecs or change which fields are required.

## Acceptance Criteria

Applicable fields use omitzero. Strings, slices, maps, and pointers keep the correct omission behavior. Existing golden JSON output does not change.

## Tests

Run JSON golden tests, go test ./provider ./internal/output, and make quality.

## Notes

**2026-09-04T13:40:25Z**

Triage: ready-for-agent

**2026-09-04T13:51:03Z**

Changed optional Boolean, numeric, and duration JSON tags from omitempty to omitzero. Kept strings, slices, maps, pointers, required fields, and existing codecs unchanged. go test ./provider ./internal/output and make quality pass.
