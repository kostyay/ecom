---
id: eco-4fc7
status: closed
created: "2026-10-03T14:38:05Z"
type: task
priority: 2
assignee: Kostya Ostrovsky
parent: eco-2616
tests_passed: true
---
# Implement manufacturer search Providers with fixtures

Research YT Industries, Propain, and Canyon public product sources; implement supported search requests, parsers, help, registration, and documentation.

## Design

Use Core-owned transport only. Declare only verified Capabilities. Canyon targets ES/en/EUR.

## Acceptance Criteria

All three Providers return validated Product summaries and honest pagination, reject unsupported requests before transport, and retain decimal Displayed prices.

## Tests

Offline fixture, conformance, malformed/empty/partial/pagination tests; make quality; live smoke tests separately.

## Notes

**2026-10-03T14:38:24Z**

Triage: ready-for-agent. Public sources identified: YT Shopware Store API, Propain WooCommerce Store API, Canyon English/Spain search HTML. Validating pricing and pagination before implementation.

**2026-10-03T15:09:05Z**

Implemented search-only Providers: yt-industries (DE/en, Shopware JSON), propain (DE/en, storefront HTML), canyon (ES/en, bikes-tab HTML). Preserved starting prices, original prices, stock labels, YT product-selecting number URLs, and explicit pagination. Sanitized fixtures and offline parser/conformance/CLI/cache tests added. make quality passed: format, lint (0 issues), vet, full tests, fixtures, docs, race, build. Live CLI HTTP smoke tests returned 24 YT, 10 Propain, and 24 Canyon products without warnings. Inline simplification review completed (subagents unavailable): one reuse cleanup applied (shared Propain page-path formatter); zero quality/efficiency changes, no safety checks removed.
