package output

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/kostyay/ecom/provider"
)

const (
	nameWidth        = 42
	descriptionWidth = 64
	urlWidth         = 54
)

// WriteTable writes a compact human-readable representation of an envelope.
// Table output is a presentation format and is not a stable machine contract.
func WriteTable(writer io.Writer, envelope Envelope) error {
	if writer == nil {
		return errors.New("output writer is required")
	}

	table := &tableRenderer{writer: tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)}
	table.metadata(envelope)

	switch data := envelope.Data.(type) {
	case provider.HelpResult:
		table.help(data.Help)
	case *provider.HelpResult:
		if data == nil {
			table.unsupported(nil)
		} else {
			table.help(data.Help)
		}
	case provider.Help:
		table.help(data)
	case *provider.Help:
		if data == nil {
			table.unsupported(nil)
		} else {
			table.help(*data)
		}
	case ListingData[provider.ProductSummary]:
		table.products(data.Items)
	case ListingData[provider.Category]:
		if data.SearchMethod != "" {
			table.line("Search method:\t%s", data.SearchMethod)
			table.line("")
		}
		table.categories(data.Items)
	case ListingData[provider.Brand]:
		if data.SearchMethod != "" {
			table.line("Search method:\t%s", data.SearchMethod)
			table.line("")
		}
		table.brands(data.Items)
	case ListingData[provider.Deal]:
		table.deals(data.Items)
	case ItemData:
		table.item(data.Item)
	case *ItemData:
		if data == nil {
			table.unsupported(nil)
		} else {
			table.item(data.Item)
		}
	case provider.FiltersResult:
		table.filters(data.Filters, data.SortModes)
	case *provider.FiltersResult:
		if data == nil {
			table.unsupported(nil)
		} else {
			table.filters(data.Filters, data.SortModes)
		}
	case ResponseMaintenanceData:
		table.responseMaintenance(data)
	case SessionMaintenanceData:
		table.sessionMaintenance(data)
	default:
		table.unsupported(data)
	}

	table.page(envelope.Page)
	table.warnings(envelope.Warnings)
	if err := table.writer.Flush(); err != nil {
		return fmt.Errorf("write table output: %w", err)
	}
	return nil
}

func (r *tableRenderer) responseMaintenance(data ResponseMaintenanceData) {
	r.line("Cache maintenance")
	r.line("Operation:\t%s", display(data.Operation))
	r.line("Scope:\t%s", display(data.Scope))
	r.line("Entries deleted:\t%d", data.EntriesDeleted)
	r.line("Bytes released:\t%d", data.BytesReleased)
}

func (r *tableRenderer) sessionMaintenance(data SessionMaintenanceData) {
	r.line("Session maintenance")
	r.line("Operation:\t%s", display(data.Operation))
	r.line("Deleted:\t%s", yesNo(data.Deleted))
}

type tableRenderer struct {
	writer *tabwriter.Writer
}

func (r *tableRenderer) line(format string, values ...any) {
	_, _ = fmt.Fprintf(r.writer, format+"\n", values...)
}

func (r *tableRenderer) metadata(envelope Envelope) {
	r.line("Provider:\t%s", display(envelope.Provider))
	r.line("Market:\t%s/%s/%s", display(envelope.Market.Country), display(envelope.Market.Language), display(envelope.Market.Currency))
	if envelope.Cache != nil {
		state := "fresh"
		if envelope.Cache.Hit {
			state = "hit"
		}
		if envelope.Cache.Stale {
			state += ", stale"
		}
		details := []string{state, "age " + durationLabel(envelope.Cache.AgeSeconds), "TTL " + durationLabel(envelope.Cache.TTLSeconds)}
		if envelope.Cache.ResourceCount > 1 {
			details = append(details, fmt.Sprintf("%d/%d hits", envelope.Cache.HitCount, envelope.Cache.ResourceCount))
		}
		r.line("Cache:\t%s", strings.Join(details, ", "))
	}
	r.line("")
}

func (r *tableRenderer) products(items []provider.ProductSummary) {
	r.line("ID\tNAME\tBRAND\tPRICE\tDISCOUNT\tAVAILABILITY\tURL")
	for _, item := range items {
		r.productRow(item)
	}
	if len(items) == 0 {
		r.line("(no products)")
	}
}

