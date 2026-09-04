// Package wallapop implements the Wallapop commerce provider.
// Import this package for its registration side effect.
package wallapop

import (
	"bytes"
	"cmp"
	"context"
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kostyay/ecom/provider"
	"golang.org/x/net/html"
)

const (
	// Name is the stable provider identifier.
	Name             = "wallapop"
	apiBaseURL       = "https://api.wallapop.com/api/v3/search"
	websiteBaseURL   = "https://es.wallapop.com"
	defaultPageSize  = 40
	defaultLatitude  = 42.5063
	defaultLongitude = 1.5218
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type implementation struct{}

func init() { provider.MustRegister(registration()) }

func registration() provider.Registration {
	return provider.Registration{
		Name: Name, SDKAPIVersion: provider.APIVersion, Implementation: implementation{},
		Capabilities: []provider.CapabilityName{provider.CapabilitySearch, provider.CapabilityItem},
	}
}

func (implementation) ValidateConfig(configuration map[string]any) error {
	for key := range configuration {
		return provider.NewError(provider.ErrorCodeInvalidProviderConfig, fmt.Sprintf("Wallapop does not support setting %q", key), nil)
	}
	return nil
}

func (implementation) Help(_ context.Context, request provider.HelpRequest) (provider.HelpResult, error) {
	if err := validatePolicy(request.Request); err != nil {
		return provider.HelpResult{}, err
	}
	help := provider.Help{
		Name: Name, DisplayName: "Wallapop", Description: "Find public second-hand listings on Wallapop.",
		Capabilities: []provider.CapabilityHelp{
			{Name: provider.CapabilitySearch, Supported: true, Description: "Search public listings near a coordinate."},
			{Name: provider.CapabilityItem, Supported: true, Description: "Get public listing details by slug or URL."},
			{Name: provider.CapabilityCategories, Supported: false},
			{Name: provider.CapabilityCategorySearch, Supported: false},
			{Name: provider.CapabilityCategoryItems, Supported: false},
			{Name: provider.CapabilityBrands, Supported: false},
			{Name: provider.CapabilityBrandSearch, Supported: false},
			{Name: provider.CapabilityBrandItems, Supported: false},
			{Name: provider.CapabilityDeals, Supported: false},
			{Name: provider.CapabilityFilters, Supported: false},
			{Name: provider.CapabilityVariantSelection, Supported: false},
		},
		Search: &provider.SearchHelp{
			QueryRequired: true, Syntax: "plain listing text", Examples: []string{"gravel bike talla M"},
			Notes: []string{"The default search center is Andorra la Vella. Use latitude and longitude together to change it."},
		},
		Filters: searchFilterDefinitions(),
		SortModes: []provider.SortMode{
			{Value: "most_relevance", Label: "Relevance", AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}},
			{Value: "closest", Label: "Distance", AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}},
			{Value: "newest", Label: "Newest", AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}},
			{Value: "price_low_to_high", Label: "Lowest price", AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}},
			{Value: "price_high_to_low", Label: "Highest price", AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}},
		},
		Pagination: &provider.PaginationHelp{
			Mode: provider.PaginationPageNumber, FirstPage: 1, DefaultPageSize: defaultPageSize,
			SupportedPageSizes: []int{defaultPageSize},
			Notes:              []string{"Wallapop controls the actual item count. Later page requests follow its temporary cursor from page one."},
		},
		Markets: &provider.MarketRestrictions{
			Currencies: []string{"EUR"},
			Notes:      []string{"Search position comes from filters, not the market country. Prices exclude shipping and buyer fees."},
		},
		Access:       &provider.AccessRequirements{Authentication: provider.AuthenticationNone, Browser: provider.BrowserNone},
		Transport:    []provider.TransportNote{{Mode: provider.TransportHTTP, UseWhen: "Get the public search API and listing page."}},
		Warnings:     []provider.HelpWarning{{Code: "public_endpoint", Message: "Wallapop can change or limit its public endpoints."}},
		ProviderData: provider.Data{Name: jsonv1.RawMessage(`{"default_search_center":{"latitude":42.5063,"longitude":1.5218}}`)},
	}
	if err := help.Validate(); err != nil {
		return provider.HelpResult{}, fmt.Errorf("validate Wallapop help: %w", err)
	}
	return provider.HelpResult{Help: help}, nil
}

