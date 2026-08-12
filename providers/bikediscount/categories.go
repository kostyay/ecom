package bikediscount

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/kostyay/ecom/provider"
)

var (
	llmsCategoryLinkPattern = regexp.MustCompile(`^\s*\[([^\]]+)\]\((https://www\.bike-discount\.de/navigation/([0-9a-fA-F]{32}))\)\s*$`)
	navigationIDPattern     = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
)

type categoryExtraction struct {
	items    []provider.Category
	warnings []provider.Warning
}

// Categories lists Bike-Discount roots, direct children, or a flat recursive
// tree. ParentID is opaque. It is either an llms.txt navigation ID or an exact
// English canonical path returned by an earlier call.
func (implementation) Categories(ctx context.Context, request provider.CategoryListRequest) (provider.CategoryPage, error) {
	page, err := categoryPageInfo(request.Page)
	if err != nil {
		return provider.CategoryPage{}, err
	}

	rootsResponse, err := fetchResource(ctx, request.Request, resourceTarget{Path: "/llms.txt"})
	if err != nil {
		return provider.CategoryPage{}, err
	}
	roots := extractRootCategories(responseDocument(rootsResponse))
	if len(roots.items) == 0 {
		return provider.CategoryPage{}, provider.NewError(
			provider.ErrorCodeHTTPFailure, "the Bike-Discount root categories could not be parsed", nil,
		)
	}
	if strings.TrimSpace(request.ParentID) == "" && !request.Recursive {
		return provider.CategoryPage{Items: roots.items, Page: page, Warnings: roots.warnings}, nil
	}

	if strings.TrimSpace(request.ParentID) != "" {
		root, target, resolveErr := resolveCategoryParent(request.ParentID, roots.items)
		if resolveErr != nil {
			return provider.CategoryPage{}, resolveErr
		}
		extraction, fetchErr := fetchCategoryTree(ctx, request.Request, root, target)
		if fetchErr != nil {
			return provider.CategoryPage{}, fetchErr
		}
		if !request.Recursive {
			extraction.items = directCategoryChildren(extraction.items, root.ID)
		}
		extraction.warnings = append(roots.warnings, extraction.warnings...)
		return provider.CategoryPage{Items: extraction.items, Page: page, Warnings: extraction.warnings}, nil
	}

	items := append([]provider.Category(nil), roots.items...)
	warnings := append([]provider.Warning(nil), roots.warnings...)
	seen := make(map[string]struct{}, len(items))
	for _, root := range items {
		seen[root.ID] = struct{}{}
	}
	completed := 0
	for _, root := range roots.items {
		extraction, fetchErr := fetchCategoryTree(ctx, request.Request, root, resourceTarget{Path: "/navigation/" + root.ID})
		if fetchErr != nil {
			continue
		}
		completed++
		warnings = append(warnings, extraction.warnings...)
		for _, category := range extraction.items {
			if _, duplicate := seen[category.ID]; duplicate {
				continue
			}
			seen[category.ID] = struct{}{}
			items = append(items, category)
		}
	}
	if completed != len(roots.items) {
		warnings = append(warnings, partialCategoryWarning(
			"Some category trees could not be loaded.", len(roots.items), completed,
			errors.New("one or more root category requests failed"),
		))
	}
	return provider.CategoryPage{Items: items, Page: page, Warnings: warnings}, nil
}

// CategoryItems lists one page of products from an opaque category ID.
func (implementation) CategoryItems(ctx context.Context, request provider.CategoryItemsRequest) (provider.ProductPage, error) {
	categoryTarget, err := categoryResourceTarget(request.CategoryID)
	if err != nil {
		return provider.ProductPage{}, err
	}
	page, query, err := categoryItemQuery(request)
	if err != nil {
		return provider.ProductPage{}, err
	}
	categoryTarget.Query = query
	requestURL, err := bikeDiscountRequestURL(request.Market, categoryTarget)
	if err != nil {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the category URL is invalid", err)
	}
	response, err := fetchResource(ctx, request.Request, categoryTarget)
	if err != nil {
		return provider.ProductPage{}, err
	}
	if response.FinalURL != "" {
		requestURL = response.FinalURL
	}
	extraction, hasNext, err := extractListing(responseDocument(response), requestURL)
	if err != nil {
		return provider.ProductPage{}, provider.NewError(provider.ErrorCodeHTTPFailure, "the category product page could not be parsed", err)
	}
	page.HasNext = hasNext
	warnings := append([]provider.Warning(nil), extraction.Warnings...)
	warnings = append(warnings, currencyWarnings(request.Market.Currency, extraction.Products...)...)
	return provider.ProductPage{Items: extraction.Products, Page: page, Warnings: warnings}, nil
}

