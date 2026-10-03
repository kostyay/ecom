---
id: eco-ab95
status: closed
created: "2026-10-03T19:33:07Z"
type: task
priority: 2
assignee: Kostya Ostrovsky
parent: eco-2616
tests_passed: true
---
# Simplify manufacturer Provider changes without changing behavior

Run inline reuse, code-quality, and efficiency reviews over the uncommitted YT Industries, Propain, Canyon, shared helpers, and associated Go tests. Exclude existing Providers, fixtures, documentation, and unrelated mobile-de research.

## Design

Apply 1 reuse finding, 2 quality findings, and 2 efficiency findings. Skip generic parser/help unification, pagination-helper extraction, and request map-to-struct serialization changes.

## Acceptance Criteria

Preserve complete fixture outputs, request ordering and bytes, safe errors, price semantics, market validation, URL guards, and unsupported-operation behavior.

## Tests

Before/after full fixture output/request comparison; targeted edge-case regressions; make quality.

## Notes

**2026-10-03T19:33:19Z**

Triage: ready-for-agent. All three review passes completed inline; no subagent primitive is available. Reuse: replace HasPrefix+TrimPrefix with CutPrefix in Propain ID parsing. Quality: flatten YT canonical-URL selection and replace ambiguous positional fixture tables with keyed fields. Efficiency: stream HTML class tokens with FieldsSeq (as used by BuscoCotxe) and parse Canyon next-link query once. Skipped 3 low-value/risky proposals: generic parser/help framework, tiny shared page-count helper, and YT map-to-struct request encoding (could change body/cache identity).

**2026-10-03T20:07:46Z**

User approved all test-audit fixes. Before/after comparison of 36 fixture scenarios (all three Providers, normal/later/last/empty/malformed/wrong-page, EUR/USD) produced byte-identical result/error/request captures. Temporary assertion-free snapshot harness removed after comparison. Consolidating shared-owner checks and replacing implementation-derived assertions with behavior tests.

**2026-10-03T20:22:44Z**

Applied all approved test-audit fixes: removed assertion-free snapshot harness, implementation-equivalence class test, and incidental Propain ID-class test; replaced YT self-derived URL and Euro display expectations with independent literals; consolidated conformance into one suite per Provider; moved generic validation/configuration/transport failures to shoputil owner tests; removed duplicate parser/capability checks; replaced resource-field query copying and CLI JSON greps with serialized HTTP boundary checks and decoded JSON assertions. Cancellation spies record canceled fetch attempts. CLI tests now verify actual SQLite cache replay, refresh bypass, later-page requests, and special-character query isolation. Preserved YT sensitive-key/channel-partition policy coverage and site-specific price, stock, URL, and partial-result tests. make quality passed (0 lint issues; vet, full offline/fixture/doc/race tests, build). Class matching benchmark dropped from 80 B / 1 allocation to 0 B / 0 allocations. Original simplification applied counts: reuse 1, quality 2, efficiency 2; skipped 3 broader/risky refactors. No commits or pushes.
