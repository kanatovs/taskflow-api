package domain

import "errors"

var (
	ErrNotFound           = errors.New("resource not found")
	ErrEmailExists        = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUnauthenticated    = errors.New("authentication required")
)

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "request validation failed" }
