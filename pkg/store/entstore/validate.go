package entstore

import (
	"fmt"
	"regexp"
)

// slugPattern permits only ASCII letters, digits, hyphens, and underscores.
// This prevents path-traversal characters (.., /, \) and SQL injection payloads
// from reaching database identifier construction or filesystem path joins.
var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// validateSlug checks that a slug (org, team, app) contains only safe characters.
// It returns an error if the slug is empty or contains characters outside [a-zA-Z0-9_-].
func validateSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("slug must not be empty")
	}
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("invalid slug %q: must start with a letter or digit and contain only [a-zA-Z0-9_-]", slug)
	}
	return nil
}

// validateUserID checks that a user ID contains only UUID-safe characters
// (hex digits, hyphens) or other identity-provider-safe characters.
// This prevents path traversal via user IDs in SQLite file paths.
var userIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_@.+-]*$`)

func validateUserID(userID string) error {
	if userID == "" {
		return fmt.Errorf("user ID must not be empty")
	}
	if !userIDPattern.MatchString(userID) {
		return fmt.Errorf("invalid user ID %q: contains unsafe characters", userID)
	}
	return nil
}
