package article

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Sentinel errors, mapped to HTTP statuses in httpapi.
var (
	ErrNotFound         = errors.New("not found")
	ErrAuthorNotFound   = errors.New("author not found")
	ErrCategoryNotFound = errors.New("category not found")
	ErrImageNotFound    = errors.New("image not found")
	ErrCommentNotFound  = errors.New("comment not found")
	ErrInvalidCursor    = errors.New("invalid cursor")
	ErrTooManyImages    = errors.New("too many images")
	ErrUnsupportedImage = errors.New("unsupported image")
	ErrUnauthenticated  = errors.New("unauthenticated")
	ErrForbidden        = errors.New("forbidden")
	// ErrUnavailable is a transient backend failure; the request may be retried.
	ErrUnavailable = errors.New("backend temporarily unavailable")
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError reports every invalid field at once.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Field + ": " + f.Message
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// NewValidationError builds a single-field error for callers outside the package.
func NewValidationError(field, message string) error {
	return &ValidationError{Fields: []FieldError{{Field: field, Message: message}}}
}

type validationErrors struct{ fields []FieldError }

func (v *validationErrors) add(field, msg string) {
	v.fields = append(v.fields, FieldError{Field: field, Message: msg})
}

func (v *validationErrors) required(field, value string) {
	if value == "" {
		v.add(field, "is required")
	}
}

func (v *validationErrors) text(field, value string, max int) {
	if value == "" {
		v.add(field, "is required")
		return
	}
	if utf8.RuneCountInString(value) > max {
		v.add(field, fmt.Sprintf("must be at most %d characters", max))
	}
}

func (v *validationErrors) err() error {
	if len(v.fields) == 0 {
		return nil
	}
	return &ValidationError{Fields: v.fields}
}
