# User Guide

## Command model

Each command uses one provider. Select it in one of these ways:

```sh
ecom --provider bike-discount search "helmet"
ECOM_PROVIDER=bike-discount ecom search "helmet"
ecom --config ./config.yaml search "helmet"
```

The default configuration selects `bike-discount`. There is no command that searches all providers. Use this command before an agent makes provider-specific requests:

```sh
ecom provider help bike-discount
ecom provider help bike-discount -o table
```

Provider help is the source of truth for capabilities, filter keys, sort values, markets, page sizes, access limits, and known warnings.

## Find products

### Search

Search returns products only:

```sh
ecom search "powertube"
ecom search "powertube" --page 1 --page-size 48 -o table
```

Bike-Discount uses a verified legacy search request. The site can ignore the query. Search results include the `search_semantics_unverified` warning. Check the product names before you use the result.

### Categories

List root categories, list children, or get the recursive tree:

```sh
ecom categories
ecom categories --parent 018c7eacc99370f89d7947c78ee08b5b
ecom categories --recursive
```

Search category names and paths with text:

```sh
ecom categories "brakes"
```

Bike-Discount has no verified native category search. The CLI gets the recursive category tree and searches it locally without case sensitivity. JSON output gives `data.search_method` as `local`.

Use an opaque category ID from a category result to list products:

```sh
ecom category-items 018c7eacc99370f89d7947c78ee08b5b --page 1 --page-size 48
```

An ID can be a navigation ID or an exact canonical path returned by the provider. Do not make an ID from a category name.

### Brands

List or search brands:

```sh
ecom brands
ecom brands "shimano" -o table
```

Bike-Discount has no verified native brand search. The CLI searches the complete brand index locally without case sensitivity. Use the returned canonical brand slug:

```sh
ecom brand-items shimano --page 1 --page-size 48
```

### Deals

Get provider-declared deals:

```sh
ecom deals --page 1 --page-size 48 -o table
```

Bike-Discount uses its stable bike sale page. A result is a deal only if the site shows an original price, a discount amount, or a discount percentage. `ecom` does not calculate a deal score. It does not combine the separate outdoor, running, e-bike, or special-deal pages.

### Items and variants

Get one item by its displayed numeric item number or complete English Bike-Discount URL:

```sh
ecom item 20166382
ecom item "https://www.bike-discount.de/en/yamaha-500-wh-36v/13.6ah-frame-battery"
```

An item request without `--variant` returns all visible variants. Use exact label and value text from the item result:

```sh
ecom item 20166382 --variant 'Size=M'
ecom item 20166382 --variant 'Color=black' --variant 'Size=M'
```

The key and value are case-sensitive provider values. A selection that does not exist returns `variant_not_found`.

## Filters, sort, and pages

Filter syntax is provider-specific. Each filter is `key=value`. Repeat `--filter` only if provider help marks the key as repeatable:

```sh
ecom search "helmet" --filter manufacturer=0123456789abcdef0123456789abcdef
ecom category-items 018c7eacc99370f89d7947c78ee08b5b \
  --filter properties=0123456789abcdef0123456789abcdef \
  --filter properties=fedcba9876543210fedcba9876543210
```

Bike-Discount accepts these filter keys for `search`, `category-items`, `brand-items`, and `deals`:

- `manufacturer`: one 32-character ID from the current listing.
- `properties`: a repeatable 32-character ID from the current listing.

Do not copy an ID from a different listing. Bike-Discount joins repeated `properties` values with `|` for the website request.

The only verified Bike-Discount sort value is `standard`:

```sh
ecom deals --sort standard
```

List the filter and sort definitions for all listing commands or for one
capability:

```sh
ecom filters
ecom filters deals -o table
ecom filters category_items --category 018c7eacc99370f89d7947c78ee08b5b
ecom filters brand_items --brand shimano
```

Bike-Discount returns its verified static definitions. A future provider can
use the category or brand context to return definitions for one listing.

Listing commands return one page. Bike-Discount pages start at 1. Its only verified page size is 48:

```sh
ecom search "helmet" --page 2 --page-size 48
```

There is no `--all` flag. Read `page.has_next` when the provider reports it. Then request the next page.

## Output

### JSON envelope

JSON is the default. `-o json` and `--json` also select JSON. Data commands return one JSON document with this stable top-level shape:

```json
{
  "schema_version": "1",
  "provider": "bike-discount",
  "market": {"country": "DE", "language": "en", "currency": "EUR"},
  "data": {},
  "page": {},
  "cache": {},
  "warnings": [],
  "transport_attempts": []
}
```

Optional fields are absent when they do not apply. Product lists are in `data.items`. One full product is in `data.item`. The `cache` object summarizes all resources used by the command.

### Table

Use tables for people:

```sh
ecom deals -o table
ecom item 20166382 -o table
```

Table output is not a stable machine interface.

### JSONPath

JSONPath uses kubectl-style templates and reads the complete envelope:

```sh
ecom search "helmet" -o 'jsonpath={.data.items[*].url}'
ecom deals -o 'jsonpath={range .data.items[*]}{.product.name}{"\\t"}{.product.price.display}{"\\n"}{end}'
ecom item 20166382 -o 'jsonpath={.data.item.variants[*].attributes}'
```

The shell quotes keep braces and special characters unchanged. An invalid template returns `invalid_output_template`. Do not combine `--json` with table or JSONPath output.

## Price and market policy

The global market selects a country, language, and requested currency:

```yaml
market:
  country: DE
  language: en
  currency: EUR
pricing:
  include_shipping: false
```

`ecom` preserves the site's displayed price text and decimal amount. It does not use a binary floating-point value for money. It does not convert currency. If the site returns another currency, the result keeps that currency and can include `currency_unavailable`.