func extractRootCategories(document []byte) categoryExtraction {
	lines := strings.Split(string(document), "\n")
	items := make([]provider.Category, 0, len(lines))
	found := 0
	for _, line := range lines {
		if !strings.Contains(line, "](") {
			continue
		}
		found++
		match := llmsCategoryLinkPattern.FindStringSubmatch(line)
		if len(match) != 4 {
			continue
		}
		items = append(items, provider.Category{
			ID: strings.ToLower(match[3]), Name: normalizeText(match[1]), Path: normalizeText(match[1]),
			URL: match[2], HasChildren: true,
		})
	}
	result := categoryExtraction{items: items}
	if found > len(items) {
		result.warnings = append(result.warnings, partialCategoryWarning(
			"Some root category entries could not be parsed.", found, len(items),
			errors.New("one or more llms.txt links are malformed"),
		))
	}
	return result
}

func resolveCategoryParent(id string, roots []provider.Category) (provider.Category, resourceTarget, error) {
	id = strings.TrimSpace(id)
	if navigationIDPattern.MatchString(id) {
		id = strings.ToLower(id)
		for _, root := range roots {
			if root.ID == id {
				return root, resourceTarget{Path: "/navigation/" + id}, nil
			}
		}
		return provider.Category{}, resourceTarget{}, provider.NewError(
			provider.ErrorCodeInvalidFilter, "the category navigation ID is not present in llms.txt", nil,
		)
	}
	if validCategoryPath(id) {
		return provider.Category{ID: id, URL: bikeDiscountBaseURL + id}, resourceTarget{URL: bikeDiscountBaseURL + id}, nil
	}
	return provider.Category{}, resourceTarget{}, invalidCategoryID()
}

func categoryResourceTarget(id string) (resourceTarget, error) {
	id = strings.TrimSpace(id)
	if navigationIDPattern.MatchString(id) {
		return resourceTarget{Path: "/navigation/" + strings.ToLower(id)}, nil
	}
	if validCategoryPath(id) {
		return resourceTarget{URL: bikeDiscountBaseURL + id}, nil
	}
	return resourceTarget{}, invalidCategoryID()
}

func invalidCategoryID() error {
	return provider.NewError(
		provider.ErrorCodeInvalidFilter,
		"the category ID must be a root navigation ID or a returned English canonical path",
		nil,
	)
}

func validCategoryPath(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.Path == value &&
		strings.HasPrefix(parsed.Path, "/en/") && parsed.Path != "/en/" && !strings.Contains(parsed.Path, "//")
}

func fetchCategoryTree(ctx context.Context, request provider.Request, root provider.Category, target resourceTarget) (categoryExtraction, error) {
	pageURL, err := bikeDiscountRequestURL(request.Market, target)
	if err != nil {
		return categoryExtraction{}, provider.NewError(provider.ErrorCodeInvalidResourceRequest, "the category URL is invalid", err)
	}
	response, err := fetchResource(ctx, request, target)
	if err != nil {
		return categoryExtraction{}, err
	}
	if response.FinalURL != "" {
		pageURL = response.FinalURL
	}
	return extractCategoryTree(responseDocument(response), pageURL, root)
}

