---
id: eco-89da
status: closed
created: "2026-09-11T10:40:25Z"
type: task
priority: 2
assignee: Kostya Ostrovsky
parent: eco-ed12
tests_passed: true
---
# Record BuscoCotxe search fixtures and page rules

Save small sanitized fixtures for normal, second, last, and empty search pages. Record source URLs, capture time, and sanitization. Confirm behavior beyond the last page.

## Design

Public /ca/search uses search and pn. Research found 30 cards per full page. Keep listing cards and relevant page metadata; remove account forms, contact data, analytics, and unrelated page content. Include price-on-request and sold cards.

## Acceptance Criteria

Fixtures and a manifest are present under providers/buscocotxe/testdata. They cover page 1, page 2, a short last page, an explicit empty result, missing price, and sold state. The plan records out-of-range page behavior and any query or redirect limits.

## Tests

Inspect fixture IDs, card counts, page metadata, and absence of unnecessary private data. Parser and conformance tasks must consume these fixtures.

## Notes

**2026-09-11T10:40:25Z**

Triage: ready-for-agent

Plan: docs/providers/buscocotxe-plan.md
Implementation is planned and has not started.

**2026-09-11T10:46:21Z**

Updated fixture approach: start with /data/personal/carfinder/tests/fixtures/index_page_1.html and index_page_2.html. These are filter-page fixtures from the older project. Reuse sanitized relevant parts; keep separate search-page fixtures, including the first, second, last, and empty responses already captured for this plan. Record source paths and commit 4e39366; do not invent historical capture timestamps. Existing tests/test_index_parser.py provides field expectations. Add cases absent from the old suite: description price leakage, decimal comma, explicit empty versus unexpected HTML, bad ID, and partial results.

**2026-09-11T10:53:25Z**

Completed: added nine sanitized HTML fixtures, manifest.json, README.md, and the standard-library check.py under providers/buscocotxe/testdata. Reused both Carfinder index fixtures; retained four earlier search captures; added one read-only HTTP capture for pn=43; added labeled synthetic regression and unexpected-page fixtures. Original Carfinder capture times remain unknown. The pn=43 response was HTTP 200 with no URL redirect and returned page 42 with seven cars. Updated the plan to require a page-mismatch error. Validation passed: python3 providers/buscocotxe/testdata/check.py (9 fixtures); source-to-fixture comparison of URLs, headings, summary fields, price overlays, and image styles; replay of all seven source-derived fixtures through the existing Carfinder parser; git diff --check. Carfinder is unchanged. The Go parser and conformance tasks must consume these fixtures.
