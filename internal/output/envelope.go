package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// ErrorFormat is the rendering format of errors on stderr.
type ErrorFormat string

// The supported error formats.
const (
	// ErrorText is the default: one "repsol: ..." line and an optional hint.
	ErrorText ErrorFormat = "text"
	// ErrorJSON is one compact JSON envelope line.
	ErrorJSON ErrorFormat = "json"
)

// ResolveErrorFormat picks the error format: the flag wins over the
// REPSOL_ERROR_FORMAT environment value, which wins over text.
func ResolveErrorFormat(flag, env string) (ErrorFormat, error) {
	value := flag
	if value == "" {
		value = env
	}
	switch ErrorFormat(value) {
	case "", ErrorText:
		return ErrorText, nil
	case ErrorJSON:
		return ErrorJSON, nil
	default:
		return "", apperr.New(apperr.Usage, "invalid_error_format",
			fmt.Sprintf("unknown error format %q; use %q or %q", value, ErrorText, ErrorJSON))
	}
}

// Envelope is the machine-readable form of a failure. Every key is always
// present, so consumers can rely on the shape. The values never contain a
// secret.
type Envelope struct {
	// Code is the snake_case machine code of the failure.
	Code string `json:"code"`
	// Category is the snake_case category name.
	Category string `json:"category"`
	// ExitCode is the process exit code of the failure.
	ExitCode int `json:"exit_code"`
	// Retryable reports whether repeating the call could succeed.
	Retryable bool `json:"retryable"`
	// Message is the human-readable description.
	Message string `json:"message"`
	// HTTPStatus is the status of the failing response, or 0.
	HTTPStatus int `json:"http_status"`
	// Endpoint is the API path of the failing call, or empty.
	Endpoint string `json:"endpoint"`
	// Hint tells the user what to do next, or is empty.
	Hint string `json:"hint"`
}

// EnvelopeFor converts an error into its envelope. A nil error yields the zero
// envelope.
func EnvelopeFor(err error) Envelope {
	if err == nil {
		return Envelope{}
	}
	e := apperr.From(err)
	return Envelope{
		Code:       e.Code,
		Category:   e.Category.String(),
		ExitCode:   int(e.Category),
		Retryable:  e.Retryable,
		Message:    e.Message,
		HTTPStatus: e.HTTPStatus,
		Endpoint:   e.Endpoint,
		Hint:       e.Hint,
	}
}

// WriteError writes err to w in the given format. JSON is one compact line
// ending in a newline. Text is "repsol: <message>" followed by
// "hint: <hint>" when the error has one. A nil error writes nothing.
func WriteError(w io.Writer, f ErrorFormat, err error) {
	if err == nil {
		return
	}
	env := EnvelopeFor(err)
	if f == ErrorJSON {
		data, marshalErr := json.Marshal(env)
		if marshalErr != nil {
			return
		}
		fmt.Fprintf(w, "%s\n", data)
		return
	}
	fmt.Fprintf(w, "repsol: %s\n", env.Message)
	if env.Hint != "" {
		fmt.Fprintf(w, "hint: %s\n", env.Hint)
	}
}
