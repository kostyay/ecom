package cli

import (
	coreapp "github.com/kostyay/ecom/internal/app"
	"github.com/kostyay/ecom/internal/output"
	"github.com/spf13/cobra"
)

type brandsFlags struct {
	listingPageFlags
}

func (app *application) newBrandsCommand() *cobra.Command {
	flags := &brandsFlags{}
	command := &cobra.Command{
		Use: "brands [query]", Short: "List or search provider brands",
		Args:        app.dataCommandArgs(cobra.MaximumNArgs(1)),
		Annotations: map[string]string{requiresProviderAnnotation: "true", dataCommandAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) == 1 {
				query = args[0]
			}
			return app.runBrands(cmd, query, flags)
		},
	}
	addListingPageFlags(command, &flags.listingPageFlags)
	return command
}

func (app *application) runBrands(cmd *cobra.Command, query string, flags *brandsFlags) error {
	selection, err := commandOutputSelection(cmd)
	if err != nil {
		return &codedError{code: codeCommand, err: err}
	}
	services, err := app.commandServices(cmd.Context())
	if err != nil {
		return err
	}
	result, err := services.Brands(cmd.Context(), coreapp.BrandsInput{
		Query: query, Page: flags.page, PageSet: cmd.Flags().Changed("page"), PageSize: flags.pageSize, PageSizeSet: cmd.Flags().Changed("page-size"),
		Refresh: flags.refresh, StaleIfError: flags.staleIfError, Interactive: flags.interactive,
	})
	if err != nil {
		return err
	}
	envelope := output.NewSearchedListing(result.ProviderName, result.Market, result.Page.Items, result.Page.Page, result.Page.Warnings, result.Page.ProviderData, result.SearchMethod, result.Metadata)
	return output.Write(cmd.OutOrStdout(), envelope, selection)
}

type productListingFlags struct {
	filters []string
	sort    string
	listingPageFlags
}

type listingPageFlags struct {
	page         int
	pageSize     int
	refresh      bool
	staleIfError bool
	interactive  bool
}

func (app *application) newBrandItemsCommand() *cobra.Command {
	flags := &productListingFlags{}
	command := &cobra.Command{
		Use: "brand-items <brand-id>", Short: "List products for one provider brand",
		Args:        app.dataCommandArgs(cobra.ExactArgs(1)),
		Annotations: map[string]string{requiresProviderAnnotation: "true", dataCommandAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.runBrandItems(cmd, args[0], flags)
		},
	}
	addProductListingFlags(command, flags)
	return command
}

func (app *application) runBrandItems(cmd *cobra.Command, brandID string, flags *productListingFlags) error {
	selection, err := commandOutputSelection(cmd)
	if err != nil {
		return &codedError{code: codeCommand, err: err}
	}
	services, err := app.commandServices(cmd.Context())
	if err != nil {
		return err
	}
	result, err := services.BrandItems(cmd.Context(), coreapp.BrandItemsInput{
		BrandID: brandID, Filters: flags.filters, Sort: flags.sort,
		Page: flags.page, PageSet: cmd.Flags().Changed("page"), PageSize: flags.pageSize, PageSizeSet: cmd.Flags().Changed("page-size"),
		Refresh: flags.refresh, StaleIfError: flags.staleIfError, Interactive: flags.interactive,
	})
	if err != nil {
		return err
	}
	return output.Write(cmd.OutOrStdout(), output.NewProductListing(result.ProviderName, result.Market, result.Page, result.Metadata), selection)
}

func (app *application) newDealsCommand() *cobra.Command {
	flags := &productListingFlags{}
	command := &cobra.Command{
		Use: "deals", Short: "List provider-declared product deals",
		Args:        app.dataCommandArgs(cobra.NoArgs),
		Annotations: map[string]string{requiresProviderAnnotation: "true", dataCommandAnnotation: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return app.runDeals(cmd, flags)
		},
	}
	addProductListingFlags(command, flags)
	return command
}

func (app *application) runDeals(cmd *cobra.Command, flags *productListingFlags) error {
	selection, err := commandOutputSelection(cmd)
	if err != nil {
		return &codedError{code: codeCommand, err: err}
	}
	services, err := app.commandServices(cmd.Context())
	if err != nil {
		return err
	}
	result, err := services.Deals(cmd.Context(), coreapp.DealsInput{
		Filters: flags.filters, Sort: flags.sort,
		Page: flags.page, PageSet: cmd.Flags().Changed("page"), PageSize: flags.pageSize, PageSizeSet: cmd.Flags().Changed("page-size"),
		Refresh: flags.refresh, StaleIfError: flags.staleIfError, Interactive: flags.interactive,
	})
	if err != nil {
		return err
	}
	envelope := output.NewListing(result.ProviderName, result.Market, result.Page.Items, result.Page.Page, result.Page.Warnings, result.Page.ProviderData, result.Metadata)
	return output.Write(cmd.OutOrStdout(), envelope, selection)
}

func addProductListingFlags(command *cobra.Command, flags *productListingFlags) {
	command.Flags().StringArrayVar(&flags.filters, "filter", nil, "filter products with a provider key=value (repeatable)")
	command.Flags().StringVar(&flags.sort, "sort", "", "use a provider sort mode")
	addListingPageFlags(command, &flags.listingPageFlags)
}