func extractCategoryTree(document []byte, pageURL string, rootCategory provider.Category) (categoryExtraction, error) {
	root, err := parseHTML(document)
	if err != nil {
		return categoryExtraction{}, fmt.Errorf("parse category HTML: %w", err)
	}
	navigation := firstDescendant(root, func(node *htmlNode) bool {
		return node.tag == "nav" && strings.EqualFold(normalizeText(node.attrs["aria-label"]), "Catalog")
	})
	if navigation == nil {
		return categoryExtraction{}, errors.New("catalog navigation is missing")
	}
	lists := directChildren(navigation, "ul")
	if len(lists) == 0 {
		return categoryExtraction{}, errors.New("catalog navigation list is missing")
	}

	seen := map[string]struct{}{rootCategory.ID: {}}
	items := make([]provider.Category, 0)
	found, parsed := 0, 0
	if validCategoryPath(rootCategory.ID) {
		if requestedNode, categoryPath := findCategoryListItem(navigation, pageURL, rootCategory.ID, nil); requestedNode != nil {
			rootCategory.Path = categoryPath
			walkCategoryNode(requestedNode, pageURL, rootCategory, true, seen, nil, &items, &found, &parsed)
			result := categoryExtraction{items: items}
			if found > parsed {
				result.warnings = append(result.warnings, partialCategoryWarning(
					"Some category entries could not be parsed.", found, parsed,
					errors.New("one or more category nodes are malformed, duplicated, or cyclic"),
				))
			}
			return result, nil
		}
	}
	for _, list := range lists {
		for _, item := range directChildren(list, "li") {
			walkCategoryNode(item, pageURL, rootCategory, true, seen, nil, &items, &found, &parsed)
		}
	}
	result := categoryExtraction{items: items}
	if found > parsed {
		result.warnings = append(result.warnings, partialCategoryWarning(
			"Some category entries could not be parsed.", found, parsed,
			errors.New("one or more category nodes are malformed, duplicated, or cyclic"),
		))
	}
	return result, nil
}

func findCategoryListItem(root *htmlNode, pageURL, categoryID string, ancestors []string) (*htmlNode, string) {
	for _, node := range root.children {
		if node.tag != "li" {
			if found, categoryPath := findCategoryListItem(node, pageURL, categoryID, ancestors); found != nil {
				return found, categoryPath
			}
			continue
		}
		link := firstDirectChild(node, "a")
		pathParts := ancestors
		if link != nil {
			name := nodeText(link)
			id, _ := canonicalCategoryIdentity(link.attrs["href"], pageURL)
			if name != "" && id != "" {
				pathParts = append(append([]string(nil), ancestors...), name)
				if id == categoryID {
					return node, strings.Join(pathParts, " / ")
				}
			}
		}
		if found, categoryPath := findCategoryListItem(node, pageURL, categoryID, pathParts); found != nil {
			return found, categoryPath
		}
	}
	return nil, ""
}

func walkCategoryNode(
	node *htmlNode,
	pageURL string,
	parent provider.Category,
	allowPageRoot bool,
	seen map[string]struct{},
	ancestors map[string]struct{},
	items *[]provider.Category,
	found, parsed *int,
) {
	*found++
	link := firstDirectChild(node, "a")
	if link == nil {
		return
	}
	name := nodeText(link)
	id, categoryURL := canonicalCategoryIdentity(link.attrs["href"], pageURL)
	if name == "" || id == "" {
		return
	}

	childLists := directChildren(node, "ul")
	isPageRoot := allowPageRoot && (id == parent.ID || (parent.Name != "" && strings.EqualFold(name, parent.Name)))
	if isPageRoot {
		*parsed++
		if parent.Name == "" {
			parent.Name = name
		}
		if parent.Path == "" {
			parent.Path = parent.Name
		}
		for _, list := range childLists {
			for _, child := range directChildren(list, "li") {
				walkCategoryNode(child, pageURL, parent, false, seen, map[string]struct{}{id: {}}, items, found, parsed)
			}
		}
		return
	}
	if _, cyclic := ancestors[id]; cyclic {
		return
	}
	if _, duplicate := seen[id]; duplicate {
		return
	}
	seen[id] = struct{}{}
	*parsed++
	path := name
	if parent.Path != "" {
		path = parent.Path + " / " + name
	}
	category := provider.Category{
		ID: id, Name: name, Path: path, ParentID: parent.ID, URL: categoryURL,
		HasChildren: len(childLists) > 0,
	}
	*items = append(*items, category)
	nextAncestors := cloneStringSet(ancestors)
	nextAncestors[id] = struct{}{}
	for _, list := range childLists {
		for _, child := range directChildren(list, "li") {
			walkCategoryNode(child, pageURL, category, false, seen, nextAncestors, items, found, parsed)
		}
	}
}

func canonicalCategoryIdentity(reference, pageURL string) (string, string) {
	absolute := absoluteURL(reference, pageURL)
	parsed, err := url.Parse(absolute)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "www.bike-discount.de") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ""
	}
	if !validCategoryPath(parsed.Path) {
		return "", ""
	}
	return parsed.Path, parsed.Scheme + "://" + parsed.Host + parsed.Path
}

