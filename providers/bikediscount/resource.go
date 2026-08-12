package bikediscount

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/kostyay/ecom/provider"
)

var bikeDiscountTransportOrder = []provider.TransportMode{
	provider.TransportHTTP,
	provider.TransportBrowser,
	provider.TransportCDP,
}

// resourceTarget contains the provider-owned parts of one website request.
// Common request policy always comes from the operation request.
type resourceTarget struct {
	URL   string
	Path  string
	Query []provider.RequestValue
	DOM   []provider.DOMExtraction
}

// fetchResource requests one Bike-Discount page through the Core service. It
// does not own an HTTP client, a browser, or browser session state.
func fetchResource(ctx context.Context, request provider.Request, target resourceTarget) (provider.ResourceResponse, error) {
	if err := ctx.Err(); err != nil {
		return provider.ResourceResponse{}, err
	}
	if err := validatePricingPolicy(request.Pricing); err != nil {
		return provider.ResourceResponse{}, err
	}
	if request.Resources == nil {
		return provider.ResourceResponse{}, provider.NewError(
			provider.ErrorCodeInvalidResourceRequest,
			"the Bike-Discount resource service is required",
			nil,
		)
	}
	if err := request.Market.Validate(); err != nil {
		return provider.ResourceResponse{}, provider.NewError(
			provider.ErrorCodeInvalidResourceRequest,
			"the Bike-Discount market is invalid",
			err,
		)
	}

	requestURL, err := bikeDiscountRequestURL(request.Market, target)
	if err != nil {
		return provider.ResourceResponse{}, provider.NewError(
			provider.ErrorCodeInvalidResourceRequest,
			"the Bike-Discount resource URL is invalid",
			err,
		)
	}
	response, err := request.Resources.Fetch(ctx, provider.ResourceRequest{
		Method:      http.MethodGet,
		URL:         requestURL,
		Query:       cloneRequestValues(target.Query),
		Transport:   provider.TransportPolicy{Preferred: append([]provider.TransportMode(nil), bikeDiscountTransportOrder...)},
		Market:      request.Market,
		Cache:       request.Cache,
		Interactive: request.Interactive,
		DOM:         append([]provider.DOMExtraction(nil), target.DOM...),
	})
	if err != nil {
		return response, stableResourceError(response, err)
	}
	return response, nil
}

func bikeDiscountRequestURL(market provider.Market, target resourceTarget) (string, error) {
	if target.URL != "" && target.Path != "" {
		return "", errors.New("URL and path cannot be combined")
	}
	language := strings.ToLower(strings.TrimSpace(market.Language))
	if language != "en" {
		return "", errors.New("language is not supported by the verified website pages")
	}
	if target.URL != "" {
		parsed, err := url.Parse(strings.TrimSpace(target.URL))
		if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "www.bike-discount.de") || parsed.User != nil || parsed.Fragment != "" {
			return "", errors.New("URL must be an HTTPS Bike-Discount URL without user information or a fragment")
		}
		languagePrefix := "/" + language
		if parsed.Path != languagePrefix && !strings.HasPrefix(parsed.Path, languagePrefix+"/") {
			return "", errors.New("URL language does not match the requested market")
		}
		return parsed.String(), nil
	}

	cleanPath := strings.TrimSpace(target.Path)
	if cleanPath == "" {
		cleanPath = "/"
	}
	if !strings.HasPrefix(cleanPath, "/") || strings.ContainsAny(cleanPath, "?#") {
		return "", errors.New("path must be absolute and must not contain a query or fragment")
	}
	cleanPath = path.Clean(cleanPath)
	if cleanPath == "." || cleanPath == "/" {
		cleanPath = ""
	}
	return bikeDiscountBaseURL + "/" + language + cleanPath, nil
}

func cloneRequestValues(values []provider.RequestValue) []provider.RequestValue {
	clone := make([]provider.RequestValue, len(values))
	for index, value := range values {
		clone[index] = value
		clone[index].Values = append([]string(nil), value.Values...)
	}
	return clone
}

func stableResourceError(response provider.ResourceResponse, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if _, ok := provider.ErrorCodeOf(err); ok {
		return err
	}
	code := provider.ErrorCodeHTTPFailure
	if response.Transport == provider.TransportBrowser || response.Transport == provider.TransportCDP {
		code = provider.ErrorCodeBrowserFailure
	}
	return provider.NewError(code, "the Bike-Discount resource request failed", err)
}

func responseDocument(response provider.ResourceResponse) []byte {
	if response.Page != nil {
		return response.Page.HTML
	}
	return response.Body
}

func currencyWarnings(requestedCurrency string, products ...provider.ProductSummary) []provider.Warning {
	requestedCurrency = strings.ToUpper(strings.TrimSpace(requestedCurrency))
	actualCurrencies := make(map[string]struct{})
	for _, product := range products {
		collectMoneyCurrency(actualCurrencies, product.Price)
		collectMoneyCurrency(actualCurrencies, product.OriginalPrice)
		collectMoneyCurrency(actualCurrencies, product.DiscountAmount)
		if product.PriceRange != nil {
			collectMoneyCurrency(actualCurrencies, &product.PriceRange.Minimum)
			collectMoneyCurrency(actualCurrencies, &product.PriceRange.Maximum)
		}
		if product.SelectedVariant != nil {
			collectMoneyCurrency(actualCurrencies, product.SelectedVariant.Price)
		}
		for _, variant := range product.Variants {
			collectMoneyCurrency(actualCurrencies, variant.Price)
		}
	}

	orderedCurrencies := make([]string, 0, len(actualCurrencies))
	for actualCurrency := range actualCurrencies {
		orderedCurrencies = append(orderedCurrencies, actualCurrency)
	}
	sort.Strings(orderedCurrencies)
	warnings := make([]provider.Warning, 0, len(orderedCurrencies))
	for _, actualCurrency := range orderedCurrencies {
		if actualCurrency == requestedCurrency {
			continue
		}
		warning := provider.NewWarning(
			provider.WarningCodeCurrencyUnavailable,
			"Bike-Discount returned a different displayed currency. No currency conversion was done.",
			nil,
		)
		warning.RequestedCurrency = requestedCurrency
		warning.ActualCurrency = actualCurrency
		warnings = append(warnings, warning)
	}
	return warnings
}

func collectMoneyCurrency(currencies map[string]struct{}, money *provider.Money) {
	if money == nil {
		return
	}
	currency := strings.ToUpper(strings.TrimSpace(money.Currency))
	if currency != "" {
		currencies[currency] = struct{}{}
	}
}