func searchFilterDefinitions() []provider.FilterDefinition {
	return []provider.FilterDefinition{
		{Key: "latitude", Type: provider.FilterTypeDecimal, Description: "Search-center latitude. Use with longitude.", Examples: []string{"42.5063"}, AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}},
		{Key: "longitude", Type: provider.FilterTypeDecimal, Description: "Search-center longitude. Use with latitude.", Examples: []string{"1.5218"}, AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}},
		{Key: "max_distance_km", Type: provider.FilterTypeDecimal, Description: "Keep listings within this straight-line distance.", Examples: []string{"100"}, AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}},
		{Key: "min_price", Type: provider.FilterTypeDecimal, Description: "Minimum displayed price in EUR.", Examples: []string{"500"}, AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}},
		{Key: "max_price", Type: provider.FilterTypeDecimal, Description: "Maximum displayed price in EUR.", Examples: []string{"2000"}, AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}},
		{Key: "category_id", Type: provider.FilterTypeInteger, Description: "Optional Wallapop category ID.", Examples: []string{"17000"}, AppliesTo: []provider.CapabilityName{provider.CapabilitySearch}},
	}
}

type searchOptions struct {
	latitude, longitude float64
	maxDistance         *float64
	minPrice, maxPrice  *float64
	categoryID          string
	orderBy             string
}

func parseSearchOptions(filters []provider.Filter, sortMode *provider.Sort) (searchOptions, error) {
	options := searchOptions{latitude: defaultLatitude, longitude: defaultLongitude, orderBy: "most_relevance"}
	var latitudeSet, longitudeSet bool
	if sortMode != nil {
		options.orderBy = strings.TrimSpace(sortMode.Value)
	}
	validSort := map[string]bool{"most_relevance": true, "closest": true, "newest": true, "price_low_to_high": true, "price_high_to_low": true}
	if !validSort[options.orderBy] {
		return options, provider.NewError(provider.ErrorCodeInvalidFilter, "the Wallapop sort value is not supported", nil)
	}
	seen := make(map[string]bool, len(filters))
	for _, filter := range filters {
		key, value := strings.TrimSpace(filter.Key), strings.TrimSpace(filter.Value)
		if seen[key] {
			return options, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("Wallapop filter %q cannot be repeated", key), nil)
		}
		seen[key] = true
		switch key {
		case "latitude":
			parsed, err := decimalFilter(key, value, -90, 90, true)
			if err != nil {
				return options, err
			}
			options.latitude, latitudeSet = parsed, true
		case "longitude":
			parsed, err := decimalFilter(key, value, -180, 180, true)
			if err != nil {
				return options, err
			}
			options.longitude, longitudeSet = parsed, true
		case "max_distance_km":
			parsed, err := decimalFilter(key, value, 0, math.MaxFloat64, false)
			if err != nil {
				return options, err
			}
			options.maxDistance = &parsed
		case "min_price":
			parsed, err := decimalFilter(key, value, 0, math.MaxFloat64, true)
			if err != nil {
				return options, err
			}
			options.minPrice = &parsed
		case "max_price":
			parsed, err := decimalFilter(key, value, 0, math.MaxFloat64, true)
			if err != nil {
				return options, err
			}
			options.maxPrice = &parsed
		case "category_id":
			parsed, err := strconv.ParseUint(value, 10, 64)
			if err != nil || parsed == 0 {
				return options, provider.NewError(provider.ErrorCodeInvalidFilter, "Wallapop category_id must be a positive integer", err)
			}
			options.categoryID = value
		default:
			return options, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("unknown Wallapop filter %q", key), nil)
		}
	}
	if latitudeSet != longitudeSet {
		return options, provider.NewError(provider.ErrorCodeInvalidFilter, "Wallapop latitude and longitude must be used together", nil)
	}
	if options.minPrice != nil && options.maxPrice != nil && *options.minPrice > *options.maxPrice {
		return options, provider.NewError(provider.ErrorCodeInvalidFilter, "Wallapop min_price must not exceed max_price", nil)
	}
	return options, nil
}