func (r *tableRenderer) productRow(item provider.ProductSummary) {
	r.line("%s\t%s\t%s\t%s\t%s\t%s\t%s",
		cell(item.ID, 18), cell(item.Name, nameWidth), cell(item.Brand, 20), cell(price(item), 24), cell(discount(item), 32), cell(availability(item.Availability, item.StockText), 32), cell(item.URL, urlWidth))
}

func (r *tableRenderer) deals(items []provider.Deal) {
	r.line("ID\tNAME\tBRAND\tPRICE\tDISCOUNT\tAVAILABILITY\tURL")
	for _, deal := range items {
		r.productRow(deal.Product)
	}
	if len(items) == 0 {
		r.line("(no deals)")
	}
}

func (r *tableRenderer) categories(items []provider.Category) {
	r.line("ID\tNAME\tPATH\tPARENT\tCHILDREN\tURL")
	for _, item := range items {
		r.line("%s\t%s\t%s\t%s\t%s\t%s", cell(item.ID, 18), cell(item.Name, nameWidth), cell(item.Path, nameWidth), cell(item.ParentID, 18), yesNo(item.HasChildren), cell(item.URL, urlWidth))
	}
	if len(items) == 0 {
		r.line("(no categories)")
	}
}

func (r *tableRenderer) brands(items []provider.Brand) {
	r.line("ID\tNAME\tURL")
	for _, item := range items {
		r.line("%s\t%s\t%s", cell(item.ID, 18), cell(item.Name, nameWidth), cell(item.URL, urlWidth))
	}
	if len(items) == 0 {
		r.line("(no brands)")
	}
}

func (r *tableRenderer) item(item provider.ItemDetail) {
	r.line("Item")
	r.line("ID:\t%s", display(item.ID))
	r.line("Name:\t%s", cell(item.Name, descriptionWidth))
	r.line("Brand:\t%s", display(item.Brand))
	r.line("Price:\t%s", price(item.ProductSummary))
	r.line("Discount:\t%s", discount(item.ProductSummary))
	r.line("Availability:\t%s", availability(item.Availability, item.StockText))
	r.line("URL:\t%s", cell(item.URL, urlWidth))
	if item.ImageURL != "" {
		r.line("Image:\t%s", cell(item.ImageURL, urlWidth))
	}
	if item.Description != "" {
		r.line("Description:\t%s", cell(item.Description, descriptionWidth))
	}
	if item.SelectedVariant != nil {
		variant := item.SelectedVariant
		r.line("Selected variant:\t%s; %s; %s; %s", display(variant.ID), cell(attributes(variant.Attributes), nameWidth), money(variant.Price), availability(variant.Availability, variant.StockText))
	}
	if len(item.Attributes) > 0 {
		r.line("")
		r.line("Attributes")
		r.line("NAME\tVALUE")
		for _, attribute := range item.Attributes {
			r.line("%s\t%s", cell(attribute.Name, 28), cell(attribute.Value, descriptionWidth))
		}
	}
	if len(item.Variants) > 0 {
		r.line("")
		r.line("Variants")
		r.line("ID\tATTRIBUTES\tPRICE\tAVAILABILITY\tSELECTED")
		for _, variant := range item.Variants {
			r.line("%s\t%s\t%s\t%s\t%s", cell(variant.ID, 18), cell(attributes(variant.Attributes), nameWidth), money(variant.Price), availability(variant.Availability, variant.StockText), yesNo(variant.Selected))
		}
	}
}

func (r *tableRenderer) help(help provider.Help) {
	name := help.DisplayName
	if name == "" {
		name = help.Name
	}
	r.line("Provider Help")
	r.line("Name:\t%s", display(name))
	if help.Description != "" {
		r.line("Description:\t%s", cell(help.Description, descriptionWidth))
	}

	r.line("")
	r.line("Capabilities")
	r.line("NAME\tSUPPORTED\tDESCRIPTION / NOTES")
	for _, capability := range help.Capabilities {
		notes := joinDetails(capability.Description, capability.Notes)
		r.line("%s\t%s\t%s", capability.Name, yesNo(capability.Supported), cell(notes, descriptionWidth))
	}
	if len(help.Capabilities) == 0 {
		r.line("(none)")
	}

	if help.Search != nil {
		r.line("")
		r.line("Search")
		r.line("Query required:\t%s", yesNo(help.Search.QueryRequired))
		r.line("Syntax:\t%s", display(help.Search.Syntax))
		if len(help.Search.Examples) > 0 {
			r.line("Examples:\t%s", cell(strings.Join(help.Search.Examples, "; "), descriptionWidth))
		}
	}
	r.filters(help.Filters, help.SortModes)
	r.pagination(help.Pagination)
	r.markets(help.Markets)
	r.access(help.Access, help.Transport)
	if len(help.Warnings) > 0 {
		r.line("")
		r.line("Access notes")
		for _, warning := range help.Warnings {
			r.line("%s:\t%s", display(warning.Code), cell(warning.Message, descriptionWidth))
		}
	}
}

