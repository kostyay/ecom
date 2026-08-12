# ADR 0001: Use Compiled Go Providers

## Status

Accepted

## Decision

Each provider is a Go module that registers itself through an `init` function. A CLI distribution enables the provider with a blank import. A versioned Provider SDK validates compatibility in code during registration.

The Core owns shared policy and infrastructure. Providers own website-specific requests, navigation, capabilities, and parsing.

## Consequences

- A new provider requires a new build of the CLI distribution.
- Providers can be independent Go modules.
- The SDK can include a conformance test kit.
- A dynamic process or binary plugin protocol is outside the first version.
