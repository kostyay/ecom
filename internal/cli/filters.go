package cli

import (
	coreapp "github.com/kostyay/ecom/internal/app"
	"github.com/kostyay/ecom/internal/output"
	"github.com/kostyay/ecom/provider"
	"github.com/spf13/cobra"
)

type filtersFlags struct {
	categoryID   string
	brandID      string
	refresh      bool
	staleIfError bool
	interactive  bool
}

func (app *application) newFiltersCommand() *cobra.Command {
	flags := &filtersFlags{}
	command := &cobra.Command{
		Use: "filters [capability]", Short: "List provider filters and sort modes",
		Args:        app.dataCommandArgs(cobra.MaximumNArgs(1)),
		Annotations: map[string]string{requiresProviderAnnotation: "true", dataCommandAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			capability := provider.CapabilityName("")
			if len(args) == 1 {
				capability = provider.CapabilityName(args[0])
			}
			return app.runFilters(cmd, capability, flags)
		},
	}
	command.Flags().StringVar(&flags.categoryID, "category", "", "use one provider category context")
	command.Flags().StringVar(&flags.brandID, "brand", "", "use one provider brand context")
	command.Flags().BoolVar(&flags.refresh, "refresh", false, "bypass cached responses")
	command.Flags().BoolVar(&flags.staleIfError, "stale-if-error", false, "use an expired response if a fresh request fails")
	command.Flags().BoolVar(&flags.interactive, "interactive", false, "permit an interactive browser challenge")
	return command
}

func (app *application) runFilters(cmd *cobra.Command, capability provider.CapabilityName, flags *filtersFlags) error {
	selection, err := commandOutputSelection(cmd)
	if err != nil {
		return &codedError{code: codeCommand, err: err}
	}
	services, err := app.commandServices(cmd.Context())
	if err != nil {
		return err
	}
	result, err := services.Filters(cmd.Context(), coreapp.FiltersInput{
		Capability: capability, CategoryID: flags.categoryID, BrandID: flags.brandID,
		Refresh: flags.refresh, StaleIfError: flags.staleIfError, Interactive: flags.interactive,
	})
	if err != nil {
		return err
	}
	envelope := output.New(result.ProviderName, result.Market, result.Result, result.Result.Warnings, result.Metadata)
	return output.Write(cmd.OutOrStdout(), envelope, selection)
}
