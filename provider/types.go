// Package provider defines the public types used by ecom providers.
package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	decimalPattern  = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

// Market identifies the country, language, and preferred currency for a request.
type Market struct {
	Country  string `json:"country"`
	Language string `json:"language"`
	Currency string `json:"currency"`
}

// Validate checks that a market has all required values.
func (m Market) Validate() error {
	if strings.TrimSpace(m.Country) == "" {
		return errors.New("market country is required")
	}
	if strings.TrimSpace(m.Language) == "" {
		return errors.New("market language is required")
	}
	if !currencyPattern.MatchString(m.Currency) {
		return errors.New("market currency must be a three-letter uppercase code")
	}
	return nil
}

// Money contains a decimal amount and the exact price text from the provider.
type Money struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Display  string `json:"display"`
}

// Validate checks the stable money representation.
func (m Money) Validate() error {
	if !decimalPattern.MatchString(m.Amount) {
		return errors.New("money amount must be a non-negative decimal string")
	}
	if !currencyPattern.MatchString(m.Currency) {
		return errors.New("money currency must be a three-letter uppercase code")
	}
	if m.Display == "" {
		return errors.New("money display text is required")
	}
	return nil
}

// PriceRange is the lowest and highest displayed price for a product.
type PriceRange struct {
	Minimum Money `json:"minimum"`
	Maximum Money `json:"maximum"`
}

// Validate checks both prices and their order.
func (r PriceRange) Validate() error {
	if err := r.Minimum.Validate(); err != nil {
		return fmt.Errorf("minimum price: %w", err)
	}
	if err := r.Maximum.Validate(); err != nil {
		return fmt.Errorf("maximum price: %w", err)
	}
	if r.Minimum.Currency != r.Maximum.Currency {
		return errors.New("price range currencies must match")
	}

	minimum, _ := new(big.Rat).SetString(r.Minimum.Amount)
	maximum, _ := new(big.Rat).SetString(r.Maximum.Amount)
	if minimum.Cmp(maximum) > 0 {
		return errors.New("minimum price must not exceed maximum price")
	}
	return nil
}

// Attribute is one product or variant property.
type Attribute struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Availability is the provider-neutral stock state when it is known.
type Availability string

const (
	// AvailabilityUnknown means that the provider did not supply a stock state.
	AvailabilityUnknown Availability = "unknown"
	// AvailabilityInStock means that the provider shows the item as available.
	AvailabilityInStock Availability = "in_stock"
	// AvailabilityOutOfStock means that the provider shows the item as unavailable.
	AvailabilityOutOfStock Availability = "out_of_stock"
	// AvailabilityPreorder means that the provider accepts orders before release.
	AvailabilityPreorder Availability = "preorder"
)

// DetailLevel tells whether a product contains listing or item-detail data.
type DetailLevel string

const (
	// DetailLevelSummary identifies product-listing data.
	DetailLevelSummary DetailLevel = "summary"
	// DetailLevelFull identifies full item data.
	DetailLevelFull DetailLevel = "full"
)

// Data contains provider-specific JSON grouped by namespace.
type Data map[string]json.RawMessage

// Variant is one visible purchasable product choice.
type Variant struct {
	ID           string       `json:"id,omitempty"`
	Attributes   []Attribute  `json:"attributes,omitempty"`
	Price        *Money       `json:"price,omitempty"`
	Availability Availability `json:"availability,omitempty"`
	StockText    string       `json:"stock_text,omitempty"`
	Selected     bool         `json:"selected,omitzero"`
	ProviderData Data         `json:"provider_data,omitempty"`
}

// ProductSummary contains common product data from a listing.
type ProductSummary struct {
	ID              string       `json:"id,omitempty"`
	URL             string       `json:"url,omitempty"`
	Name            string       `json:"name,omitempty"`
	Brand           string       `json:"brand,omitempty"`
	Price           *Money       `json:"price,omitempty"`
	OriginalPrice   *Money       `json:"original_price,omitempty"`
	DiscountAmount  *Money       `json:"discount_amount,omitempty"`
	DiscountPercent *int         `json:"discount_percent,omitempty"`
	PriceRange      *PriceRange  `json:"price_range,omitempty"`
	Availability    Availability `json:"availability,omitempty"`
	StockText       string       `json:"stock_text,omitempty"`
	SelectedVariant *Variant     `json:"selected_variant,omitempty"`
	Variants        []Variant    `json:"variants,omitempty"`
	ImageURL        string       `json:"image_url,omitempty"`
	Attributes      []Attribute  `json:"attributes,omitempty"`
	RetrievedAt     time.Time    `json:"retrieved_at,omitzero"`
	DetailLevel     DetailLevel  `json:"detail_level,omitempty"`
	ProviderData    Data         `json:"provider_data,omitempty"`
}

