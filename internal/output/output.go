// Package output renders command results and errors in the formats the CLI
// supports: machine-readable JSON, and a pretty rendering that falls back to
// JSON when a value has no pretty printer.
//
// stdout carries data only. Errors go to stderr, as text or as one compact
// JSON line, and never carry a secret.
package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// Format is the rendering format of command output.
type Format string

// The supported output formats.
const (
	// FormatJSON is indented JSON with snake_case keys.
	FormatJSON Format = "json"
	// FormatPretty is the human-readable rendering of a value.
	FormatPretty Format = "pretty"
)

// ResolveFormat picks the output format: an explicit flag wins, and an empty
// flag means pretty on a terminal and JSON otherwise.
func ResolveFormat(flag string, stdoutTTY bool) (Format, error) {
	switch Format(flag) {
	case FormatJSON:
		return FormatJSON, nil
	case FormatPretty:
		return FormatPretty, nil
	case "":
		if stdoutTTY {
			return FormatPretty, nil
		}
		return FormatJSON, nil
	default:
		return "", apperr.New(apperr.Usage, "invalid_format",
			fmt.Sprintf("unknown output format %q; use %q or %q", flag, FormatJSON, FormatPretty))
	}
}

// Pretty is implemented by values that have a human-readable rendering.
type Pretty interface {
	// PrettyPrint writes the human-readable form of the value.
	PrettyPrint(w io.Writer) error
}

// Render writes v to w in the given format. JSON is indented with two spaces
// and ends with a newline. Pretty uses PrettyPrint when v implements Pretty and
// falls back to indented JSON otherwise.
func Render(w io.Writer, f Format, v any) error {
	switch f {
	case FormatPretty:
		if p, ok := v.(Pretty); ok {
			return p.PrettyPrint(w)
		}
		return renderJSON(w, v)
	case FormatJSON:
		return renderJSON(w, v)
	default:
		return apperr.New(apperr.Usage, "invalid_format",
			fmt.Sprintf("unknown output format %q", f))
	}
}

// renderJSON writes v as two-space indented JSON followed by a newline.
func renderJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return apperr.New(apperr.Generic, "output_failed",
			"the output could not be rendered").WithCause(err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return apperr.New(apperr.Generic, "output_failed",
			"the output could not be written").WithCause(err)
	}
	return nil
}
