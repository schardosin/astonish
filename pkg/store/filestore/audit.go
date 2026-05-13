package filestore

import (
	"context"

	"github.com/schardosin/astonish/pkg/store"
)

// NoopAuditStore is a no-op audit store for personal mode.
// In personal mode, there is no audit logging requirement.
// This will be replaced by a real implementation in the PG store.
type NoopAuditStore struct{}

// NewNoopAuditStore creates a no-op audit store.
func NewNoopAuditStore() store.AuditStore {
	return &NoopAuditStore{}
}

func (n *NoopAuditStore) Log(_ context.Context, _ *store.AuditEntry) error {
	return nil
}

func (n *NoopAuditStore) Query(_ context.Context, _ store.AuditFilter) ([]*store.AuditEntry, error) {
	return nil, nil
}

// Compile-time check.
var _ store.AuditStore = (*NoopAuditStore)(nil)
