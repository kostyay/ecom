# Provider Author Guide

This guide is for a Go developer who writes an external `ecom` provider. You
only need these public packages:

```text
github.com/kostyay/ecom/provider
github.com/kostyay/ecom/provider/conformance
```

Do not import a package below `internal/`. The Core supplies transport, cache,
rate limits, retries, browser sessions, configuration loading, and output.

## 1. Create the provider module

Use one Go module for one website provider. A small module can have this
layout:

```text
example.com/ecom-tinyshop/
  go.mod
  provider.go
  provider_test.go
  testdata/
    search.json
```

Add the SDK module as a dependency:

```sh
go mod init example.com/ecom-tinyshop
go get github.com/kostyay/ecom
```

Pin a tested SDK release in `go.mod`. At registration, set `SDKAPIVersion` to
`provider.APIVersion`. The Core rejects a provider that has a different API
version. A provider update can use a new module major version if the Go API is
not source-compatible.

## 2. Register the provider

A provider module registers one implementation in `init`:

```go
func init() {
	provider.MustRegister(provider.Registration{
		Name:           "tiny-shop",
		SDKAPIVersion:  provider.APIVersion,
		Implementation: implementation{},
		Capabilities:   []provider.CapabilityName{provider.CapabilitySearch},
	})
}
```

The provider name is a stable command and output identifier. It must start
with a lowercase letter. It can then contain lowercase letters, digits, and
single hyphens. Its maximum length is 63 characters.

`MustRegister` stops program startup if registration is not valid. This is
the correct action for an invalid compiled distribution. `Register` returns an
error and is useful in tests or in a custom registry. Registration detects:

- An invalid name or a nil implementation.
- An SDK API version mismatch.
- A duplicate provider name or capability.
- An unknown capability.
- A declared capability without its required Go interface.
- Variant selection without the item capability.

A CLI distribution includes the provider with one blank import:

```go
import _ "example.com/ecom-tinyshop"
```

This import runs the provider `init` function. It includes that one vendor in
the binary. It does not load code at run time. A new or removed provider needs
a new CLI build.

## 3. Declare only implemented capabilities

Every implementation must implement `provider.HelpProvider`. Declare each
additional capability only when the implementation has the matching method:

| Capability | Required interface | Result |
| --- | --- | --- |
| `search` | `SearchProvider` | `ProductPage` |
| `categories` | `CategoryListProvider` | `CategoryPage` |
| `category_search` | `CategorySearchProvider` | `CategoryPage` |
| `category_items` | `CategoryItemsProvider` | `ProductPage` |
| `brands` | `BrandListProvider` | `BrandPage` |
| `brand_search` | `BrandSearchProvider` | `BrandPage` |
| `brand_items` | `BrandItemsProvider` | `ProductPage` |
| `deals` | `DealsProvider` | `DealPage` |
| `filters` | `FiltersProvider` | `FiltersResult` |
| `item` | `ItemProvider` | `ItemResult` |
| `variant_selection` | `ItemProvider`, with `item` also declared | `ItemResult` |

The registry facade returns `capability_unavailable` for an operation that is
not declared. Do not add empty operation methods or declare planned work.

## 4. Use the common data model

Return provider IDs and canonical website URLs. Use `ProductSummary` for list
results and `ItemDetail` for a full item. A full item must have
`DetailLevelFull`. A listing item normally has `DetailLevelSummary`.

Use `Money` for a displayed price:

```go
provider.Money{
	Amount:   "79.95",
	Currency: "EUR",
	Display:  "€79.95",
}
```

`Amount` is a non-negative decimal string. Do not use a binary floating-point
number for money. Preserve the website price text in `Display`. Return the
item price that the website shows. Do not add shipping or optional fees. Do
not convert currency. If the website cannot show the requested currency,
return its actual currency and add a `currency_unavailable` warning.

Do not estimate a discount. A `Deal` must contain an original price, a
discount amount, or a discount percentage that the website shows. Use
`PriceRange` or variant prices when visible variants have different prices.
An item request without variant selections returns all visible variants. An
invalid selection returns `variant_not_found` and tells the caller how to find
valid choices.

Use only common fields when they have the documented meaning. Put website
data that has no common field in `provider.Data`. Each map key is a namespace,
usually the provider name, and each value must be valid JSON:

```go
provider.Data{
	"tiny-shop": json.RawMessage(`{"catalog_section":"outlet"}`),
}
```