func directChildren(node *htmlNode, tag string) []*htmlNode {
	children := make([]*htmlNode, 0)
	for _, child := range node.children {
		if child.tag == tag {
			children = append(children, child)
		}
	}
	return children
}

func firstDirectChild(node *htmlNode, tag string) *htmlNode {
	for _, child := range node.children {
		if child.tag == tag {
			return child
		}
	}
	return nil
}

func cloneStringSet(source map[string]struct{}) map[string]struct{} {
	clone := make(map[string]struct{}, len(source)+1)
	for key := range source {
		clone[key] = struct{}{}
	}
	return clone
}

func directCategoryChildren(items []provider.Category, parentID string) []provider.Category {
	children := make([]provider.Category, 0)
	for _, category := range items {
		if category.ParentID == parentID {
			children = append(children, category)
		}
	}
	return children
}

func categoryPageInfo(request provider.PageRequest) (provider.PageInfo, error) {
	number, size := request.Number, request.Size
	if number == 0 {
		number = 1
	}
	if size == 0 {
		size = defaultPageSize
	}
	if number < 1 || size != defaultPageSize {
		return provider.PageInfo{}, provider.NewError(
			provider.ErrorCodeInvalidFilter, "Bike-Discount pages start at 1 and support only page size 48", nil,
		)
	}
	return provider.PageInfo{Number: number, Size: size}, nil
}

func categoryItemQuery(request provider.CategoryItemsRequest) (provider.PageInfo, []provider.RequestValue, error) {
	return productListingQuery(request.Page, request.Sort, request.Filters)
}

func productListingQuery(pageRequest provider.PageRequest, sortMode *provider.Sort, filters []provider.Filter) (provider.PageInfo, []provider.RequestValue, error) {
	page, err := categoryPageInfo(pageRequest)
	if err != nil {
		return provider.PageInfo{}, nil, err
	}
	query := []provider.RequestValue{
		{Name: "p", Values: []string{strconv.Itoa(page.Number)}},
		{Name: "n", Values: []string{strconv.Itoa(page.Size)}},
	}
	if sortMode != nil {
		if sortMode.Value != "standard" {
			return provider.PageInfo{}, nil, provider.NewError(provider.ErrorCodeInvalidFilter, "the only verified product listing sort value is standard", nil)
		}
		query = append(query, provider.RequestValue{Name: "order", Values: []string{"standard"}})
	}

	manufacturer := ""
	properties := make([]string, 0)
	for _, filter := range filters {
		value := strings.ToLower(strings.TrimSpace(filter.Value))
		if !navigationIDPattern.MatchString(value) {
			return provider.PageInfo{}, nil, provider.NewError(provider.ErrorCodeInvalidFilter, "product listing filter values must be 32-character website IDs", nil)
		}
		switch filter.Key {
		case "manufacturer":
			if manufacturer != "" {
				return provider.PageInfo{}, nil, provider.NewError(provider.ErrorCodeInvalidFilter, "manufacturer cannot be repeated", nil)
			}
			manufacturer = value
		case "properties":
			properties = append(properties, value)
		default:
			return provider.PageInfo{}, nil, provider.NewError(provider.ErrorCodeInvalidFilter, "the product listing filter key is not supported", nil)
		}
	}
	if manufacturer != "" {
		query = append(query, provider.RequestValue{Name: "manufacturer", Values: []string{manufacturer}})
	}
	if len(properties) > 0 {
		query = append(query, provider.RequestValue{Name: "properties", Values: []string{strings.Join(properties, "|")}})
	}
	return page, query, nil
}

func listingHasNext(root *htmlNode) *bool {
	next := firstDescendant(root, func(node *htmlNode) bool {
		return node.tag == "a" && strings.EqualFold(normalizeText(node.attrs["rel"]), "next")
	})
	value := next != nil
	return &value
}

func partialCategoryWarning(message string, found, parsed int, cause error) provider.Warning {
	warning := provider.NewWarning(provider.WarningCodePartialParsing, message, cause)
	warning.FoundCount = &found
	warning.ParsedCount = &parsed
	return warning
}

var _ provider.CategoryListProvider = implementation{}
var _ provider.CategoryItemsProvider = implementation{}
