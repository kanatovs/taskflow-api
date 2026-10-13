package domain

import "errors"

var (
	ErrNotFound    = errors.New("resource not found")
	ErrEmailExists = errors.New("email already registered")
)