// ItemDetail contains full information for one provider item.
type ItemDetail struct {
	ProductSummary
	Description string `json:"description,omitempty"`
}

// Category is one node in a provider category tree.
type Category struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	Path         string `json:"path,omitempty"`
	ParentID     string `json:"parent_id,omitempty"`
	URL          string `json:"url,omitempty"`
	HasChildren  bool   `json:"has_children"`
	ProviderData Data   `json:"provider_data,omitempty"`
}

// Brand identifies a provider brand page or filter value.
type Brand struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	URL          string `json:"url,omitempty"`
	ProviderData Data   `json:"provider_data,omitempty"`
}

// Deal is a product for which the provider shows a price reduction.
type Deal struct {
	Product ProductSummary `json:"product"`
}

// Validate checks a product and its nested common values.
func (p ProductSummary) Validate() error {
	if err := validateOptionalURL("product URL", p.URL); err != nil {
		return err
	}
	if err := validateOptionalURL("image URL", p.ImageURL); err != nil {
		return err
	}
	if err := validateMoney("price", p.Price); err != nil {
		return err
	}
	if err := validateMoney("original price", p.OriginalPrice); err != nil {
		return err
	}
	if err := validateMoney("discount amount", p.DiscountAmount); err != nil {
		return err
	}
	if p.DiscountPercent != nil && (*p.DiscountPercent < 0 || *p.DiscountPercent > 100) {
		return errors.New("discount percent must be between 0 and 100")
	}
	if p.PriceRange != nil {
		if err := p.PriceRange.Validate(); err != nil {
			return fmt.Errorf("price range: %w", err)
		}
	}
	if err := validateAvailability(p.Availability); err != nil {
		return err
	}
	if p.DetailLevel != "" && p.DetailLevel != DetailLevelSummary && p.DetailLevel != DetailLevelFull {
		return fmt.Errorf("unknown detail level %q", p.DetailLevel)
	}
	if p.SelectedVariant != nil {
		if err := p.SelectedVariant.Validate(); err != nil {
			return fmt.Errorf("selected variant: %w", err)
		}
	}
	for index, variant := range p.Variants {
		if err := variant.Validate(); err != nil {
			return fmt.Errorf("variant %d: %w", index, err)
		}
	}
	return validateProviderData(p.ProviderData)
}

// Validate checks an item detail and its embedded product data.
func (i ItemDetail) Validate() error {
	if err := i.ProductSummary.Validate(); err != nil {
		return err
	}
	if i.DetailLevel != DetailLevelFull {
		return errors.New("item detail level must be full")
	}
	return nil
}

// Validate checks a variant and its nested common values.
func (v Variant) Validate() error {
	if err := validateMoney("variant price", v.Price); err != nil {
		return err
	}
	if err := validateAvailability(v.Availability); err != nil {
		return err
	}
	return validateProviderData(v.ProviderData)
}

// Validate checks category common values.
func (c Category) Validate() error {
	if err := validateOptionalURL("category URL", c.URL); err != nil {
		return err
	}
	return validateProviderData(c.ProviderData)
}

// Validate checks brand common values.
func (b Brand) Validate() error {
	if err := validateOptionalURL("brand URL", b.URL); err != nil {
		return err
	}
	return validateProviderData(b.ProviderData)
}

// Validate checks that a deal contains valid product data and a shown reduction.
func (d Deal) Validate() error {
	if err := d.Product.Validate(); err != nil {
		return err
	}
	if d.Product.OriginalPrice == nil && d.Product.DiscountAmount == nil && d.Product.DiscountPercent == nil {
		return errors.New("deal must contain a provider-shown reduction")
	}
	return nil
}

func validateMoney(name string, money *Money) error {
	if money == nil {
		return nil
	}
	if err := money.Validate(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func validateOptionalURL(name, value string) error {
	if value == "" {
		return nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute HTTP URL", name)
	}
	return nil
}

func validateAvailability(value Availability) error {
	switch value {
	case "", AvailabilityUnknown, AvailabilityInStock, AvailabilityOutOfStock, AvailabilityPreorder:
		return nil
	default:
		return fmt.Errorf("unknown availability %q", value)
	}
}

func validateProviderData(data Data) error {
	for namespace, value := range data {
		if strings.TrimSpace(namespace) == "" {
			return errors.New("provider data namespace is required")
		}
		if !json.Valid(value) {
			return fmt.Errorf("provider data %q must contain valid JSON", namespace)
		}
	}
	return nil
}
