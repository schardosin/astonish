package entstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	orgent "github.com/SAP/astonish/ent/org"
	"github.com/SAP/astonish/ent/org/orgauditlog"
	"github.com/SAP/astonish/pkg/store"
)

// orgAuditStore implements store.AuditStore for org-level audit logs.
type orgAuditStore struct {
	client *orgent.Client
}

var _ store.AuditStore = (*orgAuditStore)(nil)

func (as *orgAuditStore) Log(ctx context.Context, entry *store.AuditEntry) error {
	uid, err := uuid.Parse(entry.UserID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}

	create := as.client.OrgAuditLog.Create().
		SetUserID(uid).
		SetAction(entry.Action).
		SetResource(entry.Resource)

	if entry.TeamID != "" {
		create.SetTeamID(entry.TeamID)
	}
	if entry.Detail != nil {
		if detailMap, ok := entry.Detail.(map[string]any); ok {
			create.SetDetail(detailMap)
		}
	}
	if entry.IPAddress != "" {
		create.SetIPAddress(entry.IPAddress)
	}
	if entry.SessionID != "" {
		create.SetSessionID(entry.SessionID)
	}
	if !entry.Timestamp.IsZero() {
		create.SetTimestamp(entry.Timestamp)
	}

	created, err := create.Save(ctx)
	if err != nil {
		return err
	}
	entry.ID = int64(created.ID)
	entry.Timestamp = created.Timestamp
	return nil
}

func (as *orgAuditStore) Query(ctx context.Context, filter store.AuditFilter) ([]*store.AuditEntry, error) {
	q := as.client.OrgAuditLog.Query()

	if filter.UserID != "" {
		uid, err := uuid.Parse(filter.UserID)
		if err == nil {
			q = q.Where(orgauditlog.UserIDEQ(uid))
		}
	}
	if filter.Action != "" {
		q = q.Where(orgauditlog.ActionEQ(filter.Action))
	}
	if filter.Resource != "" {
		q = q.Where(orgauditlog.ResourceEQ(filter.Resource))
	}
	if !filter.Since.IsZero() {
		q = q.Where(orgauditlog.TimestampGTE(filter.Since))
	}
	if !filter.Until.IsZero() {
		q = q.Where(orgauditlog.TimestampLTE(filter.Until))
	}

	q = q.Order(orgauditlog.ByTimestamp())

	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}

	ents, err := q.All(ctx)
	if err != nil {
		return nil, err
	}

	entries := make([]*store.AuditEntry, len(ents))
	for i, e := range ents {
		entries[i] = entOrgAuditLogToStore(e)
	}
	return entries, nil
}

func entOrgAuditLogToStore(e *orgent.OrgAuditLog) *store.AuditEntry {
	entry := &store.AuditEntry{
		ID:        int64(e.ID),
		Timestamp: e.Timestamp,
		UserID:    e.UserID.String(),
		Action:    e.Action,
		Resource:  e.Resource,
	}
	if e.TeamID != nil {
		entry.TeamID = *e.TeamID
	}
	if e.Detail != nil {
		entry.Detail = e.Detail
	}
	if e.IPAddress != nil {
		entry.IPAddress = *e.IPAddress
	}
	if e.SessionID != nil {
		entry.SessionID = *e.SessionID
	}
	return entry
}
