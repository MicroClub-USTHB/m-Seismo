// Package apperr defines the domain errors that services return and the HTTP
// layer translates into problem responses.
package apperr

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// NotFound wraps ErrNotFound with the kind of entity that was missing.
func NotFound(entity string) error {
	return fmt.Errorf("%s %w", entity, ErrNotFound)
}

// Conflict wraps ErrConflict with a human-readable reason.
func Conflict(reason string) error {
	return fmt.Errorf("%w: %s", ErrConflict, reason)
}

// FieldError describes one invalid input field.
type FieldError struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// ValidationError lists every invalid field of a request.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Name + ": " + f.Reason
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// Validator collects field errors; Err returns nil when there are none.
type Validator struct {
	fields []FieldError
}

func (v *Validator) Add(name, reason string) {
	v.fields = append(v.fields, FieldError{Name: name, Reason: reason})
}

// Check adds the field error when ok is false.
func (v *Validator) Check(ok bool, name, reason string) {
	if !ok {
		v.Add(name, reason)
	}
}

func (v *Validator) Err() error {
	if len(v.fields) == 0 {
		return nil
	}
	return &ValidationError{Fields: v.fields}
}
