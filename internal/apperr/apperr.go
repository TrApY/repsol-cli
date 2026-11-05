// Package apperr defines the typed errors used across the CLI, the MCP server
// and the internal clients, together with the stable mapping from error to
// process exit code.
//
// Every failure the user can observe is an *Error with a machine-readable Code
// and a Category. The Category value is the exit code, so the CLI and the MCP
// server report failures the same way.
package apperr

import (
	"context"
	"errors"
)

// Category is the class of a failure. Its numeric value is the process exit
// code the CLI returns for it.
type Category int

// The error categories, whose values are the CLI exit codes.
const (
	// Generic is an unclassified failure.
	Generic Category = 1
	// Usage is a bad invocation: unknown flag, invalid value, missing input.
	Usage Category = 2
	// Auth is a missing, expired or rejected session.
	Auth Category = 3
	// NotFound is a missing resource.
	NotFound Category = 4
	// Blocked is a refusal by Repsol, a firewall or a rate limit.
	Blocked Category = 5
	// Network is a transport failure or a timeout on a read.
	Network Category = 6
	// APIChanged means Repsol answered with an unexpected status or shape.
	APIChanged Category = 7
	// Refused means the request was understood but not allowed.
	Refused Category = 8
	// OutcomeUnknown means an order write may or may not have been placed.
	OutcomeUnknown Category = 9
	// Interrupted means the run was cancelled.
	Interrupted Category = 130
)

var categoryNames = map[Category]string{
	Generic:        "generic",
	Usage:          "usage",
	Auth:           "auth",
	NotFound:       "not_found",
	Blocked:        "blocked",
	Network:        "network",
	APIChanged:     "api_changed",
	Refused:        "refused",
	OutcomeUnknown: "outcome_unknown",
	Interrupted:    "interrupted",
}

// String returns the snake_case name of the category.
func (c Category) String() string {
	if name, ok := categoryNames[c]; ok {
		return name
	}
	return "generic"
}

// Error is a failure with a machine-readable code, a category, an optional
// hint for the user and the HTTP context of the call that failed.
type Error struct {
	// Category is the class of the failure; its value is the exit code.
	Category Category
	// Code is a snake_case machine code, such as "pending_order".
	Code string
	// Message is a human-readable, single-line description.
	Message string
	// Hint tells the user what to do next. It never contains secrets.
	Hint string
	// HTTPStatus is the status code of the failing response, or 0.
	HTTPStatus int
	// Endpoint is the API path of the failing call, never a full URL.
	Endpoint string
	// Retryable reports whether repeating the call could succeed.
	Retryable bool
	// NotSent reports that the request provably never left the client: it
	// failed before the first byte was written, for instance on a name lookup
	// or a refused connection. The server cannot have processed it. It is false
	// for every other failure, including those that merely look like it.
	NotSent bool
	// Err is the wrapped cause, if any.
	Err error
}

// Error returns the message, followed by the cause when there is one.
func (e *Error) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

// Unwrap returns the wrapped cause.
func (e *Error) Unwrap() error { return e.Err }

// WithHint sets the hint and returns the same error.
func (e *Error) WithHint(hint string) *Error {
	e.Hint = hint
	return e
}

// WithCause sets the cause and returns the same error.
func (e *Error) WithCause(err error) *Error {
	e.Err = err
	return e
}

// New builds an error of the given category and code.
func New(cat Category, code, msg string) *Error {
	return &Error{Category: cat, Code: code, Message: msg}
}

// From converts any error into an *Error. It returns nil only for a nil error,
// and it keeps the fields of an error that already is one.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) && typed != nil {
		return typed
	}
	switch {
	case errors.Is(err, context.Canceled):
		return &Error{Category: Interrupted, Code: "interrupted", Message: err.Error()}
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{
			Category:  Network,
			Code:      "timeout",
			Message:   err.Error(),
			Retryable: true,
		}
	default:
		return &Error{Category: Generic, Code: "error", Message: err.Error()}
	}
}

// ExitCode returns the exit code for an error: 0 for no error, the category
// value otherwise.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	return int(From(err).Category)
}
