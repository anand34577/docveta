// Package apperr defines domain errors that map cleanly onto HTTP problem responses.
// Services return these; transport code turns them into RFC 9457 problem details.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

type Kind int

const (
	KindInternal Kind = iota
	KindNotFound
	KindForbidden
	KindUnauthorized
	KindConflict
	KindValidation
	KindPrecondition
	KindTooLarge
	KindRateLimited
	KindUnavailable
	KindGone
	KindInsufficientStorage
	KindUnsupported
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type Error struct {
	Kind   Kind
	Code   string // machine readable, e.g. "duplicate_document"
	Msg    string // human readable, safe to show to users
	Fields []FieldError
	Extra  map[string]any
	Err    error // wrapped cause, never shown to users
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

func (e *Error) Status() int {
	switch e.Kind {
	case KindNotFound:
		return http.StatusNotFound
	case KindForbidden:
		return http.StatusForbidden
	case KindUnauthorized:
		return http.StatusUnauthorized
	case KindConflict:
		return http.StatusConflict
	case KindValidation:
		return http.StatusUnprocessableEntity
	case KindPrecondition:
		return http.StatusPreconditionFailed
	case KindTooLarge:
		return http.StatusRequestEntityTooLarge
	case KindRateLimited:
		return http.StatusTooManyRequests
	case KindUnavailable:
		return http.StatusServiceUnavailable
	case KindGone:
		return http.StatusGone
	case KindInsufficientStorage:
		return http.StatusInsufficientStorage
	case KindUnsupported:
		return http.StatusUnsupportedMediaType
	}
	return http.StatusInternalServerError
}

func NotFound(what string) *Error {
	return &Error{Kind: KindNotFound, Code: "not_found", Msg: what + " not found"}
}

func Forbidden(msg string) *Error {
	if msg == "" {
		msg = "You don't have permission to do this"
	}
	return &Error{Kind: KindForbidden, Code: "forbidden", Msg: msg}
}

func Unauthorized(msg string) *Error {
	if msg == "" {
		msg = "Please sign in"
	}
	return &Error{Kind: KindUnauthorized, Code: "unauthorized", Msg: msg}
}

func Conflict(code, msg string) *Error {
	return &Error{Kind: KindConflict, Code: code, Msg: msg}
}

func Invalid(field, msg string) *Error {
	return &Error{Kind: KindValidation, Code: "validation_failed", Msg: "Some fields are invalid",
		Fields: []FieldError{{Field: field, Message: msg}}}
}

func Precondition(msg string) *Error {
	return &Error{Kind: KindPrecondition, Code: "edit_conflict", Msg: msg}
}

func Unavailable(code, msg string) *Error {
	return &Error{Kind: KindUnavailable, Code: code, Msg: msg}
}

func Internal(err error) *Error {
	return &Error{Kind: KindInternal, Code: "internal", Msg: "Something went wrong", Err: err}
}

// Validation accumulates field errors.
type Validation struct{ fields []FieldError }

func (v *Validation) Add(field, format string, args ...any) {
	v.fields = append(v.fields, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
}

func (v *Validation) Err() error {
	if len(v.fields) == 0 {
		return nil
	}
	return &Error{Kind: KindValidation, Code: "validation_failed", Msg: "Some fields are invalid", Fields: v.fields}
}

// As extracts an *Error from err.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// IsKind reports whether err is an *Error of the given kind.
func IsKind(err error, k Kind) bool {
	e, ok := As(err)
	return ok && e.Kind == k
}
