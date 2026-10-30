package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/felipe9962/repsol-cli/internal/app"
	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/gigya"
)

const (
	// loginHint lists the three login methods.
	loginHint = `run "repsol auth login --har FILE", "repsol auth login --token-stdin" or "repsol auth login --email ADDRESS"`
	// passwordHint tells how to supply the password of --email.
	passwordHint = `pass --password-stdin or run the command on a terminal`
	// addressHint is the follow-up after a login without a stored address.
	addressHint = `run "repsol address set \"STREET NUMBER, TOWN\""`
	// tokenPrompt is the hidden prompt of --token-stdin on a terminal.
	tokenPrompt = `Paste "glt_…=TOKEN; gmid=GMID": `
	// passwordPrompt is the hidden prompt of --email on a terminal.
	passwordPrompt = `Repsol password: `
)

// newAuthCommand builds the auth command and its subcommands.
func newAuthCommand(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Log in to Repsol and inspect the stored session",
	}
	cmd.AddCommand(
		newAuthLoginCommand(state),
		newAuthStatusCommand(state),
		newAuthLogoutCommand(state),
	)
	return cmd
}

// authLoginOptions are the flags of auth login.
type authLoginOptions struct {
	har           string
	tokenStdin    bool
	email         string
	passwordStdin bool
}

// newAuthLoginCommand builds auth login.
func newAuthLoginCommand(state *rootState) *cobra.Command {
	opts := &authLoginOptions{}
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store a Repsol session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogin(cmd.Context(), state, opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.har, "har", "",
		"import the session from a browser HAR file")
	flags.BoolVar(&opts.tokenStdin, "token-stdin", false,
		"read a session line from stdin")
	flags.StringVar(&opts.email, "email", "",
		"log in with the account email")
	flags.BoolVar(&opts.passwordStdin, "password-stdin", false,
		"read the password from stdin")
	return cmd
}

// runAuthLogin executes exactly one login method and reports the resulting
// status.
func runAuthLogin(ctx context.Context, state *rootState, opts *authLoginOptions) error {
	methods := 0
	for _, used := range []bool{opts.har != "", opts.tokenStdin, opts.email != ""} {
		if used {
			methods++
		}
	}
	if methods != 1 {
		return apperr.New(apperr.Usage, "login_method",
			"auth login needs exactly one login method").WithHint(loginHint)
	}

	a, err := state.getApp()
	if err != nil {
		return err
	}

	var status app.AuthStatus
	switch {
	case opts.har != "":
		session, err := sessionFromFile(opts.har)
		if err != nil {
			return err
		}
		status, err = a.ImportSession(ctx, session)
		if err != nil {
			return err
		}
	case opts.tokenStdin:
		line, err := state.secretLine(tokenPrompt)
		if err != nil {
			return err
		}
		session, err := gigya.ParseSessionLine(line)
		if err != nil {
			return err
		}
		status, err = a.ImportSession(ctx, session)
		if err != nil {
			return err
		}
	case opts.email != "":
		password, err := state.password(opts.passwordStdin)
		if err != nil {
			return err
		}
		status, err = a.PasswordLogin(ctx, opts.email, password)
		if err != nil {
			return err
		}
	}

	if err := state.render(status); err != nil {
		return err
	}
	if !status.AddressSet {
		fmt.Fprintf(state.sys.Err, "hint: %s\n", addressHint)
	}
	return nil
}

// sessionFromFile imports the session of a browser HAR export.
func sessionFromFile(path string) (gigya.Session, error) {
	file, err := os.Open(path)
	if err != nil {
		return gigya.Session{}, apperr.New(apperr.Usage, "har_unreadable",
			fmt.Sprintf("the HAR file %q could not be opened", path)).WithCause(err)
	}
	defer file.Close()
	return gigya.SessionFromHAR(file)
}

// password obtains the password of an email login: from piped stdin with
// --password-stdin, otherwise from a hidden terminal prompt.
func (s *rootState) password(fromStdin bool) (string, error) {
	if fromStdin && !s.sys.InTTY {
		return readSecretLine(s.sys.In)
	}
	if s.canPrompt() {
		return s.prompt(passwordPrompt)
	}
	return "", apperr.New(apperr.Usage, "password_required",
		"auth login --email needs a password").WithHint(passwordHint)
}

// newAuthStatusCommand builds auth status.
func newAuthStatusCommand(state *rootState) *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the stored session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			status, err := a.Status(cmd.Context(), check)
			if err != nil {
				return err
			}
			return state.render(status)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false,
		"validate the session with Gigya")
	return cmd
}

// newAuthLogoutCommand builds auth logout.
func newAuthLogoutCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored session of the profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			result, err := a.Logout()
			if err != nil {
				return err
			}
			return state.render(result)
		},
	}
}