Missing information stays absent or null. Do not invent a value.

## 5. Request resources through the Core

Each operation request embeds `provider.Request`. Its `Resources` field is the
request-scoped `provider.ResourceService`. Use it for each HTTP page, rendered
browser page, or CDP page. Do not create an HTTP client, browser, cache, retry
loop, rate limiter, or cookie store in the provider.

Copy the operation policies into every `ResourceRequest`:

```go
response, err := request.Resources.Fetch(ctx, provider.ResourceRequest{
	Method:      "GET",
	URL:         "https://shop.example/products",
	Query:       []provider.RequestValue{{Name: "q", Values: []string{request.Query}}},
	Market:      request.Market,
	Cache:       request.Cache,
	Interactive: request.Interactive,
	Transport: provider.TransportPolicy{
		Preferred: []provider.TransportMode{
			provider.TransportHTTP,
			provider.TransportBrowser,
		},
	},
})
```

Use the call context in `Fetch`. Stop when the context is canceled. Inspect the
status, final URL, body or page snapshot, cache metadata, and safe transport
attempts. A browser response can put HTML in `response.Page.HTML`. Use
declarative `DOMExtraction` values when you need rendered text, HTML, or an
attribute. The provider never gets a live browser handle or unrestricted
script execution.

`Transport.Required` selects one transport. `Transport.Preferred` is the full
ordered list of allowed transports. The Core does not append a missing mode.
If both fields are empty, the Core uses its normal sequence. Use only the modes
that the provider can parse and that `Help` documents.

The Core caches successful raw resources, not normalized provider results.
It applies the command TTL, refresh, and stale-on-error policy. It also applies
rate limits, retry limits, response size limits, and browser session policy.
One operation can make more than one resource request. The request-scoped
service collects safe cache and attempt metadata for the command result.

### Sensitive request values

Do not put a credential, token, session ID, or personal value in a URL. Put a
sensitive query or header value in `RequestValue` and set `Sensitive: true`.
Set `RequestBody.Sensitive` for a sensitive body. The Core then omits that
value from cache keys, logs, and public errors.

If a sensitive value changes the response, set `CachePartition` to a stable,
non-secret identifier for the account or session. Different response scopes
must have different partitions. Never put a credential in the partition. Do
not put secrets or personal data in errors, warnings, `provider_data`, fixture
files, or safe header fields.

## 6. Validate configuration before network access

Implement `provider.ConfigValidator` when the provider has settings. The
method receives the provider block from global configuration. It must accept a
nil or empty map as defaults. Reject unknown keys, wrong types, out-of-range
values, unsafe URLs, and unsupported pricing or market choices with a clear
error. Use `invalid_provider_config` when a stable command error is useful.

Validate settings and `request.Pricing` before the first resource request. If
the provider cannot include shipping, reject `IncludeShipping: true`. Do not
silently ignore it. Also validate identifiers, filters, sorts, pages, page
sizes, variants, and provider-owned URLs before resource access when possible.

## 7. Make Help complete and offline

`Help` is the discovery contract for people and agents. It must be
deterministic and work without a `ResourceService` or network access. Its name
and supported capability flags must agree with registration.

Document these provider details:

- Search syntax and examples.
- Supported and unsupported capabilities.
- Filter keys, types, repeatability, values, examples, and applicable
  capabilities.
- Sort values and applicable capabilities.
- Pagination mode, first page, default size, supported sizes, and totals.
- Market and currency limits.
- Authentication, browser, CDP, and interactive requirements.
- Transport use and known access problems.
- Website-specific restrictions and safe `provider_data`.

Call `Help.Validate` in the provider test. A provider with the `filters`
capability can return context-sensitive definitions from `Filters`. The result
must stay consistent with the main help definitions. The provider must also
validate incoming filter values because callers can use the public Go API
without the CLI validation. Provider wire names stay private. A common filter
key can map to any safe website query shape.

For page-number pagination, validate supported sizes and return `PageInfo`
with the actual number and size. Set total fields only when the website gives
reliable totals. For cursor behavior, explain the provider-specific value in
help and put non-common page data in namespaced `provider_data`.

## 8. Return stable warnings and errors

Use `provider.NewError` with a stable `ErrorCode`. The public message must be
safe and short. The wrapped cause is for diagnostics and does not appear in
JSON. Preserve context cancellation errors.

