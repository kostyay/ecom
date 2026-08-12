# Issue Tracker: kt

Tickets for this repository are local Markdown files managed by `kt`. The files are stored in `.ktickets/`.

## Conventions

- Use `kt create` to create an epic, feature, task, bug, or chore.
- Give each implementation task a parent epic with `kt create --parent <epic-id>`.
- Use `kt dep add <ticket-id> <dependency-id>` when one ticket cannot start before another ticket is complete.
- Use `kt show`, `kt ls`, `kt ready`, and `kt query` to read work state.
- Use `kt start`, `kt pass`, and `kt close` to move work through its lifecycle.
- Use `kt add-note` to record important progress or decisions.
- Do not edit `.ktickets/` directly when a `kt` command supports the necessary operation.

## When a skill says "publish to the issue tracker"

Create the applicable ticket with `kt create`. Include a concise description, acceptance criteria, design notes when necessary, and test requirements. Add its parent and dependencies.

## When a skill says "fetch the relevant ticket"

Use `kt show <ticket-id>`. Use `kt query --json` when the operation needs structured data from multiple tickets.
