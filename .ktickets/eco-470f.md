---
id: eco-470f
status: closed
created: "2026-09-04T22:42:55Z"
type: task
priority: 2
assignee: Kostya Ostrovsky
parent: eco-1882
tests_passed: true
---
# Audit and harden Bike24 provider

Run critic and QA reviews against all declared Bike24 Provider capabilities, then fix confirmed defects.

## Acceptance Criteria

The Bike24 Provider passes adversarial offline tests, CLI checks, full repository quality checks, and a documented live-access probe.

## Tests

Run targeted tests, fuzz or parser edge checks, make quality, and a live CLI search probe.

## Notes

**2026-09-04T23:02:07Z**

Critic and QA reviews found and fixed product-card price leakage, one-digit and RRP price parsing, empty-page handling, paging totals, market validation, error mapping, malformed duplicate links, invalid images and money, and fixture drift. Current proxy-rendered Bike24 page parsed 30 products with distinct prices; direct CLI access remained blocked by Bike24 Akamai. Target tests, 30-second fuzzing, git diff --check, and make quality passed.