func (r *tableRenderer) filters(filters []provider.FilterDefinition, sorts []provider.SortMode) {
	if len(filters) > 0 {
		r.line("")
		r.line("Filters")
		r.line("KEY\tTYPE\tREPEATABLE\tVALUES\tAPPLIES TO\tDESCRIPTION")
		for _, filter := range filters {
			values := make([]string, 0, len(filter.AllowedValues))
			for _, value := range filter.AllowedValues {
				label := value.Value
				if value.Label != "" && value.Label != value.Value {
					label += " (" + value.Label + ")"
				}
				values = append(values, label)
			}
			r.line("%s\t%s\t%s\t%s\t%s\t%s", cell(filter.Key, 24), filter.Type, yesNo(filter.Repeatable), cell(strings.Join(values, ", "), nameWidth), capabilities(filter.AppliesTo), cell(filter.Description, descriptionWidth))
		}
	}
	if len(sorts) > 0 {
		r.line("")
		r.line("Sort modes")
		r.line("VALUE\tLABEL\tAPPLIES TO\tDESCRIPTION")
		for _, sortMode := range sorts {
			r.line("%s\t%s\t%s\t%s", cell(sortMode.Value, 24), cell(sortMode.Label, 24), capabilities(sortMode.AppliesTo), cell(sortMode.Description, descriptionWidth))
		}
	}
	if len(filters) == 0 && len(sorts) == 0 {
		r.line("(no filters or sort modes)")
	}
}

func (r *tableRenderer) pagination(page *provider.PaginationHelp) {
	if page == nil {
		return
	}
	r.line("")
	r.line("Pagination")
	r.line("Mode:\t%s", page.Mode)
	r.line("First page:\t%d", page.FirstPage)
	r.line("Default page size:\t%d", page.DefaultPageSize)
	if len(page.SupportedPageSizes) > 0 {
		values := make([]string, len(page.SupportedPageSizes))
		for i, size := range page.SupportedPageSizes {
			values[i] = strconv.Itoa(size)
		}
		r.line("Page sizes:\t%s", strings.Join(values, ", "))
	}
	r.line("Reports totals:\titems=%s, pages=%s", yesNo(page.ReportsTotalItems), yesNo(page.ReportsTotalPages))
}

func (r *tableRenderer) markets(markets *provider.MarketRestrictions) {
	if markets == nil {
		return
	}
	r.line("")
	r.line("Markets")
	r.line("Countries:\t%s", list(markets.Countries))
	r.line("Languages:\t%s", list(markets.Languages))
	r.line("Currencies:\t%s", list(markets.Currencies))
}

func (r *tableRenderer) access(access *provider.AccessRequirements, transport []provider.TransportNote) {
	if access == nil && len(transport) == 0 {
		return
	}
	r.line("")
	r.line("Access")
	if access != nil {
		r.line("Authentication:\t%s", access.Authentication)
		r.line("Browser:\t%s", access.Browser)
		r.line("CDP:\t%s", yesNo(access.SupportsCDP))
		r.line("Interactive:\t%s", yesNo(access.SupportsInteractive))
		if len(access.Notes) > 0 {
			r.line("Notes:\t%s", cell(strings.Join(access.Notes, "; "), descriptionWidth))
		}
	}
	for _, note := range transport {
		r.line("%s:\t%s", note.Mode, cell(joinDetails(note.UseWhen, note.Notes), descriptionWidth))
	}
}

