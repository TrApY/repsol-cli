package apperr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestExitCode(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{errors.New("boom"), 1},
		{New(Usage, "bad_flag", "x"), 2},
		{fmt.Errorf("wrap: %w", New(Auth, "session_expired", "x")), 3},
		{New(OutcomeUnknown, "order_outcome_unknown", "x"), 9},
		{context.Canceled, 130},
		{context.DeadlineExceeded, 6},
	}
	for _, c := range cases {
		if got := ExitCode(c.err); got != c.want {
			t.Errorf("ExitCode(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestCategoryNames(t *testing.T) {
	want := map[Category]string{Generic: "generic", Usage: "usage", Auth: "auth",
		NotFound: "not_found", Blocked: "blocked", Network: "network",
		APIChanged: "api_changed", Refused: "refused",
		OutcomeUnknown: "outcome_unknown", Interrupted: "interrupted"}
	for c, s := range want {
		if c.String() != s {
			t.Errorf("%d.String() = %q, want %q", c, c.String(), s)
		}
	}
}

func TestFromKeepsFields(t *testing.T) {
	e := New(Refused, "pending_order", "order 20000001 is pending").WithHint("pass --force")
	got := From(fmt.Errorf("ctx: %w", e))
	if got.Code != "pending_order" || got.Hint != "pass --force" ||
		!strings.Contains(got.Error(), "order 20000001 is pending") {
		t.Fatalf("From lost fields: %+v", got)
	}
	if d := From(context.DeadlineExceeded); d.Code != "timeout" || !d.Retryable {
		t.Fatalf("deadline: %+v", d)
	}
}
