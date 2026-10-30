package output_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/output"
)

// TestResolveFormat checks the format precedence: the flag wins, an empty flag
// follows the terminal, and anything else is a usage error.
func TestResolveFormat(t *testing.T) {
	cases := []struct {
		name      string
		flag      string
		stdoutTTY bool
		want      output.Format
		wantExit  int
	}{
		{"default on a terminal", "", true, output.FormatPretty, 0},
		{"default off a terminal", "", false, output.FormatJSON, 0},
		{"json wins on a terminal", "json", true, output.FormatJSON, 0},
		{"pretty wins off a terminal", "pretty", false, output.FormatPretty, 0},
		{"unknown value", "yaml", false, "", int(apperr.Usage)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := output.ResolveFormat(tc.flag, tc.stdoutTTY)
			if exit := apperr.ExitCode(err); exit != tc.wantExit {
				t.Fatalf("ResolveFormat(%q, %v) exit code = %d, want %d (err %v)",
					tc.flag, tc.stdoutTTY, exit, tc.wantExit, err)
			}
			if got != tc.want {
				t.Fatalf("ResolveFormat(%q, %v) = %q, want %q",
					tc.flag, tc.stdoutTTY, got, tc.want)
			}
		})
	}
}

// prettyValue is a value with a pretty printer, used to check that Render
// prefers it over the JSON fallback.
type prettyValue string

func (p prettyValue) PrettyPrint(w io.Writer) error {
	_, err := io.WriteString(w, "pretty:"+string(p))
	return err
}

// TestRender checks the JSON shape and the pretty fallback.
func TestRender(t *testing.T) {
	var buf bytes.Buffer
	if err := output.Render(&buf, output.FormatJSON, map[string]any{"b": 1}); err != nil {
		t.Fatalf("Render(JSON) error = %v", err)
	}
	if want := "{\n  \"b\": 1\n}\n"; buf.String() != want {
		t.Fatalf("Render(JSON) = %q, want %q", buf.String(), want)
	}

	buf.Reset()
	if err := output.Render(&buf, output.FormatPretty, prettyValue("x")); err != nil {
		t.Fatalf("Render(pretty, printer) error = %v", err)
	}
	if want := "pretty:x"; buf.String() != want {
		t.Fatalf("Render(pretty, printer) = %q, want %q", buf.String(), want)
	}

	buf.Reset()
	if err := output.Render(&buf, output.FormatPretty, map[string]any{"b": 1}); err != nil {
		t.Fatalf("Render(pretty, fallback) error = %v", err)
	}
	if want := "{\n  \"b\": 1\n}\n"; buf.String() != want {
		t.Fatalf("Render(pretty, fallback) = %q, want %q", buf.String(), want)
	}
}

// TestEnvelope checks the error envelope: one compact JSON line with every key
// present, and the two-line text rendering.
func TestEnvelope(t *testing.T) {
	err := apperr.New(apperr.Refused, "pending_order", "m").WithHint("h")

	var buf bytes.Buffer
	output.WriteError(&buf, output.ErrorJSON, err)
	line := buf.String()
	if !strings.HasSuffix(line, "\n") {
		t.Fatalf("WriteError(JSON) = %q, want a trailing newline", line)
	}
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("WriteError(JSON) = %q, want one compact line", line)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("WriteError(JSON) is not valid JSON: %v (%q)", err, line)
	}
	wantKeys := []string{
		"code", "category", "exit_code", "retryable",
		"message", "http_status", "endpoint", "hint",
	}
	if len(got) != len(wantKeys) {
		t.Fatalf("envelope keys = %v, want exactly %v", keysOf(got), wantKeys)
	}
	for _, key := range wantKeys {
		if _, ok := got[key]; !ok {
			t.Fatalf("envelope is missing key %q: %v", key, got)
		}
	}
	if got["code"] != "pending_order" {
		t.Fatalf("code = %v, want pending_order", got["code"])
	}
	if got["category"] != "refused" {
		t.Fatalf("category = %v, want refused", got["category"])
	}
	if got["exit_code"] != float64(8) {
		t.Fatalf("exit_code = %v, want 8", got["exit_code"])
	}
	if got["message"] != "m" {
		t.Fatalf("message = %v, want m", got["message"])
	}
	if got["hint"] != "h" {
		t.Fatalf("hint = %v, want h", got["hint"])
	}

	buf.Reset()
	output.WriteError(&buf, output.ErrorText, err)
	if want := "repsol: m\nhint: h\n"; buf.String() != want {
		t.Fatalf("WriteError(text) = %q, want %q", buf.String(), want)
	}

	buf.Reset()
	output.WriteError(&buf, output.ErrorText, apperr.New(apperr.Usage, "usage", "no hint"))
	if want := "repsol: no hint\n"; buf.String() != want {
		t.Fatalf("WriteError(text, no hint) = %q, want %q", buf.String(), want)
	}
}

// TestResolveErrorFormat checks the error format precedence: flag, then
// environment, then text.
func TestResolveErrorFormat(t *testing.T) {
	cases := []struct {
		flag, env string
		want      output.ErrorFormat
		wantExit  int
	}{
		{"", "", output.ErrorText, 0},
		{"json", "", output.ErrorJSON, 0},
		{"", "json", output.ErrorJSON, 0},
		{"text", "json", output.ErrorText, 0},
		{"yaml", "", "", int(apperr.Usage)},
		{"", "yaml", "", int(apperr.Usage)},
	}
	for _, tc := range cases {
		got, err := output.ResolveErrorFormat(tc.flag, tc.env)
		if exit := apperr.ExitCode(err); exit != tc.wantExit {
			t.Fatalf("ResolveErrorFormat(%q, %q) exit code = %d, want %d (err %v)",
				tc.flag, tc.env, exit, tc.wantExit, err)
		}
		if got != tc.want {
			t.Fatalf("ResolveErrorFormat(%q, %q) = %q, want %q", tc.flag, tc.env, got, tc.want)
		}
	}
}

// keysOf returns the keys of m, for failure messages.
func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
