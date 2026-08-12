package cli

import (
	coreapp "github.com/kostyay/ecom/internal/app"
	"github.com/kostyay/ecom/internal/output"
	"github.com/spf13/cobra"
)

type categoryListFlags struct {
	parent    string
	recursive bool
	listingPageFlags
}

func (app *application) newCategoriesCommand() *cobra.Command {
	flags := &categoryListFlags{}
	command := &cobra.Command{
		Use: "categories [query]", Short: "List or search provider categories",
		Args:        app.dataCommandArgs(cobra.MaximumNArgs(1)),
		Annotations: map[string]string{requiresProviderAnnotation: "true", dataCommandAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) == 1 {
				query = args[0]
			}
			return app.runCategories(cmd, query, flags)
		},
	}
	command.Flags().StringVar(&flags.parent, "parent", "", "list direct children of this opaque category ID")
	command.Flags().BoolVar(&flags.recursive, "recursive", false, "list the recursive category tree")
	addListingPageFlags(command, &flags.listingPageFlags)
	return command
}

func (app *application) runCategories(cmd *cobra.Command, query string, flags *categoryListFlags) error {
	selection, err := commandOutputSelection(cmd)
	if err != nil {
		return &codedError{code: codeCommand, err: err}
	}
	services, err := app.commandServices(cmd.Context())
	if err != nil {
		return err
	}
	result, err := services.Categories(cmd.Context(), coreapp.CategoriesInput{
		Query: query, ParentID: flags.parent, Recursive: flags.recursive,
		Page: flags.page, PageSet: cmd.Flags().Changed("page"), PageSize: flags.pageSize, PageSizeSet: cmd.Flags().Changed("page-size"),
		Refresh: flags.refresh, StaleIfError: flags.staleIfError, Interactive: flags.interactive,
	})
	if err != nil {
		return err
	}
	envelope := output.NewSearchedListing(result.ProviderName, result.Market, result.Page.Items, result.Page.Page, result.Page.Warnings, result.Page.ProviderData, result.SearchMethod, result.Metadata)
	return output.Write(cmd.OutOrStdout(), envelope, selection)
}

func (app *application) newCategoryItemsCommand() *cobra.Command {
	flags := &productListingFlags{}
	command := &cobra.Command{
		Use: "category-items <category-id>", Short: "List products in one provider category",
		Args:        app.dataCommandArgs(cobra.ExactArgs(1)),
		Annotations: map[string]string{requiresProviderAnnotation: "true", dataCommandAnnotation: "true"},
		RunE:        func(cmd *cobra.Command, args []string) error { return app.runCategoryItems(cmd, args[0], flags) },
	}
	addProductListingFlags(command, flags)
	return command
}

func (app *application) runCategoryItems(cmd *cobra.Command, categoryID string, flags *productListingFlags) error {
	selection, err := commandOutputSelection(cmd)
	if err != nil {
		return &codedError{code: codeCommand, err: err}
	}
	services, err := app.commandServices(cmd.Context())
	if err != nil {
		return err
	}
	result, err := services.CategoryItems(cmd.Context(), coreapp.CategoryItemsInput{
		CategoryID: categoryID, Filters: flags.filters, Sort: flags.sort,
		Page: flags.page, PageSet: cmd.Flags().Changed("page"), PageSize: flags.pageSize, PageSizeSet: cmd.Flags().Changed("page-size"),
		Refresh: flags.refresh, StaleIfError: flags.staleIfError, Interactive: flags.interactive,
	})
	if err != nil {
		return err
	}
	return output.Write(cmd.OutOrStdout(), output.NewProductListing(result.ProviderName, result.Market, result.Page, result.Metadata), selection)
}

func addListingPageFlags(command *cobra.Command, flags *listingPageFlags) {
	command.Flags().IntVar(&flags.page, "page", 0, "select a provider page number")
	command.Flags().IntVar(&flags.pageSize, "page-size", 0, "select a provider-supported page size")
	command.Flags().BoolVar(&flags.refresh, "refresh", false, "bypass cached responses")
	command.Flags().BoolVar(&flags.staleIfError, "stale-if-error", false, "use an expired response if a fresh request fails")
	command.Flags().BoolVar(&flags.interactive, "interactive", false, "permit an interactive browser challenge")
}
