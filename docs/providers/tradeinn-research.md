# Tradeinn network search research

Status: verified with Chrome DevTools Protocol and implemented on 2026-09-19

This note records the public search flow used by the Tradeinn website network.
`bikeinn.com` redirects to `tradeinn.com/bikeinn`. The search on that page can
return products from other Tradeinn shops.

## Result

The compiled `tradeinn` Provider supports network search. One Provider covers
the shops; a separate Provider for each shop is not needed.

The browser sends one public HTTP POST request. The response is JSON. It does
not need an account, a browser challenge, or a browser after the request shape
is known. The endpoint is an internal website interface, not a documented
public API. It can change without notice.

## Evidence and access

The research used a new headless Chrome session through Chrome DevTools
Protocol. It typed `powertube` in the Bikeinn search box and recorded the
website requests. Direct read-only HTTP requests then repeated the search and
the next page.

| Request | Result |
| --- | --- |
| `GET https://www.bikeinn.com/` | Redirected to `https://www.tradeinn.com/bikeinn/es` for the detected market. |
| `GET https://www.tradeinn.com/bikeinn/en` | HTTP 200 with the public shop page. |
| Search POST from Chrome | HTTP 200 with a JSON body and 45 results. |
| Same POST with form-encoded data and normal browser headers | HTTP 200 with the same result contract. |
| Search POST with no `visitorid` | JSON error: `visitorId` is required. |
| Search POST with a locally generated numeric visitor ID | HTTP 200 with normal results. |
| Search POST with the website's anonymous fallback visitor ID | HTTP 200 with normal results. |

The implementation sends the normal browser user agent and referrer headers.
An earlier curl check returned an empty body, but its visitor ID also differed.
This does not establish a user-agent restriction. No account cookie was needed.
The Provider sends the public `id_pais` market cookie.

## Search request

The browser sends this request as `multipart/form-data`. The endpoint also
accepted `application/x-www-form-urlencoded`, which is simpler for the Core
resource service.

```http
POST https://www.tradeinn.com/listado.php
Content-Type: application/x-www-form-urlencoded
Referer: https://www.tradeinn.com/en?country=de
Cookie: id_pais=75
User-Agent: <normal browser user agent>

action=buscador_google
palabras=powertube
id_tienda=0
nextToken=null
visitorid=1556872684.1715855695
idioma=eng
```

The website reads its visitor ID from the `_ga` cookie. Its `getVid` script
reverses the two numeric parts and has the public anonymous fallback shown
above. A generated numeric pair was also accepted. It is not a credential.
The Provider uses the stable fallback so identical searches can reuse the Core
cache. The request body is sensitive because later requests contain a cursor;
its SHA-256 hash supplies the cache partition.

`id_tienda=4` identifies Bikeinn. `id_tienda=0` identifies Tradeinn. Both values
returned the same 48 matches for the checked query. The result included 36
Bikeinn products and 12 Traininn products. Thus, `id_tienda` does not restrict
the response to one shop in this flow.

The current shop map stored by the page was:

| ID | Shop | ID | Shop |
| --- | --- | --- | --- |
| 0 | tradeinn | 1 | diveinn |
| 2 | snowinn | 3 | trekkinn |
| 4 | bikeinn | 5 | smashinn |
| 6 | swiminn | 7 | waveinn |
| 8 | motardinn | 9 | outletinn |
| 10 | runnerinn | 11 | goalinn |
| 12 | dressinn | 13 | traininn |
| 14 | xtremeinn | 15 | kidinn |
| 16 | techinn | 17 | bricoinn |
| 18 | hunt | 19 | horse-riding |
| 20 | golf | 21 | basketball |
| 22 | baseball | 23 | american-football |
| 24 | handball | 25 | hockey |
| 26 | rugby | 27 | volleyball |
| 28 | nutrition | 777 | sports |

Treat these IDs as observed website data. Do not make the full map a required
search contract. Each result has its shop ID and a shop URL.

## Search response

The server sends `Content-Type: text/html`, but the body is JSON. The top-level
fields observed were:

```text
results
facets
totalSize
attributionToken
nextPageToken
correctedQuery
queryExpansionInfo
```

Each result contained:

- A numeric model ID.
- A title and optional brand.
- `IN_STOCK` availability for the checked results.
- A product URI with the Tradeinn shop path.
- A `shopId` attribute.
- Price fields for many country IDs.

The `powertube` check returned 48 results. The first response contained 45
results and a `nextPageToken`. A second request with that token contained the
last three results and no next token. Preserve the result order. Follow tokens
to implement later page numbers.

The product URI contains internal query values. The Provider removes the full
query and uses the remaining product URL. The browser can use a different slug
for the same product ID. Do not return internal margin, sales, stock, or cost
parameters in the Product summary URL.

The search response does not give one simple current price field. It groups
prices by numeric country ID, for example `price_all_1_to_10`, and each entry
has the form `<country-id>:<amount>`. The checked Andorra page used country ID
4 and EUR; Germany used country ID 75 and EUR. Chrome checks of both markets
confirmed the displayed price for product `139041968`: 675.99 EUR in Andorra
and 739.99 EUR in Germany. Select the matching country value, not the first
price. Do not infer currency from the amount.

Tradeinn can return related products for an unmatched query. The checked query
`zzzxxyy987654321` returned nine products, a corrected query of
`zzxxyy 987654321`, and `queryExpansionInfo.expandedQuery=true`. The Provider
reports a `search_semantics_unverified` warning and the correction/expansion
metadata. This is not an empty-result fixture.

## Facets and suggestions

The response included facets for categories, shop IDs, brands, color, size,
gender, availability, and internal ranking values. The website sends optional
filters in an `atributos` JSON array. The first Provider does not need these
filters.

Autocomplete uses the same endpoint with `action=suggest`. The checked `power`
request returned a `completionResults` array. Autocomplete is not needed for
the CLI search Capability.

## Implemented first version

The `tradeinn` Provider supports Search only:

- Use Core-owned HTTP transport and form-encoded POST data.
- Support the verified `AD/en/EUR` and `DE/en/EUR` markets. Other requested
  currencies produce EUR prices and a `currency_unavailable` warning.
- Use the website's public anonymous fallback visitor ID.
- Support page size 45 only.
- Follow `nextPageToken` values from page one to the requested page, with a
  Provider limit of ten pages. Reject repeated tokens.
- Return the model ID, clean product URL, title, brand, selected-country price,
  availability, retrieval time, and shop data. Catalog `IN_STOCK` does not
  establish immediate warehouse stock or a delivery date.
- Report `totalSize` and `has_next`. Do not infer total pages from page size.
- Reject filters and sort values in the first version.
- Leave the image and variants absent. The website derives image URLs and did
  not return variants in the checked response.

Saved, sanitized JSON fixtures cover first, last, and expanded-query results.
Offline tests cover parsing, conformance, failures, paging, CLI output, and
cache separation. Live checks remain outside the normal test suite. No browser
fallback is implemented; the current check does not justify browser code.

## Verification

The built CLI passed live checks on 2026-09-19: `powertube` returned 45 products
on page 1 and three on page 2; a repeated page-2 request reused both cached
responses. The Andorra request returned 675.99 EUR for the first product. The
unmatched query returned nine related products with the expected warning and
correction metadata. `make quality` passed, including lint, vet, offline tests,
documentation checks, race checks, and the build.
