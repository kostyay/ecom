package ytindustries

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/providers/internal/shoputil"
)

var productIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type searchDocument struct {
	Elements []json.RawMessage `json:"elements"`
	Total    *int              `json:"total"`
	Page     int               `json:"page"`
	Limit    int               `json:"limit"`
	Alias    string            `json:"apiAlias"`
	Filters  struct {
		Search string `json:"search"`
	} `json:"currentFilters"`
}

type calculatedPrice struct {
	UnitPrice json.Number `json:"unitPrice"`
	ListPrice *struct {
		Price json.Number `json:"price"`
	} `json:"listPrice"`
	HasRange bool `json:"hasRange"`
}

type product struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Number       string `json:"productNumber"`
	Manufacturer struct {
		Name string `json:"name"`
	} `json:"manufacturer"`
	Cover *struct {
		Media struct {
			URL string `json:"url"`
		} `json:"media"`
	} `json:"cover"`
	URLs []struct {
		Path      string `json:"seoPathInfo"`
		Canonical bool   `json:"isCanonical"`
		Deleted   bool   `json:"isDeleted"`
		ProductID string `json:"foreignKey"`
	} `json:"seoUrls"`
	Price      calculatedPrice `json:"calculatedPrice"`
	Cheapest   calculatedPrice `json:"calculatedCheapestPrice"`
	Extensions struct {
		Availability struct {
			Text     string `json:"availability"`
			Preorder bool   `json:"preorder"`
		} `json:"availability"`
	} `json:"extensions"`
}

func parseSearch(response provider.ResourceResponse, query string, page int, currency string) (provider.ProductPage, error) {
	var doc searchDocument
	if json.Unmarshal(response.Body, &doc) != nil || doc.Alias != "product_listing" || doc.Total == nil || *doc.Total < 0 || doc.Elements == nil || doc.Page != page || doc.Limit != pageSize || doc.Filters.Search != query {
		return provider.ProductPage{}, shoputil.InvalidResult("YT returned invalid search metadata")
	}
	expected := min(pageSize, max(0, *doc.Total-(page-1)*pageSize))
	if len(doc.Elements) != expected {
		return provider.ProductPage{}, shoputil.InvalidResult("YT returned an inconsistent product count")
	}
	totalPages := *doc.Total / pageSize
	if *doc.Total%pageSize != 0 {
		totalPages++
	}
	result := provider.ProductPage{Items: []provider.ProductSummary{}, Page: provider.PageInfo{Number: page, Size: pageSize, TotalItems: doc.Total, TotalPages: new(totalPages), HasNext: new(page < totalPages)}}
	seen := make(map[string]bool)
	for _, raw := range doc.Elements {
		var p product
		if json.Unmarshal(raw, &p) != nil {
			continue
		}
		item, ok := parseProduct(p, response.RetrievedAt)
		if !ok || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		result.Items = append(result.Items, item)
	}
	return shoputil.Finish(result, len(doc.Elements), currency)
}

func parseProduct(p product, stamp time.Time) (provider.ProductSummary, bool) {
	item := provider.ProductSummary{ID: p.ID, Name: strings.TrimSpace(p.Name), Brand: p.Manufacturer.Name, DetailLevel: provider.DetailLevelSummary, RetrievedAt: stamp, Availability: provider.AvailabilityUnknown}
	if !productIDPattern.MatchString(p.ID) || item.Name == "" {
		return item, false
	}
	for _, u := range p.URLs {
		if !u.Canonical || u.Deleted || u.ProductID != p.ID {
			continue
		}
		item.URL = shoputil.ProductURL(u.Path, baseURL+"/", "/")
		if item.URL == "" {
			return item, false
		}
		// The number parameter selects the actual bike; it is not tracking data.
		link, _ := url.Parse(u.Path)
		if number := link.Query().Get("number"); number != "" {
			if number != p.Number {
				return item, false
			}
			item.URL += "?" + url.Values{"number": {number}}.Encode()
		}
		break
	}
	if p.Cover != nil {
		item.ImageURL = shoputil.ProductURL(p.Cover.Media.URL, baseURL+"/", "/")
	}
	price := p.Price
	if p.Cheapest.HasRange {
		price = p.Cheapest
		item.ProviderData = provider.Data{Name: json.RawMessage(`{"starting_price":true}`)}
	}
	item.Price = money(price.UnitPrice)
	if item.Price == nil {
		return item, false
	}
	if price.ListPrice != nil {
		item.OriginalPrice = money(price.ListPrice.Price)
		if item.OriginalPrice == nil {
			return item, false
		}
	}
	item.StockText = p.Extensions.Availability.Text
	switch {
	case p.Extensions.Availability.Preorder:
		item.Availability = provider.AvailabilityPreorder
	case strings.EqualFold(item.StockText, "in stock"):
		item.Availability = provider.AvailabilityInStock
	case strings.EqualFold(item.StockText, "out of stock"):
		item.Availability = provider.AvailabilityOutOfStock
	}
	if p.Number != "" {
		item.Attributes = []provider.Attribute{{Name: "product_number", Value: p.Number}}
	}
	return item, item.Validate() == nil
}

func money(number json.Number) *provider.Money {
	amount := number.String()
	if !strings.Contains(amount, ".") {
		amount += ".00"
	} else if _, fraction, _ := strings.Cut(amount, "."); len(fraction) == 1 {
		amount += "0"
	}
	value := &provider.Money{Amount: amount, Currency: "EUR", Display: amount + " €"}
	if value.Validate() != nil {
		return nil
	}
	return value
}
