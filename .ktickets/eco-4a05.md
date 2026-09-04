---
id: eco-4a05
status: closed
created: "2026-09-04T13:39:54Z"
type: bug
priority: 1
parent: eco-d9a2
tests_passed: true
---
# Make Wallapop item-page parsing robust

Parse the Wallapop NEXT_DATA script without dependence on HTML attribute order or unrelated text that contains the same marker.

## Design

Use the already installed golang.org/x/net/html parser. Do not add a dependency or a general HTML abstraction.

## Acceptance Criteria

Item detail parsing finds a script element by id __NEXT_DATA__. Attribute order does not affect parsing. Missing or invalid script data returns the existing safe provider error.

## Tests

Add focused cases for reversed script attributes, a missing script, and invalid JSON. Run go test ./providers/wallapop.

## Notes

**2026-09-04T13:40:24Z**

Triage: ready-for-agent

**2026-09-04T13:46:37Z**

Replaced byte-marker slicing with golang.org/x/net/html parsing and exact script id matching. Added reversed-attribute, unrelated-text, missing-script, and invalid-JSON cases. go test ./providers/wallapop passes.