Return useful partial results when at least one item was parsed. Add a
`partial_parsing` warning with `FoundCount`, `ParsedCount`, and a safe item ID
or URL when possible. If no useful result or valid page metadata is available,
return an error. Use `currency_unavailable` when the actual displayed currency
differs. Use `search_semantics_unverified` when the site can ignore or change
the meaning of a search request.

Validate all returned common values. `Money`, `PriceRange`, products,
variants, categories, brands, deals, help, page data, warnings, and all JSON in
`provider_data` are checked by the conformance suite.

## 9. Complete small provider example

This example is the complete `provider.go` file for a search-only provider. It
uses a public JSON endpoint through the Core. The documentation test compiles
this exact marked block.

<!-- provider-example:start -->
```go
// Package tinyshop implements the Tiny Shop provider.
// Import this package for its registration side effect.
package tinyshop

import (
	"context"
	json "encoding/json/v2"
	"fmt"
	"net/http"
	"strings"

	"github.com/kostyay/ecom/provider"
)

const Name = "tiny-shop"

type implementation struct{}

func init() {
	provider.MustRegister(registration())
}

func registration() provider.Registration {
	return provider.Registration{
		Name:           Name,
		SDKAPIVersion:  provider.APIVersion,
		Implementation: implementation{},
		Capabilities:   []provider.CapabilityName{provider.CapabilitySearch},
	}
}

func (implementation) ValidateConfig(configuration map[string]any) error {
	for key := range configuration {
		return provider.NewError(
			provider.ErrorCodeInvalidProviderConfig,
			fmt.Sprintf("tiny-shop does not support setting %q", key),
			nil,
		)
	}
	return nil
}

func (implementation) Help(context.Context, provider.HelpRequest) (provider.HelpResult, error) {
	help := provider.Help{
		Name:        Name,
		DisplayName: "Tiny Shop",
		Description: "Search the Tiny Shop product catalog.",
		Capabilities: []provider.CapabilityHelp{
			{Name: provider.CapabilitySearch, Supported: true},
		},
		Search: &provider.SearchHelp{
			QueryRequired: true,
			Syntax:        "plain product text",
			Examples:      []string{"helmet"},
		},
		Pagination: &provider.PaginationHelp{
			Mode:               provider.PaginationPageNumber,
			FirstPage:          1,
			DefaultPageSize:    20,
			SupportedPageSizes: []int{20},
		},
		Access: &provider.AccessRequirements{
			Authentication: provider.AuthenticationNone,
			Browser:        provider.BrowserNone,
		},
		Transport: []provider.TransportNote{
			{Mode: provider.TransportHTTP, UseWhen: "Get the public catalog JSON."},
		},
	}
	if err := help.Validate(); err != nil {
		return provider.HelpResult{}, err
	}
	return provider.HelpResult{Help: help}, nil
}

type searchDocument struct {
	Products []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		URL      string `json:"url"`
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
		Display  string `json:"display"`
	} `json:"products"`
	Page       int  `json:"page,omitzero"`
	HasNext    bool `json:"has_next,omitzero"`
}

func (implementation) Search(ctx context.Context, request provider.SearchRequest) (provider.ProductPage, error) {
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return provider.ProductPage{}, provider.NewError(
			provider.ErrorCodeInvalidResourceRequest, "a search query is required", nil,
		)
	}
	if request.Pricing.IncludeShipping {
		return provider.ProductPage{}, provider.NewError(
			provider.ErrorCodeInvalidProviderConfig,
			"tiny-shop cannot include shipping in prices", nil,
		)
	}
	if request.Resources == nil {
		return provider.ProductPage{}, provider.NewError(
			provider.ErrorCodeInvalidResourceRequest, "the resource service is required", nil,
		)
	}
	page := request.Page.Number
	if page == 0 {
		page = 1
	}
	if page < 1 || (request.Page.Size != 0 && request.Page.Size != 20) {
		return provider.ProductPage{}, provider.NewError(
			provider.ErrorCodeInvalidResourceRequest, "the page request is not supported", nil,
		)
	}

	response, err := request.Resources.Fetch(ctx, provider.ResourceRequest{
		Method: http.MethodGet,
		URL:    "https://shop.example/api/products",
		Query: []provider.RequestValue{
			{Name: "q", Values: []string{query}},
			{Name: "page", Values: []string{fmt.Sprint(page)}},
			{Name: "size", Values: []string{"20"}},
		},
		Market:      request.Market,
		Cache:       request.Cache,
		Interactive: request.Interactive,
		Transport: provider.TransportPolicy{
			Required: provider.TransportHTTP,
		},
	})
	if err != nil {
		return provider.ProductPage{}, err
	}
	if response.StatusCode != http.StatusOK {
		return provider.ProductPage{}, provider.NewError(
			provider.ErrorCodeHTTPFailure, "tiny-shop returned an unsuccessful response", nil,
		)
	}

	var document searchDocument
	if err := json.Unmarshal(response.Body, &document); err != nil {
		return provider.ProductPage{}, provider.NewError(
			provider.ErrorCodeInvalidProviderResult, "tiny-shop returned invalid product data", err,
		)
	}
	items := make([]provider.ProductSummary, 0, len(document.Products))
	for _, product := range document.Products {
		item := provider.ProductSummary{
			ID:   product.ID,
			Name: product.Name,
			URL:  product.URL,
			Price: &provider.Money{
				Amount: product.Amount, Currency: product.Currency, Display: product.Display,
			},
			RetrievedAt: response.RetrievedAt,
			DetailLevel: provider.DetailLevelSummary,
		}
		if err := item.Validate(); err != nil {
			return provider.ProductPage{}, provider.NewError(
				provider.ErrorCodeInvalidProviderResult, "tiny-shop returned an invalid product", err,
			)
		}
		items = append(items, item)
	}
	if document.Page > 0 {
		page = document.Page
	}
	return provider.ProductPage{
		Items: items,
		Page: provider.PageInfo{
			Number: page, Size: 20, HasNext: &document.HasNext,
		},
	}, nil
}

var (
	_ provider.HelpProvider    = implementation{}
	_ provider.ConfigValidator = implementation{}
	_ provider.SearchProvider  = implementation{}
)
```
<!-- provider-example:end -->

