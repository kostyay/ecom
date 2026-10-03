// Package shoputil contains small parsing and request helpers shared by manufacturer Providers.
package shoputil

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/kostyay/ecom/provider"
)

// ValidateSearch validates the common restrictions of a fixed-market search Provider.
func ValidateSearch(ctx context.Context, r provider.SearchRequest, country string, size int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	page := cmp.Or(r.Page.Number, 1)
	if strings.TrimSpace(r.Query) == "" || !utf8.ValidString(r.Query) || len(r.Filters) != 0 || r.Sort != nil || page < 1 || page > 1000 || r.Page.Size != 0 && r.Page.Size != size {
		return 0, provider.NewError(provider.ErrorCodeInvalidFilter, "a search query, pages 1 to 1000, and the documented page size are required; filters and sorts are unsupported", nil)
	}
	if r.Pricing.IncludeShipping {
		return 0, provider.NewError(provider.ErrorCodeInvalidProviderConfig, "this Provider cannot include shipping in displayed prices", nil)
	}
	if r.Market.Validate() != nil || !strings.EqualFold(r.Market.Country, country) || !strings.EqualFold(r.Market.Language, "en") {
		return 0, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "this Provider requires the "+country+" market in English", nil)
	}
	if r.Resources == nil {
		return 0, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the resource service is required", nil)
	}
	return page, nil
}

// ValidateConfig rejects settings for Providers without custom configuration.
func ValidateConfig(config map[string]any) error {
	if len(config) != 0 {
		return provider.NewError(provider.ErrorCodeInvalidProviderConfig, "this Provider does not support custom settings", nil)
	}
	return nil
}

// Fetch forwards command policies and accepts successful responses at the requested endpoint only.
func Fetch(ctx context.Context, r provider.Request, resource provider.ResourceRequest) (provider.ResourceResponse, error) {
	resource.Market, resource.Cache, resource.Interactive = r.Market, r.Cache, r.Interactive
	response, err := r.Resources.Fetch(ctx, resource)
	if ctx.Err() != nil {
		return response, ctx.Err()
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return response, err
		}
		if _, coded := provider.ErrorCodeOf(err); coded {
			return response, err
		}
		return response, provider.NewError(provider.ErrorCodeHTTPFailure, "the Provider resource request failed", err)
	}
	if response.StatusCode != http.StatusOK {
		code := provider.ErrorCodeHTTPFailure
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
			code = provider.ErrorCodeAccessBlocked
		}
		return response, provider.NewError(code, "the Provider returned an unsuccessful response", nil)
	}
	if response.FinalURL != "" {
		actual, err := url.Parse(response.FinalURL)
		expected, _ := url.Parse(resource.URL)
		if err != nil || actual.Scheme != expected.Scheme || actual.Host != expected.Host || actual.Path != expected.Path || actual.User != nil {
			return response, InvalidResult("the Provider redirected away from the requested endpoint")
		}
	}
	if response.Page != nil {
		response.Body = response.Page.HTML
	}
	return response, nil
}

// InvalidResult creates a safe parser failure without exposing response content.
func InvalidResult(message string) error {
	return provider.NewError(provider.ErrorCodeInvalidProviderResult, message, nil)
}

// Finish adds common warnings and rejects listings in which every entry failed.
func Finish(result provider.ProductPage, found int, currency string) (provider.ProductPage, error) {
	if found > 0 && len(result.Items) == 0 {
		return provider.ProductPage{}, InvalidResult("no product entries could be parsed")
	}
	if len(result.Items) != found {
		warning := provider.NewWarning(provider.WarningCodePartialParsing, "Some product entries could not be parsed.", nil)
		warning.FoundCount, warning.ParsedCount = new(found), new(len(result.Items))
		result.Warnings = append(result.Warnings, warning)
	}
	if currency != "EUR" {
		warning := provider.NewWarning(provider.WarningCodeCurrencyUnavailable, "This catalog displays prices in EUR only.", nil)
		warning.RequestedCurrency, warning.ActualCurrency = currency, "EUR"
		result.Warnings = append(result.Warnings, warning)
	}
	return result, nil
}

// ProductURL resolves only HTTPS product links on the expected host and path.
func ProductURL(value, base, prefix string) string {
	root, _ := url.Parse(base)
	link, err := url.Parse(strings.TrimSpace(value))
	if err != nil || value == "" || link.User != nil {
		return ""
	}
	link = root.ResolveReference(link)
	if link.Scheme != "https" || link.Host != root.Host || !strings.HasPrefix(link.Path, prefix) {
		return ""
	}
	link.RawQuery, link.Fragment = "", ""
	return link.String()
}
