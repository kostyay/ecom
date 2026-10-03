# YT Industries, Propain, and Canyon discovery

Verified with public, anonymous HTTP requests on **2026-10-03**. Live checks are
separate from the normal offline tests. No account, browser session, CAPTCHA
solver, or proxy was used. These Providers implement **search only**, not item
details, bike configuration, categories, filters, sorts, or variant selection.

## YT Industries (`yt-industries`)

- Storefront: `https://www.yt-industries.com/`.
- Platform: Nuxt storefront backed by Shopware Store API.
- The homepage publishes the European sales-channel key in
  `window.__NUXT__.config.public.shopware.shopwareAccessToken`. Discover it at
  runtime rather than committing a key. Send it as a sensitive `sw-access-key`
  header; a SHA-256 partition separates cached responses for different channels.
- `GET /store-api/context` with this key reports `shippingLocation.country.iso`
  `DE`, `languageInfo.localeCode` `en-GB`, and `currency.isoCode` `EUR`. The
  Provider verifies these values before searching. It does not reuse the
  response's anonymous context token or create session state. The raw context
  response still follows the usual Core cache policy.
- `POST /store-api/search` accepts JSON:

  ```json
  {"search":"capra","page":1,"limit":24,"total-count-mode":1,"associations":{"seoUrls":{},"cover":{"associations":{"media":{}}}}}
  ```

  GET is not supported (405). The response gives `apiAlias: product_listing`,
  `currentFilters.search`, `page`, `limit`, `total`, and `elements`. Those values
  must agree with the request. Pages 1, 2, and 3 returned 24, 24, and 9 entries
  out of 57. An unlikely query returned an explicit zero total and empty array.
- Prices come from `calculatedPrice.unitPrice` and provider-shown `listPrice`,
  never a float conversion. JSON amounts have no formatted display string, so
  the Provider renders the decimal with two fractional digits where needed and
  the EUR symbol. `calculatedCheapestPrice.hasRange` marks a starting price,
  exposed as `provider_data["yt-industries"].starting_price`.
- Prefer canonical, non-deleted SEO URLs. **Preserve `?number=...`** when it
  matches `productNumber`: this chooses the actual bike, unlike tracking query
  parameters. Some search entries have no SEO URL; their ID, name, and price
  remain useful, so their URL stays absent rather than being fabricated.
- Availability comes from YT's own availability extension, not inferred from
  price or an aggregate numeric stock field. The listing does not establish
  that every size can be purchased. Search can include parts and clothing.

## Propain (`propain`)

- The supplied `https://propain-bikes.com/` redirects to
  `https://www.propain-bikes.com/`. English lives under `/en/`.
- Platform: WooCommerce with a country-price plugin and a bike configurator.
- **Do not use the Store API for storefront prices.**
  `/en/wp-json/wc/store/v1/products?search=tyee&per_page=24&page=1` returned 134
  entries, including internal configuration components absent from the visible
  search. Its prices also differed from the rendered storefront. Adding
  `billing_country=DE` did not resolve this. No net-to-gross calculation is made.
- Verified storefront search:

  ```text
  GET /en/?s=tyee&post_type=product&wcpbc-manual-country=DE
  GET /en/page/2/?s=tyee&post_type=product&wcpbc-manual-country=DE
  ```

  Germany is selected explicitly. Captured price labels include 19% VAT and
  separate shipping links. These are server-rendered German catalog prices,
  not a live quote for a configured build or another delivery country.
- Search returns ten products per full page. Captured pages 1, 2, and 4 contain
  10, 10, and 9 products. The WooCommerce current-page marker and next link are
  authoritative. Product totals are not exposed reliably, so none are reported.
- Parse only product cards under the `products` grid. IDs are `post-N` classes;
  names and links use WooCommerce's product-title and product-link classes.
  Prices use visible amount spans, with `del` identifying original prices.
  Ignore duplicated screen-reader prices. Keep `from` prices marked with
  `provider_data.propain.starting_price`; retain two-price ranges when shown.
- The explicit `elementor-products-nothing-found` marker identifies an empty
  search. A generic page or challenge is not an empty result.
- Stock classes describe the listing, not the availability of a particular
  configuration. Do not label third-party parts as Propain-branded products.

## Canyon (`canyon`)

- Use the exact requested market: `https://www.canyon.com/en-es/`, **ES/en/EUR**.
  The repository's default country is DE, so set `ECOM_MARKET_COUNTRY=ES`.
- Platform: Salesforce Commerce Cloud, server-rendered search HTML.
- Verified request:

  ```text
  GET /en-es/search/?q=spectral&searchType=bikes&start=0&sz=24&pn=0&searchredirect=false
  ```

  Later pages use offsets 24, 48, etc., and zero-based `pn`. The site's next
  button points to `/en-es/shop/`; the same parameters on `/en-es/search/`
  return the requested page and keep the operation on a single endpoint.
- **Bikes only**: the search header counted 100 results across tabs, whereas
  the selected bikes tab counted 50. Gear and editorial results are excluded.
  Read the selected tab's `data-count`, not the all-results heading. Pages
  1–3 returned 24, 24, and 2 bikes, including outlet listings.
- Verify footer `data-page-number` and `data-page-size`, and next-link offsets.
  An explicit `js-noResults-search` marker distinguishes a genuine empty first
  page from a changed layout, challenge, or an out-of-range page.
- Product IDs come from the canonical product URL, not the tile's variant SKU.
  Use product name/image elements and `productTile__priceSale` /
  `productTile__priceOriginal`. **Never use analytics `price` (net) or monthly
  financing prices.** Preserve visible EUR text and any `From` qualification,
  exposed as `provider_data.canyon.starting_price`.
- Only explicit in/out-of-stock labels set common availability. Other states
  remain unknown. A listing is not a size/color stock guarantee.
- Direct HTTP worked during discovery and live CLI smoke tests. Browser/CDP
  fallback is allowed by the Provider but was not live-tested. Redirects away
  from the Spain search endpoint fail rather than silently changing markets.

## Shared behavior and fixtures

All three Providers use Core-owned transport/cache/retry policy. They reject
unsupported markets, shipping inclusion, filters, sorts, and page sizes before
resource access. Pages are limited to 1–1000; callers should follow `has_next`.
Requests for another currency return actual EUR prices plus
`currency_unavailable`. Bad product entries produce `partial_parsing` with
found/parsed counts; completely unusable pages fail safely.

Fixtures are under each Provider's `testdata/`, with source and sanitization
notes in its README. HTML captures retain result grids and relevant pagination
or empty-state markers, not entire pages. Scripts, analytics data, unrelated
markup, unused data attributes, SVGs, and responsive image alternatives are
removed. Whitespace is normalized. YT JSON retains parsing-relevant fields and
metadata; the homepage key is replaced and no context token is stored.

Offline tests cover conformance, successful and empty pages, later pages,
partial/malformed results, request policies, market restrictions, URL safety,
price handling, and CLI JSON/table/JSONPath output with cache replay.
