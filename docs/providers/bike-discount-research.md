# Bike-Discount Provider Research

Status: controlled research on 2026-08-13

This note records the public discovery interfaces that were verified for the first
Bike-Discount provider. It also records the limits of the research. The provider
must not treat an unverified candidate as a stable website contract.

## Evidence and request limit

The research used a new Chrome session and a small number of direct, read-only
requests. No account was used. No cookie value, token, or other secret is in this
file.

| Source | Result | Use |
| --- | --- | --- |
| `GET https://www.bike-discount.de/en/llms.txt` in Chrome | Text response loaded | Root navigation names and IDs |
| `GET https://www.bike-discount.de/en/` in a new Chrome session | Cloudflare `Attention Required!` page | Challenge behavior |
| `GET /en/navigation/018c7eacc99370f89d7947c78ee08b5b` with normal browser headers | HTTP 403 from Cloudflare | Direct HTTP block behavior |
| Publicly indexed copies of official Bike-Discount pages | Current product, category, brand, deal, filter, page, and item text | HTML structure and canonical URLs |

The public index had pages crawled between the same day and three months before
this research. Price and stock evidence can become old quickly. It is structure
evidence only. Normal provider results must come from a current website response
or a cached response that follows the configured cache policy.

## Source selection

Use this order for the first implementation:

1. Use `llms.txt` for the root navigation IDs.
2. Use a verified website JSON endpoint if later browser network inspection finds
   one. No catalog JSON endpoint was verified in this research.
3. Use JSON-LD only after a fixture proves that the required fields are present.
   JSON-LD was not available for inspection from the challenged session.
4. Parse the server-rendered HTML for listings and item details.
5. Use browser DOM extraction when Cloudflare or JavaScript prevents direct HTML
   access.

Do not add a candidate JSON endpoint only because the site uses Shopware 6. The
public pages state that they use Shopware 6, but this fact does not make an
endpoint or its response contract stable.

## Search

Chrome inspection on 2026-08-13 verified the current search form:

```http
GET /en/search?search=powertube
```

The response title was `Search results for powertube` and the page contained 28
current product cards. An exact category term can redirect to its canonical
category page. For example, `search=helmet` redirected to `/en/helmets`.

Typing in the search field sends this XHR request:

```http
GET /en/suggest?search=helmet
```

The XHR response has the `text/html` content type. It is an HTML suggestion
fragment, not a JSON catalog response. Product cards contain a
`data-product-information` JSON attribute with the internal catalog ID, name,
brand, and numeric current price. The displayed SKU, URL, image, price text, and
original price remain in card markup. No separate JSON search endpoint was
observed in the storefront flow.

Use the current `search` parameter for production search. Parse the embedded
product information where it is present, and use the card markup for fields
that the JSON attribute does not contain.

### Legacy evidence

The public index verifies this legacy search request shape:

```http
GET /en/search?sSearch=aggressor
```

It produced a page titled `Search results for`. The indexed result count was not
credible for the query. Thus, `sSearch` can be ignored by the current site. Do
not use it as the only production search request without a new fixture.

Search results use the same listing cards, filters, sort control, and page
control as category listings. Return products only. Do not return category or
brand suggestions as products.

## Categories

`/en/llms.txt` returned these root entries:

| Name | Navigation ID |
| --- | --- |
| Running | `018c7eacc3e07089b55937edf7eb9e72` |
| Bike | `018c7eacc99370f89d7947c78ee08b5b` |
| Streetwear | `018c7ead02497042a9285a4a9883f748` |
| Ski | `018c7ead0bef70a29aaac185ce6ee42f` |
| Triathlon | `018c7eadb0427077a6df426a045d1c7f` |
| Outdoor | `018c7eadd05e70bca25079f5cc8e2c88` |
| Brands | `018ee59c43b577488595126670213482` |

The links in the file use this form and do not include a language segment:

```http
GET /navigation/018c7eacc99370f89d7947c78ee08b5b
```

The rendered English navigation uses canonical path URLs. Examples include:

