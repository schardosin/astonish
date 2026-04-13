package session

// ShortID returns the shortest prefix of id that uniquely identifies it
// among allIDs. The minimum returned length is 8 characters (or the full
// ID if shorter). If the ID cannot be uniquely shortened (e.g., it is a
// prefix of another ID), the full ID is returned.
func ShortID(id string, allIDs []string) string {
	const minLen = 8

	// Start from minLen and grow until the prefix is unique
	for prefixLen := minLen; prefixLen < len(id); prefixLen++ {
		prefix := id[:prefixLen]
		unique := true
		for _, other := range allIDs {
			if other == id {
				continue
			}
			if len(other) >= prefixLen && other[:prefixLen] == prefix {
				unique = false
				break
			}
		}
		if unique {
			return prefix
		}
	}

	// Full ID is needed for uniqueness (or ID is <= minLen)
	if len(id) <= minLen {
		return id
	}
	return id
}

// ShortIDs computes the shortest unique prefix for each ID in the list.
// Returns a map from full ID to its shortest unique display string.
func ShortIDs(allIDs []string) map[string]string {
	result := make(map[string]string, len(allIDs))
	for _, id := range allIDs {
		result[id] = ShortID(id, allIDs)
	}
	return result
}

// SafeShortID returns a shortened session ID for contexts where the full
// list of IDs is not available (log messages, single-session displays).
// It truncates to maxLen characters, which is more generous than the old
// 8-char limit to accommodate structured IDs like "email:direct:user@...".
func SafeShortID(id string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 16
	}
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen]
}
