// Package cli defines the ecom command-line interface.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	coreapp "github.com/kostyay/ecom/internal/app"
	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/internal/logging"
	"github.com/kostyay/ecom/internal/output"
	"github.com/kostyay/ecom/internal/version"
	"github.com/kostyay/ecom/provider"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	appName = "ecom"

	codeCommand = "command_error"
	codeConfig  = "config_error"
	codeLog     = "log_error"

	requiresProviderAnnotation = "ecom.requires-provider"
	dataCommandAnnotation      = "ecom.data-command"
)

type application struct {
	viper           *viper.Viper
	settings        config.Settings
	serviceFactory  *coreapp.Factory
	services        *coreapp.Services
	maintenance     *coreapp.MaintenanceServices
	logCloser       io.Closer
	logger          *slog.Logger
	servicesOnce    sync.Once
	servicesErr     error
	maintenanceOnce sync.Once
	maintenanceErr  error
	closeOnce       sync.Once
	closeErr        error
	dataErrorsJSON  bool
}

type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string {
	return e.err.Error()
}

func (e *codedError) Unwrap() error {
	return e.err
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Run runs the CLI and returns its process exit status.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return run(ctx, args, stdout, stderr, coreapp.NewFactory(provider.Resolve))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, serviceFactory *coreapp.Factory) int {
	app := &application{
		viper:          viper.New(),
		serviceFactory: serviceFactory,
	}
	cmd := app.newRootCommand(stdout, stderr)
	cmd.SetArgs(args)

	err := cmd.ExecuteContext(ctx)
	err = errors.Join(err, app.close())
	if err != nil && app.logger != nil {
		app.logger.ErrorContext(ctx, "command stopped", "error", err)
	}
	if app.logCloser != nil {
		err = errors.Join(err, app.logCloser.Close())
	}
	if err == nil {
		return 0
	}
	writeError(stderr, wantsJSON(args) || app.dataErrorsJSON, err)

	return 1
}

func (app *application) newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:           appName,
		Short:         "Utilities for e-commerce sites",
		Long:          "ecom is a machine-readable command-line platform for e-commerce utilities.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: app.initialize,
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetFlagErrorFunc(func(command *cobra.Command, err error) error {
		if command.Annotations[dataCommandAnnotation] == "true" {
			selection, selectionErr := commandOutputSelection(command)
			app.dataErrorsJSON = selectionErr != nil || selection.Mode != output.ModeTable
		}
		return &codedError{code: codeCommand, err: err}
	})

	flags := cmd.PersistentFlags()
	flags.Bool("json", false, "write machine-readable JSON")
	flags.StringP("output", "o", "", "set data output format: json, table, or jsonpath=<template>")
	flags.String("config", "", "read configuration from this file")
	flags.String("provider", "", "use this commerce provider")
	flags.String("log-level", "", "set the log level: debug, info, warn, or error")
	flags.String("log-file", "", "write logs to this file")

	cmd.AddCommand(
		newVersionCommand(), app.newProviderCommand(), app.newSearchCommand(),
		app.newCategoriesCommand(), app.newCategoryItemsCommand(),
		app.newBrandsCommand(), app.newBrandItemsCommand(), app.newDealsCommand(),
		app.newFiltersCommand(), app.newItemCommand(), app.newCacheCommand(),
	)

	return cmd
}

func (app *application) newSearchCommand() *cobra.Command {
	flags := &productListingFlags{}
	command := &cobra.Command{
		Use:         "search <query>",
		Short:       "Search one provider for products",
		Args:        app.dataCommandArgs(cobra.ExactArgs(1)),
		Annotations: map[string]string{requiresProviderAnnotation: "true", dataCommandAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.runSearch(cmd, args[0], flags)
		},
	}
	addProductListingFlags(command, flags)
	return command
}

func (app *application) runSearch(cmd *cobra.Command, query string, flags *productListingFlags) error {
	selection, err := commandOutputSelection(cmd)
	if err != nil {
		return &codedError{code: codeCommand, err: err}
	}
	services, err := app.commandServices(cmd.Context())
	if err != nil {
		return err
	}
	result, err := services.Search(cmd.Context(), coreapp.SearchInput{
		Query: query, Filters: flags.filters, Sort: flags.sort,
		Page: flags.page, PageSet: cmd.Flags().Changed("page"), PageSize: flags.pageSize, PageSizeSet: cmd.Flags().Changed("page-size"),
		Refresh: flags.refresh, StaleIfError: flags.staleIfError, Interactive: flags.interactive,
	})
	if err != nil {
		return err
	}
	envelope := output.NewProductListing(result.ProviderName, result.Market, result.Page, result.Metadata)
	return output.Write(cmd.OutOrStdout(), envelope, selection)
}

func (app *application) newProviderCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "provider",
		Short: "Inspect compiled commerce providers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	command.AddCommand(app.newProviderHelpCommand(), app.newProviderSessionCommand())
	return command
}

func (app *application) newProviderHelpCommand() *cobra.Command {
	return &cobra.Command{
		Use:         "help <provider>",
		Short:       "Show provider capabilities and usage details",
		Args:        app.dataCommandArgs(cobra.ExactArgs(1)),
		Annotations: map[string]string{dataCommandAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.runProviderHelp(cmd, args[0])
		},
	}
}

func (app *application) dataCommandArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		selection, err := commandOutputSelection(cmd)
		app.dataErrorsJSON = err != nil || selection.Mode != output.ModeTable
		if err != nil {
			return &codedError{code: codeCommand, err: err}
		}
		return validate(cmd, args)
	}
}

