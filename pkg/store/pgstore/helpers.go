package pgstore

// nilIfEmpty returns nil if s is empty, otherwise a pointer to s.
// Used to map Go empty strings to SQL NULL for nullable text columns.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nullableString returns nil (SQL NULL) if s is empty, otherwise s as any.
// Used for nullable UUID/text columns in parameterized queries where the
// pgx driver needs an untyped nil to produce NULL.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
