# Tradeinn search fixtures

Source: public POST `https://www.tradeinn.com/listado.php`, captured on
2026-09-19 at 17:22:51 UTC with `action=buscador_google`, `id_tienda=0`,
`idioma=eng`, and the website's public anonymous visitor ID. HTTP status: 200.
The response was JSON with a `text/html` content type.

| File | Query and page | Expected result |
| --- | --- | --- |
| `search.json` | `powertube`, first page | 45 products, total 48, next token |
| `last.json` | `powertube`, second page | 3 products, total 48, no next token |
| `expanded.json` | `zzzxxyy987654321`, first page | 9 related products, corrected query, expansion flag |

The fixtures retain product IDs, titles, brands, catalog availability, shop
IDs, and the country prices for Andorra (4) and Germany (75). Browser checks
confirmed English labels and EUR prices for both markets. The first product
cost 675.99 EUR in Andorra and 739.99 EUR in Germany.

Sanitization removes other prices, facets, project resource names, attribution
tokens, and internal product URL query values. URLs have the synthetic query
`image_created=1` to test query removal. The next token is replaced with
`fixture-page-2`. No account data or personal visitor ID is present.

The Go tests make malformed and duplicate entries from these fixtures. The
explicit empty JSON case is synthetic; the live unmatched query expanded to
related products instead of returning an empty list.

Run `go test ./providers/tradeinn` from the repository root. See
[the research note](../../../docs/providers/tradeinn-research.md) for the request
contract and its limits.
