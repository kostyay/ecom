---
name: repository-workflow
description: Apply the ecom repository rules when planning, changing, reviewing, or documenting this codebase.
user-invocable: false
---

# Repository workflow

- Use ASD-STE100 Simplified Technical English in user messages and project documents.
- Before work, read `CONTEXT.md`, `docs/architecture.md`, and the relevant records in `docs/adr/`. Continue if a file does not exist.
- Use the terms from `CONTEXT.md` in tickets, code, tests, and documents.
- Identify a conflict with an architecture decision before you make the change.

## Tickets

Before ticket work, read `docs/agents/issue-tracker.md` and use `kt`. Do not edit `.ktickets/` directly when `kt` supports the operation.

When asked to publish work to the issue tracker, create the applicable ticket with acceptance criteria, design notes when needed, test requirements, a parent epic, and dependencies.

When asked to fetch a ticket, use `kt show <ticket-id>`. Use `kt query --json` only when you need structured data from multiple tickets.

## Triage

When a workflow needs a triage state, read `docs/agents/triage-labels.md` and use one of its five standard values.
