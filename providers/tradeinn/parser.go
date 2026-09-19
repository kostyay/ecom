package tradeinn

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kostyay/ecom/provider"
)

var productPath = regexp.MustCompile(`^/([a-z][a-z-]*)/en/[^/]+/([1-9][0-9]*)/p$`)
var numericID = regexp.MustCompile(`^[0-9]+$`)

type searchDocument struct {
	Results            []jsontext.Value `json:"results"`
	TotalSize          *int             `json:"totalSize"`
	NextPageToken      string           `json:"nextPageToken"`
	CorrectedQuery     string           `json:"correctedQuery"`
	QueryExpansionInfo struct {
		ExpandedQuery bool `json:"expandedQuery"`
	} `json:"queryExpansionInfo"`
	Status string         `json:"status"`
	Error  jsontext.Value `json:"error"`
}

type searchProduct struct {
	ID      string `json:"id"`
	Product struct {
		Title        string   `json:"title"`
		Brands       []string `json:"brands"`
		URI          string   `json:"uri"`
		Availability string   `json:"availability"`
		Attributes   map[string]struct {
			Text []string `json:"text"`
		} `json:"attributes"`
	} `json:"product"`
}

func decodeSearch(body []byte) (searchDocument, error) {
	var document searchDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return document, invalidResult("Tradeinn returned invalid search JSON")
	}
	if document.Status != "" || len(document.Error) != 0 || document.TotalSize == nil || *document.TotalSize < len(document.Results) || len(document.Results) > pageSize || len(document.Results) == 0 && (*document.TotalSize != 0 || document.NextPageToken != "") {
		return document, invalidResult("Tradeinn returned invalid search metadata")
	}
	return document, nil
}

func parseProducts(document searchDocument, countryID, query string, page int, retrievedAt time.Time) (provider.ProductPage, error) {
	result := provider.ProductPage{
		Items: make([]provider.ProductSummary, 0, len(document.Results)),
		Page:  provider.PageInfo{Number: page, Size: pageSize, TotalItems: document.TotalSize, HasNext: new(document.NextPageToken != "")},
	}
	seen := make(map[string]bool)
	skipped := 0
	for _, raw := range document.Results {
		item, ok := parseProduct(raw, countryID, retrievedAt)
		if !ok || seen[item.ID] {
			skipped++
			continue
		}
		seen[item.ID] = true
		result.Items = append(result.Items, item)
	}
	if skipped > 0 {
		if len(result.Items) == 0 {
			return provider.ProductPage{}, invalidResult("Tradeinn returned no valid products")
		}
		result.Warnings = append(result.Warnings, provider.Warning{
			Code: provider.WarningCodePartialParsing, Message: "Some Tradeinn products were invalid, duplicated, or had no price for this market.",
			FoundCount: new(len(document.Results)), ParsedCount: new(len(result.Items)),
		})
	}
	if document.QueryExpansionInfo.ExpandedQuery || document.CorrectedQuery != "" && document.CorrectedQuery != query {
		result.Warnings = append(result.Warnings, provider.NewWarning(provider.WarningCodeSearchSemanticsUnverified, "Tradeinn corrected or expanded the search query; results can include related products.", nil))
		metadata, err := json.Marshal(struct {
			CorrectedQuery string `json:"corrected_query,omitempty"`
			ExpandedQuery  bool   `json:"expanded_query"`
		}{document.CorrectedQuery, document.QueryExpansionInfo.ExpandedQuery})
		if err != nil {
			return provider.ProductPage{}, err
		}
		result.ProviderData = provider.Data{Name: metadata}
	}
	return result, nil
}

func parseProduct(raw []byte, countryID string, retrievedAt time.Time) (provider.ProductSummary, bool) {
	var entry searchProduct
	if err := json.Unmarshal(raw, &entry); err != nil {
		return provider.ProductSummary{}, false
	}
	product := entry.Product
	u, err := url.Parse(product.URI)
	if err != nil || u.Scheme != "https" || u.Host != "www.tradeinn.com" || u.User != nil || u.RawPath != "" || u.Fragment != "" {
		return provider.ProductSummary{}, false
	}
	parts := productPath.FindStringSubmatch(u.Path)
	if parts == nil || parts[2] != entry.ID || strings.TrimSpace(product.Title) == "" {
		return provider.ProductSummary{}, false
	}
	u.RawQuery, u.ForceQuery = "", false
	shopIDs := product.Attributes["shopId"].Text
	if len(shopIDs) != 1 || !numericID.MatchString(shopIDs[0]) {
		return provider.ProductSummary{}, false
	}
	priceField := "price_all_1_to_10"
	if countryID == "75" {
		priceField = "price_all_71_to_80"
	}
	amount := ""
	for _, value := range product.Attributes[priceField].Text {
		id, price, found := strings.Cut(value, ":")
		if found && id == countryID {
			if amount != "" {
				return provider.ProductSummary{}, false
			}
			amount = price
		}
	}
	item := provider.ProductSummary{
		ID: entry.ID, Name: strings.TrimSpace(product.Title), URL: u.String(),
		Price:       &provider.Money{Amount: amount, Currency: "EUR", Display: amount + " €"},
		RetrievedAt: retrievedAt, DetailLevel: provider.DetailLevelSummary,
		Availability: provider.AvailabilityUnknown,
	}
	if len(product.Brands) > 0 {
		item.Brand = strings.TrimSpace(product.Brands[0])
		item.Name = strings.TrimSpace(item.Brand + " " + item.Name)
	}
	switch product.Availability {
	case "IN_STOCK":
		item.Availability = provider.AvailabilityInStock
	case "OUT_OF_STOCK":
		item.Availability = provider.AvailabilityOutOfStock
	case "PREORDER":
		item.Availability = provider.AvailabilityPreorder
	}
	metadata, err := json.Marshal(struct {
		ShopID              string `json:"shop_id"`
		Shop                string `json:"shop"`
		CatalogAvailability string `json:"catalog_availability,omitempty"`
	}{shopIDs[0], parts[1], product.Availability})
	if err != nil {
		return provider.ProductSummary{}, false
	}
	item.ProviderData = provider.Data{Name: metadata}
	return item, item.Validate() == nil
}