func decimalFilter(key, value string, minimum, maximum float64, inclusiveMinimum bool) (float64, error) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) || parsed > maximum || parsed < minimum || !inclusiveMinimum && parsed == minimum {
		return 0, provider.NewError(provider.ErrorCodeInvalidFilter, fmt.Sprintf("Wallapop %s has an invalid decimal value", key), err)
	}
	return parsed, nil
}

func (implementation) Search(ctx context.Context, request provider.SearchRequest) (provider.ProductPage, error) {
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidFilter, "the Wallapop search query is required", nil)
	}
	options, err := parseSearchOptions(request.Filters, request.Sort)
	if err != nil {
		return provider.ProductPage{}, err
	}
	page := request.Page.Number
	if page == 0 {
		page = 1
	}
	if page < 1 || page > 10 || request.Page.Size != 0 && request.Page.Size != defaultPageSize {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidFilter, "Wallapop supports pages 1 to 10 with page size 40", nil)
	}

	componentsQuery := []provider.RequestValue{{Name: "keywords", Values: []string{query}}, {Name: "order_by", Values: []string{options.orderBy}}, {Name: "source", Values: []string{"search_box"}}}
	if options.categoryID != "" {
		componentsQuery = append(componentsQuery, provider.RequestValue{Name: "category_id", Values: []string{options.categoryID}})
	}
	response, err := fetch(ctx, request.Request, apiBaseURL+"/components", componentsQuery, true)
	if err != nil {
		return provider.ProductPage{}, err
	}
	var components componentsDocument
	if err := decodeResponse(response, &components); err != nil {
		return provider.ProductPage{}, err
	}
	searchID, categoryID := components.searchParameters()
	if searchID == "" {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeHTTPFailure, "Wallapop search did not return a search ID", nil)
	}
	if categoryID == "" {
		categoryID = options.categoryID
	}

	var section sectionDocument
	nextPage := ""
	for currentPage := 1; currentPage <= page; currentPage++ {
		sectionQuery := []provider.RequestValue{
			{Name: "keywords", Values: []string{query}}, {Name: "order_by", Values: []string{options.orderBy}},
			{Name: "search_id", Values: []string{searchID}}, {Name: "latitude", Values: []string{strconv.FormatFloat(options.latitude, 'f', -1, 64)}},
			{Name: "longitude", Values: []string{strconv.FormatFloat(options.longitude, 'f', -1, 64)}}, {Name: "section_type", Values: []string{"organic_search_results"}},
			{Name: "source", Values: []string{"deep_link"}},
		}
		if categoryID != "" {
			sectionQuery = append(sectionQuery, provider.RequestValue{Name: "category_id", Values: []string{categoryID}})
		}
		if currentPage > 1 {
			if nextPage == "" {
				section = sectionDocument{}
				break
			}
			sectionQuery = append(sectionQuery, provider.RequestValue{Name: "next_page", Values: []string{nextPage}})
		}
		response, err = fetch(ctx, request.Request, apiBaseURL+"/section", sectionQuery, true)
		if err != nil {
			return provider.ProductPage{}, err
		}
		if currentPage == page {
			if err := decodeResponse(response, &section); err != nil {
				return provider.ProductPage{}, err
			}
			nextPage = section.Meta.NextPage
		} else {
			var cursor struct {
				Meta struct {
					NextPage string `json:"next_page"`
				} `json:"meta"`
			}
			if err := decodeResponse(response, &cursor); err != nil {
				return provider.ProductPage{}, err
			}
			nextPage = cursor.Meta.NextPage
		}
	}

	items := make([]provider.ProductSummary, 0, len(section.Data.Section.Items))
	failed := 0
	for _, raw := range section.Data.Section.Items {
		if raw.isReserved() {
			continue
		}
		item, itemErr := raw.summaryAt(response.RetrievedAt, options)
		if errors.Is(itemErr, errFiltered) {
			continue
		}
		if itemErr != nil {
			failed++
			continue
		}
		items = append(items, item)
	}
	warnings := currencyWarnings(request.Market.Currency, items...)
	if failed > 0 {
		warning := provider.NewWarning(provider.WarningCodePartialParsing, "Some Wallapop listings could not be parsed.", nil)
		found := len(section.Data.Section.Items)
		parsed := found - failed
		warning.FoundCount, warning.ParsedCount = &found, &parsed
		warnings = append(warnings, warning)
	}
	hasNext := nextPage != ""
	pageData, _ := json.Marshal(struct {
		SearchID string `json:"search_id,omitempty"`
		NextPage string `json:"next_page,omitempty"`
	}{SearchID: searchID, NextPage: nextPage})
	return provider.ProductPage{Items: items, Page: provider.PageInfo{Number: page, Size: defaultPageSize, HasNext: &hasNext, ProviderData: provider.Data{Name: pageData}}, Warnings: warnings}, nil
}

