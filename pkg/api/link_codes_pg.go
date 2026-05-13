package api

import (
	"context"
	"strings"

	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// pgLinkCodeBackend implements LinkCodeBackend using PostgreSQL.
// Enables stateless horizontal scaling by storing link codes in PG
// instead of process memory.
type pgLinkCodeBackend struct {
	store *pgstore.PGLinkCodeStore
}

// NewPGLinkCodeBackend creates a PG-backed link code backend.
func NewPGLinkCodeBackend(store *pgstore.PGLinkCodeStore) LinkCodeBackend {
	return &pgLinkCodeBackend{store: store}
}

func (b *pgLinkCodeBackend) Generate(ctx context.Context, userID, email, channel string) (string, error) {
	code := generateLinkCode()
	err := b.store.Generate(ctx, code, userID, email, channel)
	if err != nil {
		return "", err
	}
	return code, nil
}

func (b *pgLinkCodeBackend) Consume(ctx context.Context, code string) *PendingLink {
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
var _ LinkCodeBackend = (*pgLinkCodeBackend)(nil)
