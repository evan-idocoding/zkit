package tuning

import "errors"

var (
	// ErrInvalidKey indicates the key is empty or contains invalid characters.
	ErrInvalidKey = errors.New("tuning: invalid key")
	// ErrAlreadyRegistered indicates the same key is registered more than once.
	ErrAlreadyRegistered = errors.New("tuning: already registered")
	// ErrInvalidValue indicates a runtime Set value fails validation.
	ErrInvalidValue = errors.New("tuning: invalid value")
	// ErrTypeMismatch indicates the key exists but the type does not match.
	ErrTypeMismatch = errors.New("tuning: type mismatch")
	// ErrInvalidConfig indicates a registration-time configuration error.
	ErrInvalidConfig = errors.New("tuning: invalid config")
	// ErrNoLastValue indicates ResetToLastValue has no last value to restore.
	ErrNoLastValue = errors.New("tuning: no last value")

	// ErrNotFound indicates the key is not registered.
	ErrNotFound = errors.New("tuning: key not found")
	// ErrReentrantWrite indicates a write API is called from an onChange callback.
	ErrReentrantWrite = errors.New("tuning: re-entrant write in onChange callback")
)
