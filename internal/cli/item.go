package cli

import (
	coreapp "github.com/kostyay/ecom/internal/app"
	"github.com/kostyay/ecom/internal/output"
	"github.com/spf13/cobra"
)

type itemFlags struct {
	variants     []string
	refresh      bool
	staleIfError bool
	interactive  bool
}

func (app *application) newItemCommand() *cobra.Command {
	flags := &itemFlags{}
	command := &cobra.Command{
		Use: "item <item-id-or-url>", Short: "Get full details for one provider item",
		Args:        app.dataCommandArgs(cobra.ExactArgs(1)),
		Annotations: map[string]string{requiresProviderAnnotation: "true", dataCommandAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.runItem(cmd, args[0], flags)
		},
	}
	command.Flags().StringArrayVar(&flags.variants, "variant", nil, "select a variant with key=value (repeatable)")
	command.Flags().BoolVar(&flags.refresh, "refresh", false, "bypass cached responses")
	command.Flags().BoolVar(&flags.staleIfError, "stale-if-error", false, "use an expired response if a fresh request fails")
	command.Flags().BoolVar(&flags.interactive, "interactive", false, "permit an interactive browser challenge")
	return command
}

func (app *application) runItem(cmd *cobra.Command, idOrURL string, flags *itemFlags) error {
	selection, err := commandOutputSelection(cmd)
	if err != nil {
		return &codedError{code: codeCommand, err: err}
	}
	services, err := app.commandServices(cmd.Context())
	if err != nil {
		return err
	}
	result, err := services.Item(cmd.Context(), coreapp.ItemInput{
		IDOrURL: idOrURL, Variants: flags.variants,
		Refresh: flags.refresh, StaleIfError: flags.staleIfError, Interactive: flags.interactive,
	})
	if err != nil {
		return err
	}
	return output.Write(cmd.OutOrStdout(), output.NewItem(result.ProviderName, result.Market, result.Item, result.Metadata), selection)
}
