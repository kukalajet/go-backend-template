// Package apperr defines the typed errors that cross package boundaries.
package apperr

import "errors"

// Kind classifies an error for the boundary that reports it.
type Kind int

// The kinds of expected failure.
const (
	KindInternal    Kind = iota // 500
	KindInvalid                 // 400
	KindNotFound                // 404
	KindConflict                // 409
	KindUnavailable             // 503
	KindTooLarge                // 413
)

// Diagnostics holds details that are safe to log.
type Diagnostics struct {
	Reason          string   `json:"reason"`
	Code            string   `json:"code,omitempty"` // SQLSTATE
	ValidationPaths []string `json:"validationPaths,omitempty"`
}

// Error is an expected failure.
type Error struct {
	Kind        Kind
	Code        string       // stable, client-facing: "USER_NOT_FOUND"
	Message     string       // safe for clients
	Op          string       // "users.insert"; set for infrastructure failures
	Diagnostics *Diagnostics // safe for logs
	cause       error        // raw cause: no Unwrap, never logged on handled paths
}

// Error returns the client-safe message.
func (e *Error) Error() string { return e.Message }

// Cause returns the raw cause, for debugging only.
func (e *Error) Cause() error { return e.cause }

// New returns an error with no diagnostics.
func New(kind Kind, code, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

// Invalid returns a KindInvalid error listing the failing field paths.
func Invalid(code, message string, paths ...string) *Error {
	return &Error{
		Kind:        KindInvalid,
		Code:        code,
		Message:     message,
		Diagnostics: &Diagnostics{Reason: "ValidationError", ValidationPaths: paths},
	}
}

// InvalidRequest returns the standard validation failure for clients.
func InvalidRequest(paths ...string) *Error {
	return Invalid("INVALID_REQUEST", "The request is invalid", paths...)
}

// Infrastructure returns an error for a failure outside the process.
func Infrastructure(kind Kind, code, message, op string, d Diagnostics, cause error) *Error {
	return &Error{Kind: kind, Code: code, Message: message, Op: op, Diagnostics: &d, cause: cause}
}

// As reports whether err is an *Error and returns it.
func As(err error) (*Error, bool) {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr, true
	}

	return nil, false
}
