# Architecture Plan

## 1. Goal

Build `ecom` as a machine-readable CLI for product discovery on one commerce provider at a time. The first provider is Bike-Discount. Future providers can use different website protocols while they expose common operations where practical.

The principal agent workflow is:

1. Inspect provider help and capabilities.
2. Find categories, brands, deals, or products.
3. Move through result pages.
4. Get full details and variants for an item.
5. Use the returned displayed prices outside `ecom` when a comparison across providers is necessary.

## 2. Scope

### Included

- Product search
- Category tree navigation and category text search
- Product listing in a category
- Brand navigation and brand text search
- Product listing for a brand
- Native deal discovery
- Provider filters and sort modes
- Page-number pagination and provider-supported page sizes
- Item details by provider ID or provider-owned URL
- Product variants
- Stable JSON, table, and kubectl-style JSONPath output
- Direct HTTP, browser automation, and CDP transport
- SQLite raw-response caching and browser session state
- Request limits, retries, and partial-result warnings

### Excluded

- Product identity matching across providers
- Price comparison across providers
- Shipping-cost calculation
- Currency conversion
- Deal estimation when a provider does not show a reduction
- Automatic CAPTCHA solving and proxy rotation
- A runtime binary plugin protocol

## 3. System Structure

Use these Go package groups:

```text
cmd/ecom or main.go
    -> internal/cli
       -> internal/app
          -> public provider SDK
          -> internal/transport
          -> internal/cache
          -> internal/output
          -> internal/config

provider distribution imports
    -> _ "provider module path"
       -> provider registration
```

The exact package names can change during implementation. The ownership boundaries must remain stable.

## 4. Provider SDK

Publish a small Go package that providers can import without importing CLI internals. It must include:

- An SDK API version constant
- A concurrency-safe provider registry
- Registration validation for name, version, and capabilities
- Common request and response types
- Common product, price, variant, category, brand, filter, sort, page, warning, and error types
- A core service interface for resource requests
- A provider conformance test kit

A provider declares its capabilities. Unsupported operations return a stable `capability_unavailable` error. Provider registration fails clearly for duplicate names or incompatible SDK versions.

The provider interface must cover:

- Information and help
- Product search
- Top-level, child, recursive, and text-search category operations
- Category item listing
- Brand listing, brand search, and brand item listing
- Deal listing
- Available filters and sort modes
- Item details

Provider-specific fields belong in a namespaced `provider_data` object. Provider help explains site-specific syntax, page sizes, filters, market limits, transport needs, and known restrictions.

## 5. Common Data Model

### Product

A product summary or item detail can include:

- Provider item ID and canonical URL
- Name and brand
- Current displayed price and currency
- Original price, discount amount, and discount percentage when shown
- Availability and provider stock text
- Selected and available variants
- Main image URL
- Product attributes
- Retrieval time
- Detail level
- Namespaced provider data

Missing information is absent or `null`. A provider must not invent a value.

### Price

Never store money as a binary floating-point number. Use decimal strings and preserve the provider text:

```json
{
  "amount": "79.95",
  "currency": "EUR",
  "display": "€79.95"
}
```

The displayed item price excludes shipping and optional fees. The Core does not convert currencies. If a provider cannot show the configured currency, return the actual currency and a `currency_unavailable` warning.

### Variants

An item request without variant arguments returns all visible variants. If prices differ, return the applicable variant prices or a price range. Repeatable `--variant key=value` arguments select an exact variant. An invalid selection returns `variant_not_found` with valid choices.

## 6. Command Model

Provider-neutral commands use `--provider <name>`. Configuration can set a default provider. If neither source selects a provider, return a structured error.

The planned commands are:

```text
ecom provider help <provider>
ecom search <query>
ecom categories
ecom category-items <category-id>
ecom brands
ecom brand-items <brand-id>
ecom deals
ecom filters
ecom item <item-id-or-url>
ecom cache clear
ecom cache prune
ecom provider session clear <provider>
```

Common options include:

```text
--provider
--filter key=value
--sort
--page
--page-size
--variant key=value
--refresh
--stale-if-error
--interactive
-o json
-o table
-o jsonpath='{...}'
```

There is no `--all` mode. Each listing command returns one page. Providers report and validate supported page sizes. Normalize page numbers where possible, and let provider help describe differences.

Product search returns products only. Use separate category and brand text-search operations. Category search uses a provider function when one exists. Otherwise, it searches the locally cached category tree without case sensitivity.

## 7. Output Contract

JSON is the default for data commands. Use a stable envelope similar to:

```json
{
  "schema_version": "1",
  "provider": "bike-discount",
  "market": {
    "country": "DE",
    "language": "en",
    "currency": "EUR"
  },
  "data": {},
  "page": {},
  "cache": {
    "hit": true,
    "stored_at": "2026-08-12T10:00:00Z",
    "age_seconds": 3600,
    "ttl_seconds": 86400
  },
  "warnings": []
}
```

Use `-o table` for people. Use kubectl-style templates with `-o jsonpath='{...}'` for selected fields. JSONPath applies to the complete envelope and returns the rendered selection. Invalid templates return a structured error. JSONPath and table output cannot be combined.

