package entstore

import (
	"context"
	"fmt"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	teament "github.com/SAP/astonish/ent/team"
	"github.com/SAP/astonish/ent/team/teamauditlog"
	"github.com/SAP/astonish/pkg/store"
)

// teamAuditStore implements store.AuditStore using the Ent team client.
type teamAuditStore struct {
	client *teament.Client
}

var _ store.AuditStore = (*teamAuditStore)(nil)

func (s *teamAuditStore) Log(ctx context.Context, entry *store.AuditEntry) error {
	userID := uuid.Nil
	if entry.UserID != "" {
		if id, err := uuid.Parse(entry.UserID); err == nil {
			userID = id
		}
	}

	create := s.client.TeamAuditLog.Create().
		SetUserID(userID).
		SetAction(entry.Action).
		SetResource(entry.Resource)

	if !entry.Timestamp.IsZero() {
		create.SetTimestamp(entry.Timestamp)
	}

	if entry.Detail != nil {
		if detailMap, ok := entry.Detail.(map[string]any); ok {
			create.SetDetail(detailMap)
		}
	}

	if entry.SessionID != "" {
		create.SetSessionID(entry.SessionID)
	}

	_, err := create.Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: AuditStore.Log: %w", err)
	}
	return nil
}

func (s *teamAuditStore) Query(ctx context.Context, filter store.AuditFilter) ([]*store.AuditEntry, error) {
	q := s.client.TeamAuditLog.Query()

	if filter.UserID != "" {
		if uid, err := uuid.Parse(filter.UserID); err == nil {
			q = q.Where(teamauditlog.UserIDEQ(uid))
		}
	}
	if filter.Action != "" {
		q = q.Where(teamauditlog.ActionEQ(filter.Action))
	}
	if filter.Resource != "" {
		q = q.Where(teamauditlog.ResourceEQ(filter.Resource))
	}
	if !filter.Since.IsZero() {
		q = q.Where(teamauditlog.TimestampGTE(filter.Since))
	}
	if !filter.Until.IsZero() {
		q = q.Where(teamauditlog.TimestampLTE(filter.Until))
	}

	q = q.Order(teamauditlog.ByTimestamp(sql.OrderDesc()))

	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}

	logs, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: AuditStore.Query: %w", err)
	}

	entries := make([]*store.AuditEntry, len(logs))
	for i, l := range logs {
		entries[i] = &store.AuditEntry{
			ID:        int64(l.ID),
			Timestamp: l.Timestamp,
			UserID:    l.UserID.String(),
			Action:    l.Action,
			Resource:  l.Resource,
			Detail:    l.Detail,
		}
		if l.SessionID != nil {
			entries[i].SessionID = *l.SessionID
		}
	}
	return entries, nil
}
