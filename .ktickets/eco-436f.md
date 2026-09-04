---
id: eco-436f
status: closed
created: "2026-09-04T13:39:54Z"
type: chore
priority: 3
parent: eco-d9a2
tests_passed: true
---
# Finish the Go 1.27 idiom updates

Replace remaining older Go forms in files in the current modernization scope with the applicable Go 1.27 standard-library forms.

## Design

Keep the change mechanical. Do not rewrite loops or parsers when the modern helper does not make the code smaller and equally clear.

## Acceptance Criteria

Use errors.AsType in the shared coded-error lookup. Use slices.Sort or slices.SortFunc instead of applicable sort helpers. Use t.Context in tests that need a test-lifetime context. Replace remaining interface{} spellings with any. Use Cut helpers where they directly replace manual prefix or index-and-slice code.

## Tests

Run focused package tests and make quality.

## Notes

**2026-09-04T13:40:25Z**

Triage: ready-for-agent

**2026-09-04T13:40:25Z**

Correction: In the acceptance criteria, read “replace manual prefix or index-and-slice code.”

**2026-09-04T13:49:53Z**

Applied Go 1.27 forms: errors.AsType, slices sorting, t.Context in tests, any, and Cut helpers for direct prefix/index slicing. Focused package tests and make quality pass.
