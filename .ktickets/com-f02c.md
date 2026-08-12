---
id: com-f02c
status: closed
deps:
- com-ae9c
- com-a8b2
created: "2026-08-12T16:57:43Z"
type: task
priority: 1
assignee: kostyay
parent: com-b67f
tests_passed: true
---
# Integrate Bike-Discount market and transport needs

Apply configured country, language, and currency to verified requests and declare HTTP, browser, CDP, or challenge needs correctly.

## Design

Do not solve CAPTCHAs or rotate proxies.

## Acceptance Criteria

Market-specific requests and cache separation work; actual-currency warnings appear; normal blocks select approved fallback without provider-owned clients.

## Tests

Fake core-service tests cover HTTP, browser fallback, CDP, and challenge errors.
