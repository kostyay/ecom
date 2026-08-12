---
id: com-c8c6
status: closed
deps:
- com-8479
created: "2026-08-12T16:56:53Z"
type: task
priority: 1
assignee: kostyay
parent: com-4a93
tests_passed: true
---
# Implement interactive browser challenge flow

Return browser_challenge_required in normal mode and allow headed manual completion only with --interactive.

## Design

Do not integrate CAPTCHA-solving services.

## Acceptance Criteria

Non-interactive commands never wait; successful interactive completion saves portable state; timeout and cancellation are clear.

## Tests

State-machine tests cover normal, interactive, success, timeout, and cancellation.
