package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/kostyay/ecom/internal/output"
	"github.com/kostyay/ecom/provider"
)

// ItemInput contains arguments for one item detail request.
type ItemInput struct {
	IDOrURL      string
	Variants     []string
	Refresh      bool
	StaleIfError bool
	Interactive  bool
}

// ItemResult contains a validated item and its output context.
type ItemResult struct {
	ProviderName string
	Market       provider.Market
	Item         provider.ItemResult
	Metadata     output.Metadata
}

// Item gets full item details by opaque provider ID or provider-owned URL.
func (services *Services) Item(ctx context.Context, input ItemInput) (ItemResult, error) {
	if services == nil || services.Provider == nil {
		return ItemResult{}, errors.New("application provider services are required")
	}
	identifier, err := validateItemIdentifier(input.IDOrURL)
	if err != nil {
		return ItemResult{}, err
	}
	help, err := services.selectedProviderHelp(ctx)
	if err != nil {
		return ItemResult{}, err
	}
	if err := requireCapability(services.Provider, help, provider.CapabilityItem); err != nil {
		return ItemResult{}, err
	}
	variants, err := parseVariantSelections(input.Variants)
	if err != nil {
		return ItemResult{}, err
	}
	if len(variants) > 0 {
		if err := requireCapability(services.Provider, help, provider.CapabilityVariantSelection); err != nil {
			return ItemResult{}, err
		}
	}

	collector := newMetadataResourceService(services.Resources, services.Market, provider.CachePolicy{
		Refresh: input.Refresh, StaleIfError: input.StaleIfError,
	}, input.Interactive)
	result, err := services.Provider.Item(ctx, provider.ItemRequest{
		Market: services.Market, Pricing: PricingFromConfig(services.Settings.Pricing),
		Cache: collector.cache, Interactive: input.Interactive, Resources: collector,
		IDOrURL: identifier, Variants: variants,
	})
	if err != nil {
		return ItemResult{}, err
	}
	if err := validateItemResult(result); err != nil {
		return ItemResult{}, provider.NewError(provider.ErrorCodeInvalidProviderResult, "provider returned invalid item data", err)
	}
	if err := validateSelectedVariant(result.Item, variants); err != nil {
		return ItemResult{}, err
	}
	return ItemResult{
		ProviderName: services.Provider.Name(), Market: services.Market, Item: result, Metadata: collector.Metadata(),
	}, nil
}

func validateItemIdentifier(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", provider.NewError(provider.ErrorCodeInvalidFilter, "item ID or URL is required", nil)
	}
	looksLikeURL := strings.Contains(value, "://") || strings.HasPrefix(value, "http:") || strings.HasPrefix(value, "https:")
	if !looksLikeURL {
		return value, nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed == nil {
		return "", provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("item URL %q must be an absolute HTTP or HTTPS URL", value), err)
	}
	validScheme := strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")
	if parsed.Host == "" || !validScheme {
		return "", provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("item URL %q must be an absolute HTTP or HTTPS URL", value), err)
	}
	return value, nil
}

func parseVariantSelections(values []string) ([]provider.VariantSelection, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]provider.VariantSelection, 0, len(values))
	for _, raw := range values {
		key, value, found := strings.Cut(raw, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !found || key == "" || value == "" {
			return nil, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("variant %q must use key=value", raw), nil)
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("variant key %q cannot be repeated", key), nil)
		}
		seen[key] = struct{}{}
		result = append(result, provider.VariantSelection{Key: key, Value: value})
	}
	return result, nil
}

func validateItemResult(result provider.ItemResult) error {
	if err := result.Item.Validate(); err != nil {
		return err
	}
	if err := validateProviderData(result.ProviderData); err != nil {
		return err
	}
	if err := validateWarnings(result.Warnings); err != nil {
		return err
	}
	return nil
}

func validateSelectedVariant(item provider.ItemDetail, selections []provider.VariantSelection) error {
	if len(selections) == 0 {
		return nil
	}
	if item.SelectedVariant != nil && variantMatches(*item.SelectedVariant, selections) {
		return nil
	}
	choices := visibleVariantChoices(item.Variants)
	message := "variant selection was not found"
	if len(choices) == 0 {
		message += "; the provider returned no valid choices"
	} else {
		message += "; valid choices: " + strings.Join(choices, ", ")
	}
	return provider.NewError(provider.ErrorCodeVariantNotFound, message, nil)
}

func variantMatches(variant provider.Variant, selections []provider.VariantSelection) bool {
	attributes := make(map[string]string, len(variant.Attributes))
	for _, attribute := range variant.Attributes {
		attributes[attribute.Name] = attribute.Value
	}
	for _, selection := range selections {
		if attributes[selection.Key] != selection.Value {
			return false
		}
	}
	return true
}

func visibleVariantChoices(variants []provider.Variant) []string {
	seen := make(map[string]struct{})
	var choices []string
	for _, variant := range variants {
		for _, attribute := range variant.Attributes {
			choice := attribute.Name + "=" + attribute.Value
			if _, exists := seen[choice]; exists {
				continue
			}
			seen[choice] = struct{}{}
			choices = append(choices, choice)
		}
	}
	slices.Sort(choices)
	return choices
}