func (implementation) Item(ctx context.Context, request provider.ItemRequest) (provider.ItemResult, error) {
	slug, err := itemSlug(request.IDOrURL)
	if err != nil {
		return provider.ItemResult{}, err
	}
	response, err := fetch(ctx, request.Request, websiteBaseURL+"/item/"+slug, nil, false)
	if err != nil {
		return provider.ItemResult{}, err
	}
	pageProps, err := parsePageProps(responseDocument(response))
	if err != nil {
		return provider.ItemResult{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the Wallapop item page could not be parsed", err)
	}
	item, err := pageProps.detail(response.RetrievedAt)
	if err != nil {
		return provider.ItemResult{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the Wallapop item details are invalid", err)
	}
	return provider.ItemResult{Item: item, Warnings: currencyWarnings(request.Market.Currency, item.ProductSummary)}, nil
}

func itemSlug(value string) (string, error) {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() {
		slug, ok := strings.CutPrefix(parsed.Path, "/item/")
		if parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "es.wallapop.com") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !ok {
			return "", provider.NewError(provider.ErrorCodeInvalidFilter, "the item URL must be a Wallapop HTTPS item URL", nil)
		}
		value = slug
	}
	if !slugPattern.MatchString(value) {
		return "", provider.NewError(provider.ErrorCodeInvalidFilter, "the Wallapop item slug is invalid", nil)
	}
	return value, nil
}

func fetch(ctx context.Context, request provider.Request, target string, query []provider.RequestValue, api bool) (provider.ResourceResponse, error) {
	if err := ctx.Err(); err != nil {
		return provider.ResourceResponse{}, err
	}
	if err := validateRequest(request); err != nil {
		return provider.ResourceResponse{}, err
	}
	headers := []provider.RequestValue{
		{Name: "Accept", Values: []string{"application/json, text/plain, */*"}}, {Name: "Origin", Values: []string{websiteBaseURL}},
		{Name: "Referer", Values: []string{websiteBaseURL + "/"}}, {Name: "User-Agent", Values: []string{"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/131.0.0.0 Safari/537.36"}},
	}
	if api {
		headers = append(headers, provider.RequestValue{Name: "X-DeviceOS", Values: []string{"0"}})
	}
	response, err := request.Resources.Fetch(ctx, provider.ResourceRequest{
		Method: http.MethodGet, URL: target, Query: query, Headers: headers,
		Transport: provider.TransportPolicy{Required: provider.TransportHTTP}, Market: request.Market,
		Cache: request.Cache, Interactive: request.Interactive,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return response, err
		}
		if _, ok := provider.ErrorCodeOf(err); ok {
			return response, err
		}
		return response, provider.NewError(provider.ErrorCodeHTTPFailure, "the Wallapop resource request failed", err)
	}
	if response.StatusCode != http.StatusOK {
		return response, provider.NewError(provider.ErrorCodeHTTPFailure, "Wallapop returned an unsuccessful response", nil)
	}
	return response, nil
}

func validateRequest(request provider.Request) error {
	if err := validatePolicy(request); err != nil {
		return err
	}
	if request.Resources == nil {
		return provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the Wallapop resource service is required", nil)
	}
	if err := request.Market.Validate(); err != nil {
		return provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the Wallapop market is invalid", err)
	}
	return nil
}

func validatePolicy(request provider.Request) error {
	if request.Pricing.IncludeShipping {
		return provider.NewError(provider.ErrorCodeInvalidProviderConfig, "Wallapop cannot include shipping in displayed prices", nil)
	}
	return nil
}

func decodeResponse(response provider.ResourceResponse, target any) error {
	if err := json.Unmarshal(responseDocument(response), target); err != nil {
		return provider.NewError(provider.ErrorCodeHTTPFailure, "Wallapop returned invalid JSON", err)
	}
	return nil
}

func responseDocument(response provider.ResourceResponse) []byte {
	if response.Page != nil {
		return response.Page.HTML
	}
	return response.Body
}

type componentsDocument struct {
	Components []struct {
		Type     string `json:"type"`
		TypeData struct {
			QueryParams struct {
				SearchID   string            `json:"search_id"`
				CategoryID jsonv1.RawMessage `json:"category_id"`
			} `json:"query_params"`
		} `json:"type_data"`
	} `json:"components"`
}

func (document componentsDocument) searchParameters() (string, string) {
	for _, component := range document.Components {
		if component.Type == "search_results" {
			return component.TypeData.QueryParams.SearchID, rawText(component.TypeData.QueryParams.CategoryID)
		}
	}
	return "", ""
}

type sectionDocument struct {
	Data struct {
		Section struct {
			Items []wireItem `json:"items"`
		} `json:"section"`
	} `json:"data"`
	Meta struct {
		NextPage string `json:"next_page"`
	} `json:"meta"`
}

type wireItem struct {
	InternalID      string            `json:"id"`
	Title           jsonv1.RawMessage `json:"title"`
	Description     jsonv1.RawMessage `json:"description"`
	CategoryID      jsonv1.RawMessage `json:"category_id"`
	Price           wirePrice         `json:"price"`
	Images          []wireImage       `json:"images"`
	Reserved        jsonv1.RawMessage `json:"reserved"`
	Location        wireLocation      `json:"location"`
	Shipping        wireShipping      `json:"shipping"`
	WebSlug         string            `json:"web_slug"`
	Slug            string            `json:"slug"`
	CreatedAt       int64             `json:"created_at"`
	Condition       wireCondition     `json:"condition"`
	Characteristics jsonv1.RawMessage `json:"characteristics"`
}

type wirePrice struct {
	Amount   jsonv1.Number `json:"amount"`
	Currency string        `json:"currency"`
	Cash     *wirePrice    `json:"cash"`
}
type wireImage struct {
	URLs struct {
		Small  string `json:"small"`
		Medium string `json:"medium"`
		Big    string `json:"big"`
	} `json:"urls"`
}
type wireLocation struct {
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	PostalCode       string  `json:"postal_code"`
	PostalCodeCamel  string  `json:"postalCode"`
	City             string  `json:"city"`
	Region           string  `json:"region"`
	CountryCode      string  `json:"country_code"`
	CountryCodeCamel string  `json:"countryCode"`
}
type wireShipping struct {
	ItemIsShippable         bool `json:"item_is_shippable"`
	UserAllowsShipping      bool `json:"user_allows_shipping"`
	ItemIsShippableCamel    bool `json:"isItemShippable,omitzero"`
	UserAllowsShippingCamel bool `json:"isShippingAllowedByUser,omitzero"`
}
type wireCondition struct {
	Text  string `json:"text"`
	Value string `json:"value"`
}
type wireCharacteristic struct {
	Title string            `json:"title"`
	Key   string            `json:"key"`
	Value jsonv1.RawMessage `json:"value"`
	Text  string            `json:"text"`
}

func (item wireItem) isReserved() bool { return rawFlag(item.Reserved) }

type pageProps struct {
	Item     wireItem          `json:"item"`
	Seller   jsonv1.RawMessage `json:"itemSeller"`
	Delivery jsonv1.RawMessage `json:"itemDeliveryInfo"`
}

func parsePageProps(document []byte) (pageProps, error) {
	root, err := html.Parse(bytes.NewReader(document))
	if err != nil {
		return pageProps{}, err
	}
	var envelope struct {
		Props struct {
			PageProps pageProps `json:"pageProps"`
		} `json:"props"`
	}
	for nodes := []*html.Node{root}; len(nodes) > 0; {
		node := nodes[len(nodes)-1]
		nodes = nodes[:len(nodes)-1]
		if node.Type == html.ElementNode && node.Data == "script" {
			for _, attribute := range node.Attr {
				if attribute.Key == "id" && attribute.Val == "__NEXT_DATA__" {
					if node.FirstChild == nil {
						return pageProps{}, errors.New("NEXT_DATA script is empty")
					}
					if err := json.Unmarshal([]byte(node.FirstChild.Data), &envelope); err != nil {
						return pageProps{}, err
					}
					return envelope.Props.PageProps, nil
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			nodes = append(nodes, child)
		}
	}
	return pageProps{}, errors.New("NEXT_DATA script is missing")
}

func (props pageProps) detail(retrievedAt time.Time) (provider.ItemDetail, error) {
	item := props.Item
	options := searchOptions{latitude: defaultLatitude, longitude: defaultLongitude}
	summary, err := item.product(retrievedAt, options, provider.DetailLevelFull, &props)
	if err != nil {
		return provider.ItemDetail{}, err
	}
	var characteristics []wireCharacteristic
	_ = json.Unmarshal(item.Characteristics, &characteristics)
	for _, characteristic := range characteristics {
		name, value := characteristic.Title, rawText(characteristic.Value)
		if name == "" {
			name = characteristic.Key
		}
		if value == "" {
			value = characteristic.Text
		}
		if name != "" && value != "" {
			summary.Attributes = append(summary.Attributes, provider.Attribute{Name: name, Value: value})
		}
	}
	if item.Condition.Text != "" {
		summary.Attributes = append(summary.Attributes, provider.Attribute{Name: "Condition", Value: item.Condition.Text})
	}
	return provider.ItemDetail{ProductSummary: summary, Description: rawText(item.Description)}, nil
}

func (item wireItem) product(retrievedAt time.Time, options searchOptions, level provider.DetailLevel, props *pageProps) (provider.ProductSummary, error) {
	name, slug := rawText(item.Title), strings.TrimSpace(item.WebSlug)
	if slug == "" {
		slug = strings.TrimSpace(item.Slug)
	}
	if name == "" || !slugPattern.MatchString(slug) {
		return provider.ProductSummary{}, errors.New("listing title or slug is invalid")
	}
	price, err := item.Price.money()
	if err != nil {
		return provider.ProductSummary{}, err
	}
	distance := haversine(options.latitude, options.longitude, item.Location.Latitude, item.Location.Longitude)
	if options.maxDistance != nil && distance > *options.maxDistance {
		return provider.ProductSummary{}, errFiltered
	}
	priceValue, _ := strconv.ParseFloat(price.Amount, 64)
	if options.minPrice != nil && priceValue < *options.minPrice || options.maxPrice != nil && priceValue > *options.maxPrice {
		return provider.ProductSummary{}, errFiltered
	}
	data := wallapopData{
		InternalID: item.InternalID, City: item.Location.City, Region: item.Location.Region,
		Country: item.Location.country(), PostalCode: item.Location.postalCode(), CategoryID: rawText(item.CategoryID),
		Latitude: item.Location.Latitude, Longitude: item.Location.Longitude, DistanceKM: math.Round(distance*10) / 10,
		Shipping: item.Shipping.normalized(), CreatedAt: item.CreatedAt,
	}
	if props != nil {
		data.Seller, data.Delivery, data.Images = props.Seller, props.Delivery, imageURLs(item.Images)
	}
	providerSpecific, err := json.Marshal(data)
	if err != nil {
		return provider.ProductSummary{}, err
	}
	availability := provider.AvailabilityInStock
	if item.isReserved() {
		availability = provider.AvailabilityOutOfStock
	}
	result := provider.ProductSummary{ID: slug, URL: websiteBaseURL + "/item/" + slug, Name: name, Price: price, Availability: availability, ImageURL: firstImage(item.Images), RetrievedAt: retrievedAt, DetailLevel: level, ProviderData: provider.Data{Name: providerSpecific}}
	if err := result.Validate(); err != nil {
		return provider.ProductSummary{}, err
	}
	return result, nil
}

var errFiltered = errors.New("listing does not match local filters")

func (item wireItem) summaryAt(retrievedAt time.Time, options searchOptions) (provider.ProductSummary, error) {
	return item.product(retrievedAt, options, provider.DetailLevelSummary, nil)
}

type wallapopData struct {
	InternalID string            `json:"internal_id,omitempty"`
	City       string            `json:"city,omitempty"`
	Region     string            `json:"region,omitempty"`
	Country    string            `json:"country,omitempty"`
	PostalCode string            `json:"postal_code,omitempty"`
	CategoryID string            `json:"category_id,omitempty"`
	Latitude   float64           `json:"latitude"`
	Longitude  float64           `json:"longitude"`
	DistanceKM float64           `json:"distance_km"`
	Shipping   wireShipping      `json:"shipping"`
	CreatedAt  int64             `json:"created_at,omitzero"`
	Seller     jsonv1.RawMessage `json:"seller,omitempty"`
	Delivery   jsonv1.RawMessage `json:"delivery,omitempty"`
	Images     []string          `json:"images,omitempty"`
}

func (price wirePrice) money() (*provider.Money, error) {
	selected := price
	if price.Cash != nil {
		selected = *price.Cash
	}
	amount := normalizeDecimal(selected.Amount.String())
	currency := strings.ToUpper(strings.TrimSpace(selected.Currency))
	if amount == "" || currency == "" {
		return nil, errors.New("listing price is missing")
	}
	money := &provider.Money{Amount: amount, Currency: currency, Display: amount + " " + currency}
	if err := money.Validate(); err != nil {
		return nil, err
	}
	return money, nil
}

func normalizeDecimal(value string) string {
	if strings.Contains(value, ".") {
		value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
	}
	return value
}

func rawText(raw jsonv1.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var object struct {
		Original string `json:"original"`
	}
	if json.Unmarshal(raw, &object) == nil && object.Original != "" {
		return object.Original
	}
	var number jsonv1.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

func rawFlag(raw jsonv1.RawMessage) bool {
	var value bool
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var object struct {
		Flag bool `json:"flag"`
	}
	return json.Unmarshal(raw, &object) == nil && object.Flag
}

func firstImage(images []wireImage) string {
	if len(images) == 0 {
		return ""
	}
	return images[0].url()
}

func (image wireImage) url() string {
	return cmp.Or(image.URLs.Big, image.URLs.Medium, image.URLs.Small)
}

func imageURLs(images []wireImage) []string {
	result := make([]string, 0, len(images))
	for _, image := range images {
		if value := image.url(); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func (location wireLocation) country() string {
	return cmp.Or(location.CountryCode, location.CountryCodeCamel)
}

func (location wireLocation) postalCode() string {
	return cmp.Or(location.PostalCode, location.PostalCodeCamel)
}

func (shipping wireShipping) normalized() wireShipping {
	shipping.ItemIsShippable = shipping.ItemIsShippable || shipping.ItemIsShippableCamel
	shipping.UserAllowsShipping = shipping.UserAllowsShipping || shipping.UserAllowsShippingCamel
	shipping.ItemIsShippableCamel = false
	shipping.UserAllowsShippingCamel = false
	return shipping
}

func haversine(latitude1, longitude1, latitude2, longitude2 float64) float64 {
	const earthRadiusKM = 6371.0
	lat1, lat2 := latitude1*math.Pi/180, latitude2*math.Pi/180
	deltaLat, deltaLon := (latitude2-latitude1)*math.Pi/180, (longitude2-longitude1)*math.Pi/180
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	return 2 * earthRadiusKM * math.Asin(math.Sqrt(a))
}

func currencyWarnings(requested string, items ...provider.ProductSummary) []provider.Warning {
	requested = strings.ToUpper(strings.TrimSpace(requested))
	for _, item := range items {
		if item.Price != nil && item.Price.Currency != requested {
			warning := provider.NewWarning(provider.WarningCodeCurrencyUnavailable, "Wallapop returned EUR. No currency conversion was done.", nil)
			warning.RequestedCurrency, warning.ActualCurrency = requested, item.Price.Currency
			return []provider.Warning{warning}
		}
	}
	return nil
}

var (
	_ provider.HelpProvider    = implementation{}
	_ provider.ConfigValidator = implementation{}
	_ provider.SearchProvider  = implementation{}
	_ provider.ItemProvider    = implementation{}
)
