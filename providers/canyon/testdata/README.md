# Canyon fixtures

Captured anonymously over HTTP on 2026-10-03, ES/en/EUR.

Base endpoint: `https://www.canyon.com/en-es/search/`.

| File | Parameters |
| --- | --- |
| `search.html` | `q=spectral&start=0&sz=24` (default selected bikes tab) |
| `second.html` | `q=spectral&searchType=bikes&start=24&sz=24&pn=1&searchredirect=false` |
| `last.html` | `q=spectral&searchType=bikes&start=48&sz=24&pn=2&searchredirect=false` |
| `empty.html` | `q=zzzxxyy987654321&searchType=bikes&start=0&sz=24&searchredirect=false` |

Retained selected-tab metadata, product grid, footer/next link, and explicit
empty-state marker. Removed scripts, analytics, unused data attributes, SVGs,
responsive image alternatives, and unrelated markup; normalized whitespace.
No cookies or account data are stored. Prices and product counts are not
changed: 24, 24, 2, and 0 bikes respectively, with 50 total bikes for spectral.
Cards retain finance text so tests distinguish it from item prices. Captures
include outlet bikes, original prices, `From` prices, and out-of-stock labels.

See [research](../../../docs/providers/manufacturers-research.md). Browser/CDP
fallback is supported by policy but these captures and live smoke tests used
HTTP only.