Return partial results when at least one useful entry was parsed. Add warnings with found and parsed counts, a stable code, a short reason, and the item URL or ID when available. Fail when no useful result or valid page metadata is available.

## 8. Configuration

Extend the global configuration with these defaults:

```yaml
provider: bike-discount

market:
  country: DE
  language: en
  currency: EUR

pricing:
  include_shipping: false

cache:
  path: ""
  ttl: 24h
  max_size: 512MiB
  max_response_size: 20MiB

network:
  requests_per_second: 1
  max_concurrent_http: 2
  max_concurrent_browser: 1
  retries: 3

browser:
  cdp_address: ""
  headed: false
  interactive_timeout: 5m

providers:
  bike-discount:
    page_size: 48
```

`pricing.include_shipping` is a global provider request policy. A provider must
reject an unsupported value before it requests a website resource. The
Bike-Discount provider supports only `false` and returns displayed item prices
without shipping or optional fees.

The configured file remains in the operating system user configuration directory. Flags override environment variables, and environment variables override the file.

The Core validates known settings. Each provider validates its settings block. Cache keys include provider, market, URL, method, parameters, request body, and other response-affecting request data. Secrets never appear in cache keys.

## 9. Transport

The Core applies this sequence:

1. Use direct HTTP.
2. Use an isolated browser when JavaScript is necessary or direct HTTP is blocked.
3. Use a configured existing Chrome session through CDP when an isolated browser is blocked.
4. If manual action is necessary, return `browser_challenge_required` unless `--interactive` is present.

Interactive mode opens a headed browser and waits for the person to complete the challenge. It then stores portable session state. Non-interactive commands never wait for input.

Providers state the resource and required transport properties. They do not create clients or browsers. Prefer data sources in this order:

A provider transport preference list is the complete allowed order. The Core does not append omitted transports. A required transport selects that transport only.

1. A JSON endpoint used by the website
2. Structured page data such as JSON-LD
3. HTML parsing
4. Browser DOM extraction

## 10. Rate Limits and Retries

Default limits for each provider are:

- One network request each second
- Two concurrent HTTP requests
- One concurrent browser page
- Three retries for HTTP 429, 502, 503, and 504

Honor `Retry-After`. Use exponential delay with random variation. Limit all retry delays, including website-supplied `Retry-After` values, to one minute. The configured retry count is the number of attempts after the first request. Provider configuration can override limits. A valid cache hit does not consume a network request permit.
Browser and CDP requests share the browser concurrency limit because both use browser-family resources. HTTP requests use a separate concurrency limit.

## 11. SQLite Storage

Use one SQLite database in the operating system cache directory unless configuration changes the path. Use WAL mode and a busy timeout. Apply versioned schema migrations automatically.

Store successful raw HTTP responses and browser page content. Do not cache normalized command results. Do not cache challenge pages, access-block pages, or server errors. Compress large text responses.

Use a 24-hour default TTL. `--refresh` bypasses valid entries for all requests in the command and replaces them only after a successful fresh response. `--stale-if-error` can return an expired entry when refresh fails. Without that flag, identify the expired entry and its age in the structured error, but do not return its data.

Prune a small number of expired entries during normal use. Explicit cache commands can clear entries by provider or run full pruning. After expired entries, remove least-recently-used response entries to meet the 512 MB limit. Reject a response larger than 20 MB by default.

Store portable browser state, such as cookies and local storage, as JSON in SQLite. Create a temporary browser profile for a run. Do not store browser history, extensions, passwords, full profiles, or CDP-connected Chrome state. Cache maintenance does not remove session state.

## 12. Bike-Discount Provider

Implement Bike-Discount as the first external provider module. Its discovery work must identify:

- Search requests and native result data
- Category and brand structures
- Deal pages or discount filters
- Filter identifiers and allowed values
- Sort values
- Supported page sizes and page-number behavior
- Item IDs, canonical URLs, structured product data, and variants
- Market cookies or request parameters
- Conditions that require browser or CDP transport

The provider can use normal browser headers, cookies, JavaScript, Playwright-compatible automation, or CDP as required. It does not treat `robots.txt` as an access-control policy. The first version does not add CAPTCHA-solving services or proxy rotation.

## 13. Test Strategy

Core tests must use temporary SQLite databases, fake clocks, fake HTTP services, and fake browser services. Test migrations, key separation by market, TTL behavior, refresh behavior, stale fallback, pruning, compression, concurrency, limits, retries, and cancellation.

The SDK conformance kit must test registration, API version, declared capabilities, common fields, help, structured errors, partial results, pagination, filters, and Core transport integration.

The Bike-Discount provider must use saved HTML and JSON fixtures for normal automated tests. Live website tests are optional and must be clearly separated because they are slow and unstable.

## 14. Delivery Sequence

1. Define the SDK, registry, domain types, and output envelope.
2. Extend configuration and implement SQLite storage.
3. Implement HTTP transport, limits, retries, cache policy, and test doubles.
4. Add browser and CDP transport with portable session state.
5. Add provider-neutral commands and output modes.
6. Research and implement the Bike-Discount provider with fixtures.
7. Add conformance, integration, and command tests.
8. Update user and provider-author documentation.