```text
/en/bike/sale
/en/bike/bike-parts/mountain-bike-parts/brakes/disc-brake-sets
/en/mountain-bike-parts
```

Nested navigation links are the verified source for child categories. Use the
32-character navigation ID when it is present. Otherwise, use the canonical
path as an opaque provider category ID. Do not construct category paths from
translated names.

No separate native category text-search interface was verified. Category text
search must use a case-insensitive search of the cached category tree.

## Brands

The complete alphabetical brand index is at:

```http
GET /en/brands
```

The older `/en/hersteller-uebersicht` URL redirects to `/en/brands`. The page
groups brands under `#` and `A` through `Z`. Brand links use a canonical slug.
For example:

```http
GET /en/shimano
GET /en/shimano/deore
```

The Shimano page contains product listing cards and category links. Use the
canonical slug, such as `shimano`, as the public brand ID. A listing filter can
also expose an internal manufacturer UUID. Keep that UUID in `provider_data` and
do not replace the public slug with it.

No separate native brand text-search request was verified. Brand text search
must use a case-insensitive search of the cached `/en/brands` result.

## Deals

The principal stable deal listing is:

```http
GET /en/bike/sale
```

The page has normal listing pagination and deal subcategories. Verified child
types include close-outs, B-stock, men's clothing, women's clothing, children's
clothing, components, accessories, and electronics. Other verified deal paths
include:

```text
/en/outdoor/sale
/en/running/sale
/en/e-bike_sale
/en/bike/bike/bike-sale/special-deals
```

Campaign pages, such as `/en/pre-summer-sale`, are temporary. Do not make one a
required source. A product can show an RRP and a lower current price, a percent
badge, `Sale`, or `TOP-DEAL`. Return only reductions that the site shows. Do not
estimate a discount when the original price or discount mark is absent.

## Filters

The `/en/bike/sale` HTML exposes these common filter groups:

- In stock
- Brand
- Minimum and maximum price
- Label: New, Sale, with test result, and Bike-Discount tip
- Colour
- Minimum rating from one through five

Filters are category-specific. A bike deal page also exposed frame shape, frame
size, drive system, motor, maximum assistance, torque, display, battery mounting,
battery capacity, frame material, and colour.

Current public links verify this query grammar:

```text
?p=1&order=standard&manufacturer=<32-hex-id>&properties=<32-hex-id>|<32-hex-id>
```

`manufacturer` and `properties` accept site IDs. Multiple property IDs use `|`.
Do not hard-code IDs from another category. Read the input name and value from
the target listing fixture. Preserve unknown provider filter pairs so help can
describe and pass them through only when they are safe URL query values.

The site also has older indexed links with `o`, `n`, and `s`. Treat these as
legacy aliases. Do not mix the legacy and current filter grammar in one request.

## Sort and pagination

The current listing page verifies these rules:

- Page one can use the base URL.
- Page two uses `?p=2`.
- The page number is one-based for new requests.
- `order=standard` is a verified current sort value.
- `n=48` is a verified page-size value on English listing links.

Older indexed pages also contain `p=0`. Do not emit page zero. It can be an old
migration alias. The only verified page size is 48. Advertise `[48]` until a new
working browser fixture proves other values. Do not claim that `limit=48` works.

Legacy examples that exist in public links are:

```http
GET /en/mountain-bike-parts?p=1&o=1&n=48
GET /en/bike/bike-parts/mountain-bike-parts/rear-shock/air-shocks?p=1&o=3&n=48
```

The labels for `o=1`, `o=3`, and `o=14` were not verified. Do not expose those
numbers as named sort modes. Capture each option value and label from the sort
control before it is added to provider help.

## Items and variants

Product pages use language-prefixed canonical slugs. One verified item is:

```text
URL: https://www.bike-discount.de/en/yamaha-500-wh-36v/13.6ah-frame-battery
Item no: 20166382
RRP text: 819,00 €
Current price text at capture: 299,99 €
Availability text: In stock, delivery time 1-3 Days
```

The same page had images, a description, product features, a manufacturer number,
an EAN, and a manufacturer block. Prices on the indexed sale page differed by
crawl time. Always use one response set for a result. Never combine a current
price from one response with an RRP or stock value from another response.

