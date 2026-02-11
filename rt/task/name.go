package task

import (
	"errors"
	"strings"
)

// normalizeName trims whitespace. Empty names remain empty.
func normalizeName(name string) string {
	return strings.TrimSpace(name)
}

func validateName(name string) error {
	// Empty means unnamed (allowed).
	if name == "" {
		return nil
	}
	// Keep it stable and ops/admin-friendly.
	// Allowed: [A-Za-z0-9._-]
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			if c == '/' {
				return errors.New("contains '/' (not allowed)")
			}
			if strings.ContainsRune(" \t\r\n", rune(c)) {
				return errors.New("contains whitespace (not allowed)")
			}
			return errors.New("contains invalid char (allowed: [A-Za-z0-9._-])")
		}
	}
	return nil
}
