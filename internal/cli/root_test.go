package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// TestFinalErrorKeepsOutcomeUnknown covers Review Focus 3: a write whose
// outcome is unknown stays exit 9 even when the run was interrupted, because a
// retry could place a second order.
func TestFinalErrorKeepsOutcomeUnknown(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	unknown := apperr.New(apperr.OutcomeUnknown, "order_outcome_unknown",
		"the order may have been placed")
	if got := apperr.ExitCode(finalError(cancelled, unknown)); got != 9 {
		t.Fatalf("finalError(cancelled, outcome unknown) exit code = %d, want 9", got)
	}

	network := apperr.New(apperr.Network, "network_error", "the network failed")
	if got := apperr.ExitCode(finalError(cancelled, network)); got != 130 {
		t.Fatalf("finalError(cancelled, network) exit code = %d, want 130", got)
	}

	if got := apperr.ExitCode(finalError(context.Background(), network)); got != 6 {
		t.Fatalf("finalError(live, network) exit code = %d, want 6", got)
	}
}

// TestUsageError covers the mapping of an error raised before the root
// PersistentPreRunE ran: an unknown command, flag or argument exits 2 with the
// usage code.
func TestUsageError(t *testing.T) {
	original := apperr.New(apperr.NotFound, "not_found", "unknown command \"frobnicate\"")
	got := apperr.From(usageError(original))
	if got.Category != apperr.Usage {
		t.Fatalf("category = %v, want usage", got.Category)
	}
	if got.Code != "usage" {
		t.Fatalf("code = %q, want usage", got.Code)
	}
	if !strings.Contains(got.Message, "unknown command") {
		t.Fatalf("message = %q, want the original message", got.Message)
	}
}

// TestRunRejectsBadFormatBeforeCommand checks that an invalid --format fails as
// a usage error before the command runs: a write must never execute and then
// fail while rendering.
func TestRunRejectsBadFormatBeforeCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"--format", "yaml", "auth", "status"}, System{
		In:     strings.NewReader(""),
		Out:    &stdout,
		Err:    &stderr,
		Getenv: func(string) string { return "" },
	})
	if exit != int(apperr.Usage) {
		t.Fatalf("exit code = %d, want %d", exit, apperr.Usage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no data", stdout.String())
	}
	if !strings.Contains(stderr.String(), "repsol: ") {
		t.Fatalf("stderr = %q, want the error message", stderr.String())
	}
}
