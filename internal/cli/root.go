package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/felipe9962/repsol-cli/internal/app"
	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/gigya"
	"github.com/felipe9962/repsol-cli/internal/httpx"
	"github.com/felipe9962/repsol-cli/internal/output"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// rootState holds the global flags and the lazily built App of one run.
type rootState struct {
	sys System

	profile     string
	format      string
	noInput     bool
	debug       bool
	errorFormat string

	// started reports whether PersistentPreRunE ran. An error before it is a
	// usage error.
	started bool
	// application is the App, built on first use by a command.
	application *app.App
}

// Run executes one CLI invocation and returns its exit code. Every failure is
// printed to sys.Err as an error envelope, and the exit code follows the
// category of the error.
func Run(ctx context.Context, args []string, sys System) int {
	if sys.Getenv == nil {
		sys.Getenv = os.Getenv
	}

	state := &rootState{sys: sys}
	root := newRoot(state)
	root.SetArgs(args)
	root.SetOut(sys.Out)
	root.SetErr(sys.Err)

	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}

	// An unknown command, an unknown flag or a bad argument never reaches
	// PersistentPreRunE; it is a usage error.
	if !state.started {
		err = usageError(err)
	}
	err = finalError(ctx, err)

	// Cobra fails to find an unknown command before parsing any flag, so the
	// error format flag is read from the raw arguments in that path.
	errorFormat := state.errorFormat
	if !state.started && errorFormat == "" {
		errorFormat = errorFormatFromArgs(args)
	}
	format, formatErr := output.ResolveErrorFormat(errorFormat, sys.Getenv("REPSOL_ERROR_FORMAT"))
	if formatErr != nil {
		output.WriteError(sys.Err, output.ErrorText, formatErr)
		return apperr.ExitCode(formatErr)
	}
	output.WriteError(sys.Err, format, err)
	return apperr.ExitCode(err)
}

// errorFormatFromArgs returns the last --error-format value in args, in either
// the "--error-format value" or the "--error-format=value" form. It is the
// fallback for an invocation cobra rejects before parsing flags, such as an
// unknown command.
func errorFormatFromArgs(args []string) string {
	value := ""
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--":
			return value
		case arg == "--error-format":
			if i+1 < len(args) {
				i++
				value = args[i]
			}
		case strings.HasPrefix(arg, "--error-format="):
			value = strings.TrimPrefix(arg, "--error-format=")
		}
	}
	return value
}

// usageError converts an error raised before PersistentPreRunE ran, such as an
// unknown command, flag or argument, into a usage error with the stable code
// "usage". The original message is kept.
func usageError(err error) error {
	if err == nil {
		return nil
	}
	return apperr.New(apperr.Usage, "usage", err.Error())
}

// finalError applies the interruption rule: a cancelled run reports
// Interrupted, except when an order write may already have reached Repsol. An
// outcome-unknown error stays outcome-unknown so the caller never retries.
func finalError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil && apperr.From(err).Category != apperr.OutcomeUnknown {
		return apperr.New(apperr.Interrupted, "interrupted",
			"the run was interrupted").WithCause(ctx.Err())
	}
	return err
}

// newRoot builds the root command and its global flags.
func newRoot(state *rootState) *cobra.Command {
	root := &cobra.Command{
		Use:           "repsol",
		Short:         "Order bottled gas from Repsol",
		Long:          "Order bottled gas (butane, propane) from Repsol, paid on delivery.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			state.started = true
			return nil
		},
	}

	flags := root.PersistentFlags()
	flags.StringVar(&state.profile, "profile", "",
		"configuration profile to use")
	flags.StringVar(&state.format, "format", "",
		`output format: "json" or "pretty" (default: pretty on a terminal, json otherwise)`)
	flags.BoolVar(&state.noInput, "no-input", false,
		"never prompt; missing input becomes a usage error")
	flags.BoolVar(&state.debug, "debug", false,
		"log HTTP calls to stderr, with secrets redacted")
	flags.StringVar(&state.errorFormat, "error-format", "",
		`error format: "text" or "json"`)

	root.AddCommand(newAuthCommand(state))
	return root
}

