# Propain fixtures

Captured anonymously over HTTP on 2026-10-03, Germany/English/EUR.

| File | Source |
| --- | --- |
| `search.html` | `https://www.propain-bikes.com/en/?s=tyee&post_type=product&wcpbc-manual-country=DE` |
| `second.html` | `https://www.propain-bikes.com/en/page/2/?s=tyee&post_type=product&wcpbc-manual-country=DE` |
| `last.html` | Same parameters, `/en/page/4/` |
| `empty.html` | `/en/?s=zzzxxyy987654321&post_type=product&wcpbc-manual-country=DE` |

Retained product grid, pagination, country input where present, and explicit
empty-state marker. Removed scripts, analytics, unused data attributes, SVGs,
responsive image alternatives, and unrelated markup; normalized whitespace.
No cookies, nonces, customer data, or account state are stored. Prices and
product counts are not changed: 10, 10, 9, and 0 entries respectively. Captures
include original prices, starting prices, configured bikes, parts, and stock
classes. Parser regression tests construct additional malformed/range cases
in memory, never rewriting these captures.

The WooCommerce Store API was investigated but not used, because its catalog
and prices differ from the storefront. See
[research](../../../docs/providers/manufacturers-research.md).
