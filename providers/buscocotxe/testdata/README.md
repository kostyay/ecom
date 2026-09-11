# BuscoCotxe search fixtures

Run the fixture checks from the repository root:

```sh
python3 providers/buscocotxe/testdata/check.py
go test ./providers/buscocotxe
```

These files are small extracts of saved HTML. The manifest records sources,
capture dates, hashes, request pages, and expected contents. The Go parser tests
consume all nine files. Search conformance tests use the search and regression
fixtures. The Python check tests fixture integrity; the Go tests test parsing
and the Provider contract.

| Files | Purpose |
| --- | --- |
| `search_page_1.html`, `search_page_2.html` | Thirty cards per page; distinct IDs; stated prices and prices on request. |
| `search_last.html` | Seven sold cars on page 42 of 42. |
| `search_empty.html` | Explicit empty-result message without result totals. |
| `search_out_of_range.html` | Request page 43; HTTP 200; unchanged URL; body is page 42. Return a page-mismatch error in the Provider. |
| `carfinder_page_1.html`, `carfinder_page_2.html` | Thirty cards per page from the existing Carfinder filter fixtures. |
| `regressions.html` | Synthetic decimal price, price on request with a description price, monthly finance payment, malformed ID, duplicate ID, and dealer banner. |
| `unexpected.html` | Synthetic error HTML that must not become an empty search result. |

The September search responses were saved during the plan research. Their
capture day is known; exact capture times were not recorded. The out-of-range
response has a recorded UTC capture time. The two Carfinder fixtures come from
`/data/personal/carfinder/tests/fixtures` at commit `4e39366`. Their original
capture times and response status are unknown. The source commit date is not a
capture date.

The extracts preserve card structure, titles, price overlays, distance/year
summaries, stock text, body type, public car and image URLs, and pagination.
They remove scripts, forms, account data, dealer logos, seller IDs, favorites,
descriptions, contact details, visits, timestamps, and unrelated page content.
The public `kmk-seguiment` and `kmk-seguiment-iditem` attributes remain because
they identify listing cards. Synthetic descriptions contain only test text.

The synthetic file uses `data-fixture-placeholder="true"`. Its values do not
describe real cars. Of its five listing cards, three have distinct valid IDs,
one has an invalid ID, and one repeats a valid ID. The dealer banner is outside
the listing cards. Parser tests must check the expected prices and partial
result warning from the manifest.

The parser excludes valid duplicate IDs from warning counts. The synthetic
regression page therefore reports four found entries and three parsed entries.
Missing page numbers in an explicit empty result remain zero in the parser.
Search replaces zero with the requested page number to meet the SDK contract.
The total page count remains absent, as stated in offline Help.

The last-page and out-of-range extracts are identical after sanitization.
Their separate manifest entries preserve the different request pages. The
Provider must check requested and returned page numbers before returning data.
