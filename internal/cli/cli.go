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

	"github.com/kostyay/ecom/internal/config"
	"github.com/kostyay/ecom/internal/logging"
	"github.com/kostyay/ecom/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	appName = "ecom"

	codeCommand = "command_error"
	codeConfig  = "config_error"
	codeLog     = "log_error"
)

type application struct {
	viper     *viper.Viper
	logCloser io.Closer
	logger    *slog.Logger
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
	app := &application{viper: viper.New()}
	cmd := app.newRootCommand(stdout, stderr)
	cmd.SetArgs(args)

	err := cmd.ExecuteContext(ctx)
	if app.logCloser != nil {
		defer func() {
			_ = app.logCloser.Close()
		}()
	}
	if err == nil {
		return 0
	}

	if app.logger != nil {
		app.logger.ErrorContext(ctx, "command stopped", "error", err)
	}
	writeError(stderr, wantsJSON(args), err)

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
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &codedError{code: codeCommand, err: err}
	})

	flags := cmd.PersistentFlags()
	flags.Bool("json", false, "write machine-readable JSON")
	flags.String("config", "", "read configuration from this file")
	flags.String("log-level", "", "set the log level: debug, info, warn, or error")
	flags.String("log-file", "", "write logs to this file")

	cmd.AddCommand(newVersionCommand())

	return cmd
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
	slog.SetDefault(logger)
	logger.InfoContext(cmd.Context(), "command started", "command", cmd.CommandPath())

	return nil
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