The example returns an error for one invalid product. A production parser can
continue after an invalid entry if it returns at least one valid item and a
`partial_parsing` warning.

## 10. Test with offline fixtures

Keep small, sanitized resource bodies in `testdata`. Do not store cookies,
tokens, account data, full browser profiles, or unnecessary page content.
Record the source URL, capture time, market, and sanitization notes in a
fixture manifest. Add parser tests for normal, empty, partial, malformed,
unavailable, pagination, discount, and variant cases that apply to the site.

Use `provider/conformance.FixtureService` to give ordered offline responses and
to check each `ResourceRequest`. A conformance test has this form:

```go
func TestProviderConformance(t *testing.T) {
	resources := conformance.NewFixtureService(conformance.ResourceFixture{
		Response: provider.ResourceResponse{
			Body:       readFixture(t, "search.json"),
			StatusCode: http.StatusOK,
		},
		CheckRequest: func(request provider.ResourceRequest) error {
			if request.URL != "https://shop.example/api/products" {
				return fmt.Errorf("unexpected URL %q", request.URL)
			}
			return nil
		},
	})

	conformance.Run(t, conformance.Suite{
		Registration: registration(),
		Resources:    resources,
		Cases: []conformance.OperationCase{{
			Name:       "search fixture",
			Capability: provider.CapabilitySearch,
			Invoke: func(ctx context.Context, registered provider.Provider) (any, error) {
				return registered.Search(ctx, provider.SearchRequest{Resources: resources, Query: "helmet"})
			},
		}},
	})
}
```

Supply at least one operation case for each declared capability. Supply a
variant-selection case when that capability is declared. The suite verifies
registration, help, unsupported-operation errors, result types, common data,
page metadata, warning fields, filter consistency, namespaced JSON, fixture
use, and fixture consumption. It never uses the network.

Also test these provider responsibilities directly:

- Configuration accepts defaults and rejects each invalid form.
- Help works without resources and stays consistent with registration.
- Operation policy reaches every `ResourceRequest`.
- Sensitive values have the correct flag and cache partition.
- Filters and sorts map to the correct website values.
- Only supported page sizes and page numbers are accepted.
- Item URLs stay on the provider host and use the selected market.
- Context cancellation stops resource work.
- Parser failures do not expose source data or secrets in public messages.

Run these checks before a distribution imports the provider:

```sh
go test ./...
go test -race ./...
go vet ./...
```