func (app *application) runProviderHelp(cmd *cobra.Command, positionalProvider string) error {
	selection, err := commandOutputSelection(cmd)
	if err != nil {
		return &codedError{code: codeCommand, err: err}
	}
	providerName, err := selectedProvider(cmd, positionalProvider)
	if err != nil {
		return err
	}
	if app.serviceFactory == nil {
		return errors.New("application service factory is required")
	}
	result, err := app.serviceFactory.ProviderHelp(cmd.Context(), app.settings, providerName, app.logger)
	if err != nil {
		return err
	}
	envelope := output.New(result.ProviderName, result.Market, result.Data, nil, output.Metadata{})
	if err := output.Write(cmd.OutOrStdout(), envelope, selection); err != nil {
		return err
	}
	return nil
}

func selectedProvider(cmd *cobra.Command, positional string) (string, error) {
	providerFlag := cmd.Flags().Lookup("provider")
	if providerFlag != nil && providerFlag.Changed && providerFlag.Value.String() != positional {
		return "", provider.NewError(
			provider.ErrorCodeProviderConflict,
			fmt.Sprintf("provider argument %q conflicts with --provider %q", positional, providerFlag.Value.String()),
			nil,
		)
	}
	return positional, nil
}

func commandOutputSelection(cmd *cobra.Command) (output.Selection, error) {
	value, err := cmd.Flags().GetString("output")
	if err != nil {
		return output.Selection{}, fmt.Errorf("read output flag: %w", err)
	}
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return output.Selection{}, fmt.Errorf("read JSON flag: %w", err)
	}
	if jsonOutput && value != "" && value != string(output.ModeJSON) {
		return output.Selection{}, errors.New("--json cannot be combined with a non-JSON output format")
	}
	return output.ParseMode(value)
}

func (app *application) initialize(cmd *cobra.Command, _ []string) error {
	flags := cmd.Flags()
	configFile, err := flags.GetString("config")
	if err != nil {
		return &codedError{code: codeConfig, err: fmt.Errorf("read config flag: %w", err)}
	}

	if err := app.viper.BindPFlag("log.level", flags.Lookup("log-level")); err != nil {
		return &codedError{code: codeConfig, err: fmt.Errorf("bind log level flag: %w", err)}
	}
	if err := app.viper.BindPFlag("log.file", flags.Lookup("log-file")); err != nil {
		return &codedError{code: codeConfig, err: fmt.Errorf("bind log file flag: %w", err)}
	}
	if err := app.viper.BindPFlag("provider", flags.Lookup("provider")); err != nil {
		return &codedError{code: codeConfig, err: fmt.Errorf("bind provider flag: %w", err)}
	}

	settings, err := config.Load(app.viper, configFile)
	if err != nil {
		return &codedError{code: codeConfig, err: err}
	}

	logger, closer, err := logging.New(settings.Log)
	if err != nil {
		return &codedError{code: codeLog, err: err}
	}
	app.logger = logger
	app.logCloser = closer
	app.settings = settings
	slog.SetDefault(logger)
	logger.InfoContext(cmd.Context(), "command started", "command", cmd.CommandPath())

	if cmd.Annotations[requiresProviderAnnotation] == "true" {
		_, err := app.commandServices(cmd.Context())
		return err
	}
	return nil
}

// commandServices lazily validates the provider and creates the Core services
// that provider commands use. Provider-free commands do not call this method.
func (app *application) commandServices(ctx context.Context) (*coreapp.Services, error) {
	app.servicesOnce.Do(func() {
		if app.serviceFactory == nil {
			app.servicesErr = errors.New("application service factory is required")
			return
		}
		app.services, app.servicesErr = app.serviceFactory.NewServices(
			ctx, app.settings, app.settings.Provider, app.logger,
		)
	})
	return app.services, app.servicesErr
}

func (app *application) commandMaintenance(ctx context.Context) (*coreapp.MaintenanceServices, error) {
	app.maintenanceOnce.Do(func() {
		if app.serviceFactory == nil {
			app.maintenanceErr = errors.New("application service factory is required")
			return
		}
		app.maintenance, app.maintenanceErr = app.serviceFactory.NewMaintenanceServices(ctx, app.settings)
	})
	return app.maintenance, app.maintenanceErr
}

func (app *application) close() error {
	app.closeOnce.Do(func() {
		if app.services != nil {
			app.closeErr = app.services.Close()
		}
		if app.maintenance != nil {
			app.closeErr = errors.Join(app.closeErr, app.maintenance.Close())
		}
	})
	return app.closeErr
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := version.Current()
			jsonOutput, err := cmd.Flags().GetBool("json")
			if err != nil {
				return &codedError{code: codeCommand, err: fmt.Errorf("read JSON flag: %w", err)}
			}
			if jsonOutput {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(info); err != nil {
					return fmt.Errorf("write version JSON: %w", err)
				}
				return nil
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s %s\nGo: %s\n", appName, info.Version, info.GoVersion)
			if err != nil {
				return fmt.Errorf("write version: %w", err)
			}
			return nil
		},
	}
}

func wantsJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
		if value, ok := strings.CutPrefix(arg, "--json="); ok {
			enabled, err := strconv.ParseBool(value)
			return err == nil && enabled
		}
	}
	return false
}

func writeError(stderr io.Writer, jsonOutput bool, err error) {
	code := codeCommand
	if typed, ok := errors.AsType[*codedError](err); ok {
		code = typed.code
	} else if providerCode, ok := provider.ErrorCodeOf(err); ok {
		code = string(providerCode)
	}

	if jsonOutput {
		_ = json.NewEncoder(stderr).Encode(errorEnvelope{Error: errorBody{
			Code:    code,
			Message: err.Error(),
		}})
		return
	}

	_, _ = fmt.Fprintf(stderr, "Error: %s\n", err)
}
