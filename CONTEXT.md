# Domain Context

## Purpose

`ecom` is a machine-readable command-line platform for product discovery on one commerce website at a time. An agent can use separate provider commands and compare their results. The CLI does not match products or compare providers.

## Terms

- **Core**: The provider-neutral CLI runtime. It owns configuration, output, transport, browser use, caching, rate limits, retries, and diagnostics.
- **Provider**: A compiled Go module that contains the navigation, request, and parsing logic for one commerce website.
- **Provider SDK**: The versioned public Go API through which a provider registers and uses core services.
- **Capability**: An operation that a provider supports, such as product search, category navigation, brand navigation, deals, filters, or item details.
- **Market**: The configured country, language, and preferred currency used for provider requests.
- **Product summary**: The common product fields returned by search, category, brand, or deal listings.
- **Item detail**: The full information for one provider item, including visible variants.
- **Variant**: A purchasable product choice identified by attributes such as size or color.
- **Deal**: A product for which the provider shows a reduced price or discount. The core does not estimate whether a normal price is a good deal.
- **Displayed price**: The item price shown by the provider. It excludes shipping and optional fees.
- **Provider item ID**: An opaque identifier assigned by a provider.
- **Raw response**: Successful HTTP content or browser page content stored before provider parsing.
- **Session state**: Portable browser state, such as cookies and local storage, stored separately from raw-response cache entries.

## Boundaries

- A command operates on one provider.
- The CLI does not calculate shipping, match products across providers, compare prices, or convert currencies.
- The CLI returns the site's actual currency if the configured currency is not available.
- The core does not solve CAPTCHAs or rotate proxies by default.
- Providers do not create independent HTTP clients, browser processes, caches, or rate limiters.
