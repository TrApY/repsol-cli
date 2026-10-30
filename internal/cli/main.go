// Package cli implements the cobra commands of the repsol binary. The commands
// are thin: they parse flags, resolve the output format, and call the use cases
// in internal/app.
package cli

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// The build metadata. The release build sets Commit and Date and overrides
// Version through -ldflags.
var (
	// Version is the released version of the binary.
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit string
	// Date is the build date.
	Date string
)

// System is the process surface the CLI runs on. It is explicit so the
// testscript harness can run the real entry point in-process with the streams,
// the terminal state and the environment it needs.
type System struct {
	// In is the standard input.
	In io.Reader
	// Out is the standard output, which carries data only.
	Out io.Writer
	// Err is the standard error, which carries errors, hints and prompts.
	Err io.Writer
	// InTTY reports whether In is a terminal.
	InTTY bool
	// OutTTY reports whether Out is a terminal.
	OutTTY bool
	// Getenv reads an environment variable.
	Getenv func(string) string
	// ReadPassword reads a secret from the terminal without echoing it.
	ReadPassword func() (string, error)
}

// Main runs the CLI on the real process streams and returns its exit code. It
// stops the run on SIGINT or SIGTERM.
func Main() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return Run(ctx, os.Args[1:], System{
		In:     os.Stdin,
		Out:    os.Stdout,
		Err:    os.Stderr,
		InTTY:  term.IsTerminal(int(os.Stdin.Fd())),
		OutTTY: term.IsTerminal(int(os.Stdout.Fd())),
		Getenv: os.Getenv,
		ReadPassword: func() (string, error) {
			data, err := term.ReadPassword(int(os.Stdin.Fd()))
			if err != nil {
				return "", err
			}
			return string(data), nil
		},
	})
}