// getApp builds the App on first use. It resolves the profile, wires the shared
// transport and the three backend clients, and reads the REPSOL_* environment.
func (s *rootState) getApp() (*app.App, error) {
	if s.application != nil {
		return s.application, nil
	}

	store := config.Store{Paths: config.DefaultPaths()}
	cfg, err := store.LoadConfig()
	if err != nil {
		return nil, err
	}
	profile, err := config.ResolveProfile(s.profile, s.sys.Getenv("REPSOL_PROFILE"), cfg)
	if err != nil {
		return nil, err
	}

	var logger *slog.Logger
	if s.debug {
		logger = slog.New(slog.NewTextHandler(s.sys.Err, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	}
	httpClient := httpx.New("repsol-cli/"+Version, logger)

	s.application = app.New(app.Deps{
		API: &repsolapi.Client{
			BaseURL:   baseURL(s.sys.Getenv, "REPSOL_API_BASE_URL", repsolapi.DefaultBaseURL),
			HTTP:      httpClient,
			Telemetry: repsolapi.NewTelemetry(),
		},
		Places: &repsolapi.PlacesClient{
			BaseURL: baseURL(s.sys.Getenv, "REPSOL_ADDRESS_BASE_URL", repsolapi.DefaultPlacesBaseURL),
			HTTP:    httpClient,
		},
		Gigya: &gigya.Client{
			BaseURL: baseURL(s.sys.Getenv, "REPSOL_GIGYA_BASE_URL", gigya.DefaultBaseURL),
			APIKey:  gigya.APIKey,
			HTTP:    httpClient,
		},
		Store:   store,
		Profile: profile,
		Env: app.Env{
			LoginToken: s.sys.Getenv("REPSOL_LOGIN_TOKEN"),
			GMID:       s.sys.Getenv("REPSOL_GMID"),
			Email:      s.sys.Getenv("REPSOL_EMAIL"),
			Password:   s.sys.Getenv("REPSOL_PASSWORD"),
		},
	})
	return s.application, nil
}

// baseURL returns the environment override of a base URL, or the default. A
// trailing slash is removed so the clients can append endpoint paths.
func baseURL(getenv func(string) string, key, fallback string) string {
	if value := getenv(key); value != "" {
		return strings.TrimRight(value, "/")
	}
	return fallback
}

// render writes v to stdout in the resolved output format.
func (s *rootState) render(v any) error {
	format, err := output.ResolveFormat(s.format, s.sys.OutTTY)
	if err != nil {
		return err
	}
	return output.Render(s.sys.Out, format, v)
}

// canPrompt reports whether the run may ask the terminal for input.
func (s *rootState) canPrompt() bool {
	return s.sys.InTTY && !s.noInput
}

// prompt writes prompt to stderr and reads a secret without echoing it.
func (s *rootState) prompt(text string) (string, error) {
	if s.sys.ReadPassword == nil {
		return "", apperr.New(apperr.Usage, "missing_input",
			"the terminal cannot read a secret")
	}
	fmt.Fprint(s.sys.Err, text)
	value, err := s.sys.ReadPassword()
	fmt.Fprintln(s.sys.Err)
	if err != nil {
		return "", apperr.New(apperr.Usage, "missing_input",
			"the secret could not be read").WithCause(err)
	}
	return value, nil
}

// secretLine reads a secret either from a hidden terminal prompt or, when
// stdin is a pipe, as the first line of stdin. --no-input on a terminal is a
// usage error instead of a blocking read.
func (s *rootState) secretLine(text string) (string, error) {
	switch {
	case s.canPrompt():
		return s.prompt(text)
	case s.sys.InTTY:
		return "", apperr.New(apperr.Usage, "missing_input",
			"--no-input disables the prompt and stdin is a terminal")
	default:
		return readSecretLine(s.sys.In)
	}
}
