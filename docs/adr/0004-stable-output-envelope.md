# ADR 0004: Use a Stable JSON Envelope

## Status

Accepted

## Decision

Data commands return a stable, versioned JSON envelope by default. Human-readable tables are available with `-o table`. Kubectl-style JSONPath templates are available with `-o jsonpath='{...}'`.

Errors and warnings use stable codes. Command errors and diagnostics go to
standard error. Persistent structured logs require an explicit log file. This
keeps help and version commands free of storage side effects.

## Consequences

- Agents can depend on common metadata and data locations.
- JSONPath output can intentionally remove the envelope.
- Output schema changes require compatibility care and schema-version changes when necessary.
