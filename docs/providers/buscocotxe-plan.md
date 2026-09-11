# BuscoCotxe car search Provider plan

Status: search epic `eco-ed12` and its three tasks completed on 2026-09-11.
CLI task `eco-ac2a` completed on 2026-09-11. The next task is `eco-9542`:
release checks and a small live search check.

## Goal and scope

Add the compiled Provider `buscocotxe` in `providers/buscocotxe`.
The first version supports text search for cars, page numbers, and offline
Provider help. It returns Product summaries through the existing CLI.

Use the public Provider SDK and Core resource service. Use the existing
`golang.org/x/net/html` dependency to parse HTML. Follow the current Provider
package layout in this repository. No Core boundary change is required.

Planned commands:

```sh
ecom provider help buscocotxe
ecom search "BMW X3" --provider buscocotxe --page 1 --page-size 30
```

Declare only the `search` Capability. The first version does not include item
details, filters, sort selection, brands, categories, deals, or variants.
Reject unsupported inputs before network access. Add filters in separate work
after the website request rules are clear.

## Reuse from Carfinder

The existing project is available at `/data/personal/carfinder`, also accessible
as `~/personal/carfinder`. The requested `~/work/carfinder` path was not present.
The relevant parser and fixture history was last changed in commit `4e39366`
on 2026-04-17. Use these files as implementation references:

| Carfinder file | Reuse in this Provider |
| --- | --- |
| `src/carfinder/scraper/index_parser.py` | Card selectors, item IDs, Catalan result counts, page numbers, distance, year, and body type. Port the relevant parsing rules to Go. |
| `tests/test_index_parser.py` | Expected values for 30 cards, distinct pages, missing prices, IDs, distance, year, and body type. |
| `tests/fixtures/index_page_1.html`, `index_page_2.html` | Existing listing HTML. Copy only sanitized relevant parts and record their source. These are filter pages, so retain search-page coverage too. |
| `src/carfinder/scraper/detail_parser.py`, `tests/test_detail_parser.py` | Reference for a later item Capability: structured specification labels, sold state, and month/year dates. |
| `tests/fixtures/detail_600010.html`, `detail_600011.html`, `detail_582483.html` | Existing detail examples for later work. They are not required by the search Provider. |
| `src/carfinder/scraper/runner.py`, `src/carfinder/tasks.py` | Existing use of `/ca/filter?ordreFiltres[]=dataDesc&pn={page}`. This does not establish combined text and filter search. |

On 2026-09-11, all 15 existing index and detail parser tests passed in an
isolated Python environment with `selectolax` and `pytest`. The test command
disabled repository fixtures that load the database application. No database or
website request was needed. The old index parser also read the four search
responses already saved during this plan: 30, 30, seven, and zero cards for the
first, second, last, and empty pages. This confirms useful overlap in page
structure; it does not prove that every old parsing rule is correct.

Correct these confirmed limits when porting the parser:

- `_parse_price` reads the whole card. A price-on-request card with
  `25.000 €` in its description returns a false item price.
- The price expression reads `19.999,50 €` as `50`. Use exact decimal-string
  conversion for the displayed price.
- Missing result metadata defaults to zero results and page 1 of 1. Even
  `Service unavailable` HTML becomes an empty result. Require a recognized
  result page or explicit empty-result state.
- One invalid card ID raises an error for the entire page. Add the planned
  partial-result handling and validate both the item ID and URL.

The old index model does not include the car image or an explicit availability
field. Keep those additions in the Go parser task. Carfinder derives brand and
model from titles; retain the plan's rule to avoid inferred common fields.
Do not import the Python runtime, database, scheduler, HTTP client, cache, or
whole-catalog loop into ecom. Reuse the parsing rules and test cases through the
existing Go SDK and Core services.

## Verified website behavior

Research used a small set of public HTTP GET requests without an account.
The following observations are from 2026-09-11. Counts can change.

