package entstore

import "errors"

// errNotImpl is returned by stub store implementations.
var errNotImpl = errors.New("entstore: not yet implemented")

// nilStrPtr returns a *string pointer, or nil if the string is empty.
func nilStrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