A canonical slug can contain `/`, as the verified Yamaha URL shows. Do not use
only the final path segment as the item ID. Accept the displayed numeric item
number or the complete provider URL. Resolve a numeric item number through search
when the site does not expose a direct numeric route.

Listing cards and product pages can show a variant group labelled `Größe`
(`Size`). Values can be simple sizes or compound equipment choices. A value can
be marked as currently unavailable. The displayed price can be `from` when
variant prices differ. Parse the exact label, value, availability, and selected
state. Internal variant IDs were not verified. Capture their input attributes
from the item fixture before exact `--variant` selection is implemented.

Exclude the `plus shipping costs` link and all shipping values. The item price is
only the displayed product price.

## Market and currency

The item HTML verifies these languages: German, English, French, Spanish, and
Italian. English URLs use `/en/`. The page has a shipping-country selector with
Germany and many other countries. The verified default view was Germany with EUR
and 19 percent VAT. The footer states that price can vary by delivery country.

No stable country cookie name or country-change request was verified. The fresh
session was challenged before a country change could be inspected. Start with
the configured default market `DE`, `en`, `EUR`. Return the actual currency from
the page. Do not convert it. For another country, require a captured working
session or return a market warning until the country request is verified.

The browser session did set a site cookie. Its value is not part of this note.
Cookie values must stay in the SQLite session store and must not enter logs,
fixtures, cache keys, errors, or provider data.

## Challenge behavior

A new Chrome session received `Attention Required! | Cloudflare` for `/en/`.
A direct category request received HTTP 403 with these properties:

```text
server: cloudflare
cache-control: private, max-age=0, no-store, no-cache, must-revalidate
content-type: text/html; charset=UTF-8
```

The response had a Cloudflare ray ID and no useful catalog content. Do not cache
this page as a successful response. Detect it by status and challenge title or
body markers. Use browser transport, then configured CDP transport. If manual
action is necessary, return `browser_challenge_required` unless interactive mode
is enabled. Do not make normal commands wait for a person.

`/en/llms.txt` remained accessible in the same environment. This does not prove
that catalog pages will be accessible.

## Fixture capture procedure

Capture fixtures only in an environment that can load the public catalog.
Throttle capture to one request each second and one browser page at a time.

1. Start a new isolated browser session for the `DE/en/EUR` market.
2. Load `/en/llms.txt`, `/en/brands`, one root category, one child category,
   `/en/bike/sale`, one search result, one simple item, and one variant item.
3. For search, submit the visible form. Record the final URL, form action, input
   name, response status, and redirect chain.
4. For each listing, capture the base page, page two, size 48, each sort option,
   one manufacturer filter, one property filter, and one price filter.
5. Record browser network requests. Save a JSON response only if it is a public
   catalog response and it contains the fields that the parser uses.
6. Save the raw final HTML before DOM mutation. Save DOM HTML only in a separate
   fixture when DOM extraction is required.
7. Remove `Set-Cookie`, cookie values, Cloudflare IDs, analytics identifiers,
   local-storage values, request timestamps, and unrelated tracking markup.
8. Keep the status, content type, final URL, market, and fixture capture date in
   a small metadata file. Keep displayed price text unchanged.
9. Confirm that each fixture has product names, canonical URLs, page metadata,
   filter values, and item numbers as applicable.
10. Run all normal provider tests with the network disabled. Put optional live
    checks behind an explicit build tag or environment switch.

Do not save a challenge page as a catalog fixture. A small, scrubbed challenge
fixture is acceptable only for a challenge-detection test.

## Implementation limits

The first provider can safely implement category roots, cached local category
search, the alphabetical brand index, cached local brand search, brand pages,
deal pages, one-based pages, size 48, HTML item parsing, displayed discounts, and
challenge detection from this evidence.

Search parameter selection, named sort modes other than `standard`, exact filter
IDs, country switching, JSON endpoints, JSON-LD coverage, and internal variant
IDs need a working browser fixture before they are declared stable.