`pricing.include_shipping` is a global provider request policy. The default is `false`. Bike-Discount does not support shipping-inclusive prices. For Bike-Discount, `true` returns `invalid_provider_config` before a network request. With the supported value, all item and variant prices exclude shipping and optional fees.

## Configuration

The optional configuration file is `ecom/config.yaml` in the operating system user configuration directory. Use `--config <path>` to select a different file.

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
log:
  level: info
  file: ""
```

An empty `log.file` disables persistent logs. It does not create a log
directory. Set `log.file` or use `--log-file` when you need JSON logs with
rotation.

Precedence is:

1. Command flags.
2. `ECOM_` environment variables.
3. The configuration file.
4. Built-in defaults.

Use `_` for nested environment keys. Examples are `ECOM_PROVIDER`, `ECOM_MARKET_COUNTRY`, `ECOM_PRICING_INCLUDE_SHIPPING`, `ECOM_CACHE_TTL`, and `ECOM_BROWSER_CDP_ADDRESS`.

`network.requests_per_second` limits starts of all requests for one provider. HTTP requests use `max_concurrent_http`. Browser and CDP work share `max_concurrent_browser`. `network.retries` is the number of HTTP retry attempts after the first attempt. Keep the default rate of one request per second for Bike-Discount unless the site gives a different limit.

## Cache and sessions

Raw successful HTTP responses are in SQLite. Browser cookies and local storage are also in SQLite. The default database is `ecom/cache.db` in the operating system user cache directory. Set `cache.path` to use an exact file.

The default cache TTL is 24 hours. A command does not request the same fresh resource again during this time. Use these flags on network data commands:

```sh
ecom search "helmet" --refresh
ecom search "helmet" --stale-if-error
```

`--refresh` bypasses a fresh cached response. `--stale-if-error` permits an expired response only if a fresh request fails. Cached output reports its state in `cache`.

Use maintenance commands as follows:

```sh
ecom cache prune
ecom cache clear
ecom --provider bike-discount cache clear
ecom provider session clear bike-discount
```

`cache prune` removes expired entries and least-recently-used entries above the size limit. `cache clear` clears all providers unless `--provider` is explicit. Session clear removes the exact provider and configured-market session. It does not clear response cache entries.

## Browser, CDP, and challenges

`ecom` first uses direct HTTP. If access is blocked, it can use an isolated Chrome or Chromium process. Install a Chrome-family browser that `chromedp` can find on the system.

To use an existing browser profile, start Chrome with a DevTools address and a dedicated user data directory. One example is:

```sh
google-chrome --remote-debugging-port=9222 --user-data-dir=/tmp/ecom-chrome-profile
```

Then configure the address:

```yaml
browser:
  cdp_address: http://127.0.0.1:9222
  headed: false
  interactive_timeout: 5m
```

Do not expose the DevTools port to another computer. The CDP transport uses a new target and does not stop the existing browser.

Bike-Discount can show a Cloudflare challenge. A normal command does not wait for a person. It returns `browser_challenge_required`. To permit manual action, use:

```sh
ecom search "helmet" --interactive
```

Interactive mode opens a headed isolated browser and waits up to `browser.interactive_timeout`. Complete the challenge in that browser. The command stores the resulting portable session state in SQLite. If time expires, it returns `browser_challenge_timeout`. Use `ecom provider session clear bike-discount` to remove the stored state.

## Error codes

Data commands use JSON errors unless table output is selected:

```json
{"error":{"code":"invalid_filter","message":"..."}}
```

Stable codes are:

| Code | Meaning |
| --- | --- |
| `command_error` | The command arguments or flag combination is invalid. |
| `config_error` | The global configuration cannot be read, decoded, or validated. |
| `log_error` | Logging cannot start. |
| `provider_required` | No provider was selected. |
| `provider_not_found` | The selected provider is not compiled into this binary. |
| `provider_conflict` | A positional provider and `--provider` are different. |
| `invalid_provider_config` | Provider configuration or a provider policy is not supported. |
| `capability_unavailable` | The provider does not support the command operation. |
| `invalid_filter` | A query, filter, sort, page, ID, or selection is invalid. |
| `variant_not_found` | No visible variant matches all selections. |
| `invalid_output_template` | JSONPath is invalid or cannot run. |
| `invalid_resource_request` | A provider resource request is unsafe or invalid. |
| `access_blocked` | The website blocked access. |
| `browser_challenge_required` | Manual browser action is required but not permitted. |
| `browser_challenge_timeout` | Manual browser action did not finish in time. |
| `transport_unavailable` | A required transport is not configured. |
| `retryable_http` | An HTTP response can be tried again. |
| `http_failure` | An HTTP request, response, or page parse failed. |
| `browser_failure` | Browser or CDP work failed. |
| `response_too_large` | A response is larger than the configured limit. |
| `invalid_provider_result` | Provider output does not satisfy the SDK contract. |

Warnings do not make a useful result fail. Read `warnings` on every successful JSON result. Important warning codes are `partial_parsing`, `currency_unavailable`, and `search_semantics_unverified`.

## Bike-Discount limits

- Only `DE/en/EUR` is verified.
- No currency conversion occurs.
- Prices exclude shipping and optional fees.
- Page numbers start at 1. Page size 48 is the only verified size.
- `standard` is the only verified sort value.
- Search can ignore the legacy query parameter. Check returned products.
- Category and brand text search run locally over provider listings.
- Deals come only from the main bike sale page and only use site-shown reductions.
- Filter IDs must come from the current target listing.
- Cloudflare can require browser or manual action.
- The tool does not solve CAPTCHAs or rotate proxies.
