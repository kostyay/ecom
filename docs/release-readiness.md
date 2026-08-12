# Release Readiness Review

Review date: 2026-08-13

## Result

The first version is ready for an offline release. All repository checks pass.
The CLI, Provider SDK, Core transport, SQLite state, Bike-Discount fixtures,
user guide, and provider author guide meet their parent epic acceptance
criteria.

The review found and fixed two release defects:

- A command with an empty `log.file` created a log directory in the operating
  system user cache. Persistent logging is now disabled when this setting is
  empty. An explicit file still enables JSON logs and rotation.
- The architecture required `ecom filters`, but the root command did not
  contain it. The command now supports provider, capability, category, brand,
  cache, browser, JSON, table, and JSONPath options. Bike-Discount now declares
  and returns its verified static filter and sort definitions.

## Repository checks

The review ran these commands from the repository root:

```sh
make fmt-check
make lint
make vet
make test
make fixtures
make doc-test
make race
make build
git diff --check
```

All commands passed. The test and race commands used permission to open local
`httptest` ports. They did not use an external website. The build used an
isolated writable Go build cache. The restricted environment could not update
the optional Go module download-stat file, but the build completed and wrote
`bin/ecom`.

The review also checked all Go files with `gofmt`, scanned source files for
common secret forms, and searched the repository for temporary databases,
logs, coverage files, and temporary files. It found no source-format error,
secret, or generated release artifact. `bin/` is ignored as a local build
output.

## Binary smoke checks

The binary smoke test used new temporary configuration and cache directories.
It verified:

- Root help and the help for each command.
- Text and JSON version output.
- Provider help in JSON, table, and JSONPath output.
- The stable schema version and provider fields in JSON.
- Filter discovery in JSON, table, and JSONPath output.
- A clean SQLite migration to user version 2 in WAL mode.
- SQLite integrity after close and reopen.
- A second migration startup on the same database.
- Cache prune, cache clear, and provider session clear.

`version`, root help, and provider help created no file in the temporary user
configuration or cache directories. Maintenance commands created the configured
SQLite file as required.

## Parent acceptance review

| Parent epic | Evidence |
| --- | --- |
| Provider SDK and domain contracts | Unit tests, conformance tests, and provider author example pass. |
| Configuration and SQLite state | Configuration, migrations, cache, session, prune, reopen, WAL, and integrity checks pass. |
| Core transport services | Offline HTTP, retry, limit, browser, CDP, challenge, cancellation, and race tests pass. |
| Provider-neutral CLI and output | All planned commands resolve. JSON, table, JSONPath, paging, filters, variants, refresh, stale fallback, and maintenance tests pass. |
| Bike-Discount provider | Fixture parser tests and the complete offline conformance suite pass. |
| Integration, documentation, and release readiness | End-to-end fixture workflow, documentation tests, quality checks, build, and binary smoke checks pass. |

## Remaining live-site risks

These risks do not block the offline release:

- Cloudflare can block direct HTTP and isolated browser access. A person can
  need interactive browser action or an existing Chrome CDP session.
- The verified legacy Bike-Discount search request can ignore the query. Each
  search result includes `search_semantics_unverified`.
- Only the `DE/en/EUR` market, page size 48, and `standard` sort are verified.
- Filter IDs must come from the current target listing. Static filter discovery
  cannot supply the site IDs for a live listing.
- Category and brand text search use local filtering because no native text
  search request is verified.
- Live website tests are optional and were not part of this review. Saved,
  sanitized fixtures are the release test source.
