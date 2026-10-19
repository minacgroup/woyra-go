package woyra

import (
	"errors"
	"fmt"
)

// Error is returned for every failed API request.
//
// StatusCode is 0 when no HTTP response was received; Code is then
// "connection_error" and Err holds the underlying network error. Branch on
// Code, never on Message.
type Error struct {
	StatusCode int
	Code       string
	Message    string
	Param      string
	RequestID  string
	DocURL     string
	// Err is the underlying cause for a connection error.
	Err error
}

func (e *Error) Error() string {
	s := fmt.Sprintf("woyra: %s (code=%s", e.Message, e.Code)
	if e.StatusCode != 0 {
		s += fmt.Sprintf(", status=%d", e.StatusCode)
	}
	if e.Param != "" {
		s += ", param=" + e.Param
	}
	if e.RequestID != "" {
		s += ", request_id=" + e.RequestID
	}
	return s + ")"
}

func (e *Error) Unwrap() error { return e.Err }

// Webhook verification failures. Test with errors.Is.
var (
	ErrMissingSecret    = errors.New("woyra: a webhook signing secret is required")
	ErrMissingHeader    = errors.New("woyra: webhook signature headers are missing")
	ErrInvalidTimestamp = errors.New("woyra: Woyra-Timestamp is not a unix timestamp")
	ErrSignatureExpired = errors.New("woyra: webhook timestamp is outside the tolerance window")
	ErrInvalidSignature = errors.New("woyra: webhook signature does not match")
	ErrInvalidPayload   = errors.New("woyra: webhook payload is not valid JSON")
)
