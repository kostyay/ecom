---
id: eco-1c40
status: closed
deps:
- eco-7027
created: "2026-09-11T10:40:25Z"
type: task
priority: 2
assignee: Kostya Ostrovsky
parent: eco-ed12
tests_passed: true
---
# Implement BuscoCotxe Search and offline Help

Register buscocotxe with search support and implement Search through the Core resource service.

## Design

GET https://www.buscocotxe.ad/ca/search with encoded search and pn values. Support positive pages and size 30 only. Reuse the parser. Declare only implemented capabilities. Help must work offline. Follow docs/providers/buscocotxe-plan.md.

## Acceptance Criteria

The Provider accepts default configuration and rejects unknown settings, empty queries, invalid pages, unsupported sizes, filters, sorts, and shipping inclusion before requests. Cache, market, interactive policy, and context reach every request. EUR fallback has a warning. HTTP failures, unexpected redirects or pages, and cancellation return safe errors. Help and registration agree.

## Tests

Use FixtureService to check URL encoding, exact request values, policy propagation, cancellation, unsupported inputs without network access, HTTP errors, and mismatched page metadata. Run Help.Validate, SDK conformance, and go test ./providers/buscocotxe.

## Notes

**2026-09-11T10:40:25Z**

Triage: ready-for-agent

Plan: docs/providers/buscocotxe-plan.md
Implementation is planned and has not started.

**2026-09-11T10:46:21Z**

Carfinder confirms reusable listing structure and page-number parsing, but its runner and tasks browse /ca/filter?ordreFiltres[]=dataDesc&pn={page}. It does not implement the planned free-text search contract or prove combined text/filter behavior. Keep /ca/search, search, pn, Core-owned transport, request validation, and offline Help from this ticket. Do not import Carfinder HTTP clients, database, scheduler, or whole-catalog loops.

**2026-09-11T10:53:25Z**

Confirmed page behavior in eco-89da: /ca/search?search=BMW&pn=43 returns HTTP 200 at the requested URL, but the body reports page 42 of 42 and seven cars. Consume search_out_of_range.html in a FixtureService case and return a safe page-mismatch error. Do not repeat last-page items under the requested page number.

**2026-09-11T11:05:15Z**

Parser dependency completed. Call ParseListing(response.Body, response.RetrievedAt), using the existing pageSize and websiteHost constants. Nonempty results contain the actual response page number; compare it with the requested page and reject a mismatch. Explicit empty results have Number=0 and no TotalPages because the site omits those fields; do not treat this as an unexpected page mismatch. Parser errors use invalid_provider_result. Registration, HTTP resource requests, configuration validation, currency warnings, and offline Help remain to be implemented.

**2026-09-11T11:13:16Z**

Completed registration, offline Help, configuration validation, and Search in provider.go with provider_test.go. Search uses one Core-owned HTTP GET with RequestValue search/pn values, preserves request policies, validates market and inputs before fetch, checks final URL and actual page number, preserves cancellation and coded Core errors, and reports EUR currency fallback. Empty results use the requested page number with absent TotalPages to meet SDK pagination rules; Help documents this and does not promise total pages for every result. SDK conformance covers first/second/last/empty/partial/mismatched/unexpected pages. Checks passed: go test ./...; go test -race ./...; go vet ./...; repository golangci-lint (0 issues); formatting; fixture integrity; git diff --check. Simplification reviewers found no reuse, quality, or efficiency changes. CLI distribution import remains in eco-ac2a.
