package cli

import (
	"errors"

	"github.com/kostyay/ecom/internal/output"
	"github.com/kostyay/ecom/provider"
	"github.com/spf13/cobra"
)

func (app *application) newCacheCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "cache", Short: "Maintain cached provider responses", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	command.AddCommand(app.newCacheClearCommand(), app.newCachePruneCommand())
	return command
}

func (app *application) newCacheClearCommand() *cobra.Command {
	return &cobra.Command{
		Use: "clear", Short: "Clear all or one provider's cached responses",
		Args:        app.dataCommandArgs(cobra.NoArgs),
		Annotations: map[string]string{dataCommandAnnotation: "true"},
		RunE:        app.runCacheClear,
	}
}

func (app *application) runCacheClear(cmd *cobra.Command, _ []string) error {
	selection, err := commandOutputSelection(cmd)
	if err != nil {
		return &codedError{code: codeCommand, err: err}
	}
	services, err := app.commandMaintenance(cmd.Context())
	if err != nil {
		return err
	}
	providerName, scoped := explicitProvider(cmd)
	var entries, bytes int64
	if scoped {
		result, clearErr := services.Maintenance.ClearProviderResponses(cmd.Context(), providerName)
		if clearErr != nil {
			return clearErr
		}
		entries, bytes = result.EntriesDeleted, result.BytesReleased
	} else {
		result, clearErr := services.Maintenance.ClearResponses(cmd.Context())
		if clearErr != nil {
			return clearErr
		}
		entries, bytes = result.EntriesDeleted, result.BytesReleased
	}
	scope := "all-providers"
	if scoped {
		scope = "provider"
	}
	data := output.ResponseMaintenanceData{
		Operation: "clear", Scope: scope, EntriesDeleted: entries, BytesReleased: bytes,
	}
	return output.Write(cmd.OutOrStdout(), output.New(providerName, provider.Market{}, data, nil, output.Metadata{}), selection)
}

func (app *application) newCachePruneCommand() *cobra.Command {
	return &cobra.Command{
		Use: "prune", Short: "Remove expired and least-recently-used cached responses",
		Args:        app.dataCommandArgs(cobra.NoArgs),
		Annotations: map[string]string{dataCommandAnnotation: "true"},
		RunE:        app.runCachePrune,
	}
}

func (app *application) runCachePrune(cmd *cobra.Command, _ []string) error {
	selection, err := commandOutputSelection(cmd)
	if err != nil {
		return &codedError{code: codeCommand, err: err}
	}
	if _, scoped := explicitProvider(cmd); scoped {
		return &codedError{code: codeCommand, err: errors.New("--provider cannot be used with cache prune")}
	}
	services, err := app.commandMaintenance(cmd.Context())
	if err != nil {
		return err
	}
	result, err := services.Maintenance.PruneResponses(cmd.Context())
	if err != nil {
		return err
	}
	data := output.ResponseMaintenanceData{
		Operation: "prune", Scope: "all-providers",
		EntriesDeleted: result.EntriesDeleted, BytesReleased: result.BytesReleased,
	}
	return output.Write(cmd.OutOrStdout(), output.New("", provider.Market{}, data, nil, output.Metadata{}), selection)
}

func (app *application) newProviderSessionCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "session", Short: "Maintain stored provider browser sessions", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	command.AddCommand(app.newProviderSessionClearCommand())
	return command
}

func (app *application) newProviderSessionClearCommand() *cobra.Command {
	return &cobra.Command{
		Use: "clear <provider>", Short: "Clear one provider browser session for the configured market",
		Args:        app.dataCommandArgs(cobra.ExactArgs(1)),
		Annotations: map[string]string{dataCommandAnnotation: "true"},
		RunE:        app.runProviderSessionClear,
	}
}

func (app *application) runProviderSessionClear(cmd *cobra.Command, args []string) error {
	selection, err := commandOutputSelection(cmd)
	if err != nil {
		return &codedError{code: codeCommand, err: err}
	}
	providerName, err := selectedProvider(cmd, args[0])
	if err != nil {
		return err
	}
	services, err := app.commandMaintenance(cmd.Context())
	if err != nil {
		return err
	}
	result, err := services.Maintenance.ClearSession(cmd.Context(), providerName, services.Market)
	if err != nil {
		return err
	}
	data := output.SessionMaintenanceData{Operation: "clear", Deleted: result.Deleted}
	return output.Write(cmd.OutOrStdout(), output.New(providerName, services.Market, data, nil, output.Metadata{}), selection)
}

func explicitProvider(cmd *cobra.Command) (string, bool) {
	flag := cmd.Flags().Lookup("provider")
	if flag == nil || !flag.Changed {
		return "", false
	}
	return flag.Value.String(), true
}
