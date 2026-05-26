package api

import (
	"context"
	"strings"

	"github.com/schardosin/astonish/pkg/store"
)

// dbLinkCodeBackend implements LinkCodeBackend using any store.LinkCodeStore.
// Enables stateless horizontal scaling by storing link codes in the database
// (PG or SQLite) instead of process memory.
type dbLinkCodeBackend struct {
	store store.LinkCodeStore
}

// NewDBLinkCodeBackend creates a database-backed link code backend.
// Works with both PGLinkCodeStore and SQLiteLinkCodeStore.
func NewDBLinkCodeBackend(s store.LinkCodeStore) LinkCodeBackend {
	return &dbLinkCodeBackend{store: s}
}

func (b *dbLinkCodeBackend) Generate(ctx context.Context, userID, email, channel string) (string, error) {
	code := generateLinkCode()
	err := b.store.Generate(ctx, code, userID, email, channel)
	if err != nil {
		return "", err
	}
	return code, nil
}

func (b *dbLinkCodeBackend) Consume(ctx context.Context, code string) *PendingLink {
	code = strings.ToUpper(strings.TrimSpace(code))
	row, err := b.store.Consume(ctx, code)
	if err != nil || row == nil {
		return nil
	}
	return &PendingLink{
		Code:      row.Code,
		UserID:    row.UserID,
		Email:     row.Email,
		Channel:   row.Channel,
		CreatedAt: row.CreatedAt,
		ExpiresAt: row.ExpiresAt,
	}
}

// Compile-time interface check
var _ LinkCodeBackend = (*dbLinkCodeBackend)(nil)
