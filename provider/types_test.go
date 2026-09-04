package provider

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestDomainTypesJSONRoundTrip(t *testing.T) {
	discount := 25
	now := time.Date(2026, time.August, 12, 12, 30, 0, 0, time.UTC)
	product := ProductSummary{
		ID:              "item-123",
		URL:             "https://shop.example/items/123",
		Name:            "Trail Helmet",
		Brand:           "Example",
		Price:           &Money{Amount: "74.95", Currency: "EUR", Display: "€74.95"},
		OriginalPrice:   &Money{Amount: "99.95", Currency: "EUR", Display: "€99.95"},
		DiscountAmount:  &Money{Amount: "25.00", Currency: "EUR", Display: "€25.00"},
		DiscountPercent: &discount,
		PriceRange: &PriceRange{
			Minimum: Money{Amount: "74.95", Currency: "EUR", Display: "€74.95"},
			Maximum: Money{Amount: "84.95", Currency: "EUR", Display: "€84.95"},
		},
		Availability: AvailabilityInStock,
		StockText:    "Available now",
		SelectedVariant: &Variant{
			ID:           "black-m",
			Attributes:   []Attribute{{Name: "color", Value: "black"}, {Name: "size", Value: "M"}},
			Price:        &Money{Amount: "74.95", Currency: "EUR", Display: "€74.95"},
			Availability: AvailabilityInStock,
			Selected:     true,
		},
		Variants:     []Variant{{ID: "black-m", Selected: true}},
		ImageURL:     "https://shop.example/images/123.jpg",
		Attributes:   []Attribute{{Name: "weight", Value: "280 g"}},
		RetrievedAt:  now,
		DetailLevel:  DetailLevelSummary,
		ProviderData: Data{"example": json.RawMessage(`{"campaign":"summer"}`)},
	}

	tests := []struct {
		name  string
		value any
		new   func() any
	}{
		{name: "market", value: Market{Country: "DE", Language: "en", Currency: "EUR"}, new: func() any { return new(Market) }},
		{name: "money", value: Money{Amount: "74.95", Currency: "EUR", Display: "€74.95"}, new: func() any { return new(Money) }},
		{name: "product summary", value: product, new: func() any { return new(ProductSummary) }},
		{name: "item detail", value: ItemDetail{ProductSummary: product, Description: "A helmet."}, new: func() any { return new(ItemDetail) }},
		{name: "category", value: Category{ID: "helmets", Name: "Helmets", Path: "Cycling/Helmets", URL: "https://shop.example/helmets", HasChildren: true}, new: func() any { return new(Category) }},
		{name: "brand", value: Brand{ID: "example", Name: "Example", URL: "https://shop.example/brands/example"}, new: func() any { return new(Brand) }},
		{name: "deal", value: Deal{Product: product}, new: func() any { return new(Deal) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			decoded := test.new()
			if err := json.Unmarshal(encoded, decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(reflect.ValueOf(decoded).Elem().Interface(), test.value) {
				t.Errorf("round trip mismatch\ngot:  %#v\nwant: %#v", reflect.ValueOf(decoded).Elem().Interface(), test.value)
			}
		})
	}
}

func TestMoneyValidate(t *testing.T) {
	tests := []struct {
		name    string
		money   Money
		wantErr bool
	}{
		{name: "valid integer", money: Money{Amount: "0", Currency: "EUR", Display: "€0"}},
		{name: "valid decimal", money: Money{Amount: "79.95", Currency: "EUR", Display: "€79.95"}},
		{name: "float syntax", money: Money{Amount: "7.995e1", Currency: "EUR", Display: "€79.95"}, wantErr: true},
		{name: "negative", money: Money{Amount: "-1.00", Currency: "EUR", Display: "-€1.00"}, wantErr: true},
		{name: "lowercase currency", money: Money{Amount: "1.00", Currency: "eur", Display: "€1.00"}, wantErr: true},
		{name: "missing display", money: Money{Amount: "1.00", Currency: "EUR"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.money.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, want error %t", err, test.wantErr)
			}
		})
	}
}

func TestDomainValidation(t *testing.T) {
	invalidDiscount := 101
	tests := []struct {
		name    string
		valid   func() error
		wantErr bool
	}{
		{name: "market", valid: func() error { return (Market{Country: "DE", Language: "en", Currency: "EUR"}).Validate() }},
		{name: "market missing country", valid: func() error { return (Market{Language: "en", Currency: "EUR"}).Validate() }, wantErr: true},
		{name: "ordered price range", valid: func() error {
			return (PriceRange{Minimum: Money{Amount: "9.99", Currency: "EUR", Display: "€9.99"}, Maximum: Money{Amount: "10.00", Currency: "EUR", Display: "€10.00"}}).Validate()
		}},
		{name: "reversed price range", valid: func() error {
			return (PriceRange{Minimum: Money{Amount: "10.01", Currency: "EUR", Display: "€10.01"}, Maximum: Money{Amount: "10.00", Currency: "EUR", Display: "€10.00"}}).Validate()
		}, wantErr: true},
		{name: "product", valid: func() error {
			return (ProductSummary{URL: "https://shop.example/item", DetailLevel: DetailLevelSummary}).Validate()
		}},
		{name: "invalid product URL", valid: func() error { return (ProductSummary{URL: "/relative"}).Validate() }, wantErr: true},
		{name: "invalid discount", valid: func() error { return (ProductSummary{DiscountPercent: &invalidDiscount}).Validate() }, wantErr: true},
		{name: "invalid provider data", valid: func() error {
			return (ProductSummary{ProviderData: Data{"shop": json.RawMessage(`{`)}}).Validate()
		}, wantErr: true},
		{name: "deal with reduction", valid: func() error { return (Deal{Product: ProductSummary{DiscountPercent: new(10)}}).Validate() }},
		{name: "deal without reduction", valid: func() error { return (Deal{}).Validate() }, wantErr: true},
		{name: "full item", valid: func() error {
			return (ItemDetail{DetailLevel: DetailLevelFull}).Validate()
		}},
		{name: "summary item", valid: func() error {
			return (ItemDetail{DetailLevel: DetailLevelSummary}).Validate()
		}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.valid()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, want error %t", err, test.wantErr)
			}
		})
	}
}