func (r *tableRenderer) page(page *provider.PageInfo) {
	if page == nil {
		return
	}
	parts := make([]string, 0, 5)
	if page.Number != 0 {
		parts = append(parts, "page "+strconv.Itoa(page.Number))
	}
	if page.Size != 0 {
		parts = append(parts, "size "+strconv.Itoa(page.Size))
	}
	if page.TotalItems != nil {
		parts = append(parts, "items "+strconv.Itoa(*page.TotalItems))
	}
	if page.TotalPages != nil {
		parts = append(parts, "pages "+strconv.Itoa(*page.TotalPages))
	}
	if page.HasNext != nil {
		parts = append(parts, "next "+yesNo(*page.HasNext))
	}
	if len(parts) > 0 {
		r.line("")
		r.line("Page:\t%s", strings.Join(parts, ", "))
	}
}

func (r *tableRenderer) warnings(warnings []provider.Warning) {
	if len(warnings) == 0 {
		return
	}
	r.line("")
	r.line("Warnings")
	r.line("CODE\tMESSAGE\tITEM")
	for _, warning := range warnings {
		item := warning.ItemID
		if item == "" {
			item = warning.URL
		}
		if warning.FoundCount != nil || warning.ParsedCount != nil {
			counts := make([]string, 0, 2)
			if warning.FoundCount != nil {
				counts = append(counts, fmt.Sprintf("found %d", *warning.FoundCount))
			}
			if warning.ParsedCount != nil {
				counts = append(counts, fmt.Sprintf("parsed %d", *warning.ParsedCount))
			}
			item = strings.TrimSpace(strings.Join([]string{item, "(" + strings.Join(counts, ", ") + ")"}, " "))
		}
		if warning.RequestedCurrency != "" || warning.ActualCurrency != "" {
			item = strings.TrimSpace(strings.Join([]string{item, "(" + warning.RequestedCurrency + " → " + warning.ActualCurrency + ")"}, " "))
		}
		r.line("%s\t%s\t%s", warning.Code, cell(warning.Message, descriptionWidth), cell(item, urlWidth))
	}
}

func (r *tableRenderer) unsupported(data any) {
	if data == nil {
		r.line("(no data)")
		return
	}
	r.line("DATA\t%s", cell(fmt.Sprint(data), descriptionWidth))
}

func price(product provider.ProductSummary) string {
	if product.Price != nil {
		return money(product.Price)
	}
	if product.PriceRange != nil {
		return product.PriceRange.Minimum.Display + " – " + product.PriceRange.Maximum.Display
	}
	return "—"
}

func money(value *provider.Money) string {
	if value == nil {
		return "—"
	}
	if value.Display != "" {
		return value.Display
	}
	return strings.TrimSpace(value.Amount + " " + value.Currency)
}

func discount(product provider.ProductSummary) string {
	parts := make([]string, 0, 3)
	if product.DiscountPercent != nil {
		parts = append(parts, strconv.Itoa(*product.DiscountPercent)+"%")
	}
	if product.DiscountAmount != nil {
		parts = append(parts, "save "+money(product.DiscountAmount))
	}
	if product.OriginalPrice != nil {
		parts = append(parts, "was "+money(product.OriginalPrice))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ", ")
}

func availability(value provider.Availability, stockText string) string {
	if stockText == "" {
		return display(string(value))
	}
	if value == "" || value == provider.AvailabilityUnknown {
		return stockText
	}
	return string(value) + " (" + stockText + ")"
}

func attributes(values []provider.Attribute) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, value.Name+"="+value.Value)
	}
	return strings.Join(parts, ", ")
}

func capabilities(values []provider.CapabilityName) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = string(value)
	}
	return cell(strings.Join(parts, ", "), nameWidth)
}

func joinDetails(description string, notes []string) string {
	parts := make([]string, 0, len(notes)+1)
	if description != "" {
		parts = append(parts, description)
	}
	parts = append(parts, notes...)
	return strings.Join(parts, "; ")
}

func display(value string) string {
	if value == "" {
		return "—"
	}
	return strings.NewReplacer("\t", " ", "\r", " ", "\n", " ").Replace(value)
}

func cell(value string, width int) string {
	value = display(value)
	if utf8.RuneCountInString(value) <= width {
		return value
	}
	runes := []rune(value)
	return string(runes[:width-1]) + "…"
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func list(values []string) string {
	if len(values) == 0 {
		return "any"
	}
	return strings.Join(values, ", ")
}

func durationLabel(seconds int64) string {
	if seconds <= 0 {
		return "0s"
	}
	return (time.Duration(seconds) * time.Second).String()
}
