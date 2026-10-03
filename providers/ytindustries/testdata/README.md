# YT Industries fixtures

Captured anonymously over HTTP on 2026-10-03, DE/en/EUR.

| File | Source |
| --- | --- |
| `home.html` | `GET https://www.yt-industries.com/` |
| `context.json` | `GET https://www.yt-industries.com/store-api/context` |
| `search.json` | `POST https://www.yt-industries.com/store-api/search`, search `capra`, page 1 |
| `second.json` | Same search, page 2 |
| `last.json` | Same search, page 3 |
| `empty.json` | Same endpoint, search `zzzxxyy987654321`, page 1 |

Search JSON uses limit 24, total-count-mode 1, and associations for seoUrls and
cover.media. Requests use the storefront's published European sw-access-key.
The captured homepage is reduced to public configuration with a **synthetic**
key. Context contains only currency, language, and country, with no token or
customer fields. Search responses retain product identifiers, name,
manufacturer, cover URL, SEO URLs, calculated prices, availability, and paging
metadata. Other fields were removed; values were not invented. Two first-page
products genuinely lack SEO URLs. JSON numbers remain decimal source values.

See [research](../../../docs/providers/manufacturers-research.md). Tests read
fixtures without modifying them and require no live network.