| Source | Observed result | Design effect |
| --- | --- | --- |
| [Home page](https://www.buscocotxe.ad/) | HTTP 200 with HTML forms and listing data. | Start with HTTP through the Core. |
| [BMW search](https://www.buscocotxe.ad/ca/search?search=BMW) | HTTP 200; 30 car cards; 1,237 results; page 1 of 42. | Use `/ca/search` with the `search` parameter. |
| [Second page](https://www.buscocotxe.ad/ca/search?search=BMW&pn=2) | 30 different car cards; page 2 of 42. | Map the SDK page number to `pn`. |
| [Last page](https://www.buscocotxe.ad/ca/search?search=BMW&pn=42) | Seven car cards; page 42 of 42; sold cars were present. | Return the actual page metadata. Do not assume that all results are available. |
| [Page beyond the last page](https://www.buscocotxe.ad/ca/search?search=BMW&pn=43) | HTTP 200 with an unchanged URL, but the body returned page 42 and its seven cars. | Reject a mismatch between requested and returned page numbers. |
| [Empty search](https://www.buscocotxe.ad/ca/search?search=zzzxxyy987654321) | Explicit empty-result message and no car cards. | Distinguish an empty result from a parser failure. |
| [BMW price filter](https://www.buscocotxe.ad/ca/filter?marca%5B%5D=1&preuMin=0&preuMax=20000) | Listed numeric prices were within the bound. Cars without a stated price were also present. | A native price filter does not prove a price for every returned car. |
| [Price parameters on text search](https://www.buscocotxe.ad/ca/search?search=BMW&preuMin=0&preuMax=20000) | Results still included prices above EUR 20,000. | Do not send filters to the text search path. |
| [Text parameter on filter path](https://www.buscocotxe.ad/ca/filter?search=BMW&marca%5B%5D=1&preuMin=0&preuMax=20000) | No car cards, although the separate BMW search and filter returned cars. | Combined text and filter behavior is unverified. |

No catalog JSON endpoint was verified. HTML provides the data needed for this
scope. Browser transport and authentication are not required by these probes.
Search results can match description text as well as car names. Preserve the
website result order and semantics; do not add local text filtering.

## Request and result contract

- Use Core-owned HTTP transport with `GET /ca/search`, `search`, and `pn`.
  Send query values through `provider.RequestValue`.
- Use page 1 by default. Support page size 30 only. One command retrieves one
  listing page. Do not fetch every result page or each car detail page.
- Pass cache, market, interactive policy, and context to every resource request.
  Let the Core apply rate limits, retries, response limits, and cache policy.
- Use the Andorra catalog in Catalan. Document the actual catalog and language
  when global market defaults differ. Return EUR prices. Add
  `currency_unavailable` when another currency was requested. Do not claim
  translation or currency conversion.
- Accept an empty Provider configuration as defaults. Reject unknown settings,
  empty queries, invalid pages, unsupported sizes, filters, sorts, and shipping
  inclusion before resource access.
- Keep Help deterministic and available without resources. State the search
  syntax, page size, market limits, transport, and unsupported operations.
- For an explicit empty result, Search sets the page number to the requested
  page and leaves total pages absent. The parser's zero page number marks
  missing website metadata; the SDK requires a positive search page number.
  Help states this rule and promises total pages only for nonempty results.

Parse each listing link with `kmk-seguiment="llistat"` and a valid
`/ca/cotxe/<numeric-id>/<slug>` path. Check the website host and keep the public
listing URL. Do not use the listing page's canonical tag as a car URL.

| Website value | Product summary value |
| --- | --- |
| Numeric ID in the car URL | `ID` as a string |
| Car listing link | `URL` |
| Card heading in `.box-titol h2` | `Name` |
| Displayed item price in the card overlay | `Price`, with a decimal string and `EUR` |
| Primary card background image | `ImageURL`, after URL validation |
| Visible year and distance in the card summary | `Attributes`, with units and source text preserved |
| Explicit stock or sold text | `StockText` and a supported `Availability` value |
| Resource retrieval time | `RetrievedAt` |
| Listing scope | `DetailLevelSummary` |

Use card boundaries to keep adjacent prices separate. Do not use finance
payments or numbers in descriptions as item prices. For a price on request,
leave `Price` absent. Preserve any useful unmatched site fields under
`provider_data.buscocotxe`. Do not infer a brand from the title, parse vehicle
specifications from free text, or infer availability from a missing label.

Deduplicate repeated car IDs on the same page. Exclude dealer banners and
non-car links. Parse the displayed result count and page count, including
thousands separators. Do not calculate totals from the number of parsed cards.
Set `HasNext` from valid page metadata or navigation. The website can return
the last page for a higher requested page without a redirect. Return a safe
page-mismatch error in this case. Do not label repeated cars with the requested
page number or report an empty result.

Return `partial_parsing` with found and parsed counts when some car cards fail.
Return an error for an unexpected page with no valid results or valid empty
state. Preserve Core access errors and context cancellation. Keep error text
free of raw page content.

## Work sequence and checks

1. Reuse sanitized parts of the two Carfinder index fixtures and the search
   responses already saved during research. Cover normal, second, last, and
   empty pages. Record source URLs, capture time when known, and removed content
   in a manifest. Do not invent capture times for old fixtures. Include
   price-on-request and sold cases. Completed in `eco-89da`: nine fixtures,
   a manifest, and an offline check are available in
   [testdata](../../providers/buscocotxe/testdata/README.md). The out-of-range
   response confirms that the site returns the last page.
2. Port the relevant Carfinder card and page parsing rules to Go. Add focused
   offline checks for the confirmed old-parser limits, money, missing fields,
   duplicate IDs, banners, malformed pages, and partial results. Completed in
   `eco-7027`: `ParseListing` consumes the saved HTML and returns Product
   summaries, actual page metadata, and partial-result warnings. The Go tests
   use all nine fixtures and additional invalid-input cases.
3. Implement registration, offline Help, validation, and Search through the
   Core. Run SDK conformance and request-policy checks with FixtureService.
   Completed in `eco-1c40`: one Core-owned HTTP request per search; input,
   redirect, and returned-page checks; policy forwarding; cancellation; and
   EUR currency warnings. The conformance tests cover normal, second, last,
   empty, partial, out-of-range, and unexpected-page results.
4. Add the distribution import and user documentation. Check provider selection,
   help, JSON, table, JSONPath, pagination, and structured errors offline.
   Completed in `eco-ac2a`: the compiled CLI includes BuscoCotxe, the user docs
   state its limits, and a fixture-backed CLI test covers each output path and
   rejects unsupported filters before a resource request.
5. Run `make quality`. Make a small live CLI search check and record the result
   separately from offline tests. If access fails, record that limit clearly.

Existing changes in `main.go`, `providers/bike24`, and existing tickets are
outside this work. Read the current files again before integration.

Completion requires all planned tasks to pass their checks. The Provider must
be selectable in the built CLI and return valid Product summaries. Normal tests
must not depend on the website. Ticket IDs and dependencies are recorded below.

## Tickets

| Epic | Child tasks, in dependency order |
| --- | --- |
| [eco-ed12: Car search Provider](../../.ktickets/eco-ed12.md) | [eco-89da: Fixtures](../../.ktickets/eco-89da.md) → [eco-7027: Parser](../../.ktickets/eco-7027.md) → [eco-1c40: Search and Help](../../.ktickets/eco-1c40.md) |
| [eco-6868: CLI integration and release checks](../../.ktickets/eco-6868.md) | [eco-ac2a: CLI and guide](../../.ktickets/eco-ac2a.md) → [eco-9542: Release checks](../../.ktickets/eco-9542.md) |

The second epic depends on the first epic. The CLI integration task also depends
on the Search and Help task. The search epic and its three tasks are closed.
The CLI integration epic remains open. Its CLI and guide task is complete, and
its release-check task is ready. All tickets have the triage value
`ready-for-agent`. The next task is `eco-9542`.
