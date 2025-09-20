package personas

import (
	"errors"
	"fmt"
)

// Base error types
var (
	ErrPersonaNotFound       = errors.New("persona not found")
	ErrInvalidRequest        = errors.New("invalid selection request")
	ErrConfigInvalid         = errors.New("invalid persona configuration")
	ErrTooManySelections     = errors.New("too many concurrent selections")
	ErrSelectionTimeout      = errors.New("persona selection timeout")
	ErrCacheUnavailable      = errors.New("cache temporarily unavailable")
	ErrSemanticMatcherFailed = errors.New("semantic matcher failed")
	ErrPersonaUnavailable    = errors.New("persona temporarily unavailable")
	ErrConfigNotFound        = errors.New("configuration file not found")
	ErrManagerClosed         = errors.New("persona manager is closed")
)

// SelectionError represents an error during persona selection
type SelectionError struct {
	RequestID string
	PersonaID string
	Cause     error
	Context   map[string]interface{}
}

func (e *SelectionError) Error() string {
	if e.PersonaID != "" {
		return fmt.Sprintf("selection error for persona %s (request %s): %v",
			e.PersonaID, e.RequestID, e.Cause)
	}
	return fmt.Sprintf("selection error (request %s): %v", e.RequestID, e.Cause)
}

func (e *SelectionError) Unwrap() error {
	return e.Cause
}

// ConfigError represents a configuration-related error
type ConfigError struct {
	File    string
	Section string
	Field   string
	Cause   error
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("config error in %s[%s].%s: %v",
		e.File, e.Section, e.Field, e.Cause)
}

func (e *ConfigError) Unwrap() error {
	return e.Cause
}

// ErrorClassifier helps classify errors for appropriate handling
type ErrorClassifier struct {
	retryableErrors   map[error]bool
	timeoutErrors     map[error]bool
	degradationErrors map[error]bool
}

// NewErrorClassifier creates a new error classifier
func NewErrorClassifier() *ErrorClassifier {
	return &ErrorClassifier{
		retryableErrors: map[error]bool{
			ErrCacheUnavailable:      true,
			ErrSemanticMatcherFailed: true,
			ErrSelectionTimeout:      true,
		},
		timeoutErrors: map[error]bool{
			ErrSelectionTimeout: true,
		},
		degradationErrors: map[error]bool{
			ErrPersonaUnavailable:    true,
			ErrSemanticMatcherFailed: true,
			ErrCacheUnavailable:      true,
		},
	}
}

// IsRetryable returns true if the error is retryable
func (ec *ErrorClassifier) IsRetryable(err error) bool {
	return ec.retryableErrors[err]
}

// IsTimeout returns true if the error is a timeout
func (ec *ErrorClassifier) IsTimeout(err error) bool {
	return ec.timeoutErrors[err]
}

// RequiresDegradation returns true if the error requires graceful degradation
func (ec *ErrorClassifier) RequiresDegradation(err error) bool {
	return ec.degradationErrors[err]
}

// NewSelectionError creates a new selection error
func NewSelectionError(requestID, personaID string, cause error) *SelectionError {
	return &SelectionError{
		RequestID: requestID,
		PersonaID: personaID,
		Cause:     cause,
		Context:   make(map[string]interface{}),
	}
}

// WithContext adds context to a selection error
func (e *SelectionError) WithContext(key string, value interface{}) *SelectionError {
	e.Context[key] = value
	return e
}

// NewConfigError creates a new configuration error
func NewConfigError(file, section, field string, cause error) *ConfigError {
	return &ConfigError{
		File:    file,
		Section: section,
		Field:   field,
		Cause:   cause,
	}
}
