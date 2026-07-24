package provider

import (
	"errors"
	"fmt"
)

type ErrorKind string

const (
	ErrorKindInvalidRequest ErrorKind = "invalid_request"
	ErrorKindUnavailable    ErrorKind = "unavailable"
	ErrorKindBadResponse    ErrorKind = "bad_response"
	ErrorKindNoData         ErrorKind = "no_data"
)

type Error struct {
	Provider   string
	Operation  string
	Kind       ErrorKind
	StatusCode int
	Message    string
	Err        error
}

func (e *Error) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("%s %s failed (%s, HTTP %d): %s", e.Provider, e.Operation, e.Kind, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("%s %s failed (%s): %s", e.Provider, e.Operation, e.Kind, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func KindOf(err error) ErrorKind {
	var providerErr *Error
	if errors.As(err, &providerErr) {
		return providerErr.Kind
	}
	return ErrorKindBadResponse
}
