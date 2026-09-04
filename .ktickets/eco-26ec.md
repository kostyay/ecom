---
id: eco-26ec
status: closed
created: "2026-09-04T13:39:37Z"
type: task
priority: 1
parent: eco-d9a2
tests_passed: true
---
# Use JSON v2 in the Wallapop provider

Use encoding/json/v2 for the new Wallapop codec code. Keep encoding/json aliases only for Number and RawMessage compatibility with the Provider SDK.

## Design

Do not migrate existing Core or Bike-Discount JSON codecs. The Modern Go Guidelines require v2 only for new JSON code.

## Acceptance Criteria

Wallapop Marshal and Unmarshal calls use encoding/json/v2. Wire fields that need lower-case or camel-case names have explicit JSON tags. Boolean and numeric omission tags use omitzero where zero means absent. Provider data remains valid JSON.

## Tests

Update Wallapop fixture tests to prove lower-case and camel-case fields decode with v2. Run go test ./providers/wallapop and make quality.

## Notes

**2026-09-04T13:40:24Z**

Triage: ready-for-agent

**2026-09-04T13:45:07Z**

Implemented Wallapop encoding/json/v2 Marshal and Unmarshal use with encoding/json aliases only for RawMessage and Number. Added explicit wire tags. go test ./providers/wallapop and make quality pass.
