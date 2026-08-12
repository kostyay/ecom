---
id: com-d502
status: closed
deps:
- com-ae9c
- com-6624
created: "2026-08-12T16:57:43Z"
type: task
priority: 1
assignee: kostyay
parent: com-b67f
tests_passed: true
---
# Implement Bike-Discount item details and variants

Resolve provider IDs or owned URLs and parse canonical URL, attributes, stock, images, price ranges, and visible variants.

## Design

Preserve provider display text and actual currency.

## Acceptance Criteria

Default item output contains visible variants; exact variant selection maps correctly; foreign URLs and absent variants fail clearly.

## Tests

Fixture tests cover equal and different variant prices, stock, and invalid input.
