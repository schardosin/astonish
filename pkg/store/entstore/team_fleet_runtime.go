package entstore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	teament "github.com/SAP/astonish/ent/team"
	"github.com/SAP/astonish/ent/team/fleetmailboxmessage"
	"github.com/SAP/astonish/ent/team/fleetrunstate"
	"github.com/SAP/astonish/ent/team/fleettask"
	"github.com/SAP/astonish/pkg/store"
)

type teamFleetRunStateStore struct {
	client *teament.Client
}

var _ store.FleetRunStateStore = (*teamFleetRunStateStore)(nil)

func (s *teamFleetRunStateStore) Upsert(ctx context.Context, snap store.FleetRunStateSnapshot) error {
	if snap.SessionID == "" {
		return fmt.Errorf("entstore: FleetRunStateStore.Upsert: session_id is required")
	}
	if snap.PlanKey == "" {
		return fmt.Errorf("entstore: FleetRunStateStore.Upsert: plan_key is required")
	}
	if snap.State == "" {
		snap.State = "idle"
	}
	if snap.Ball == "" {
		snap.Ball = "agents"
	}
	if snap.Progress == nil {
		snap.Progress = map[string]any{}
	}
	if snap.LastHeartbeatAt.IsZero() {
		snap.LastHeartbeatAt = time.Now()
	}

	now := time.Now()
	update := s.client.FleetRunState.Update().
		Where(fleetrunstate.SessionIDEQ(snap.SessionID)).
		SetPlanKey(snap.PlanKey).
		SetState(snap.State).
		SetActiveAgents(snap.ActiveAgents).
		SetBall(snap.Ball).
		SetProgress(snap.Progress).
		SetLastHeartbeatAt(snap.LastHeartbeatAt).
		SetUpdatedAt(now)
	if snap.WaitingAgent == "" {
		update.ClearWaitingAgent()
	} else {
		update.SetWaitingAgent(snap.WaitingAgent)
	}
	n, err := update.Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: FleetRunStateStore.Upsert update: %w", err)
	}
	if n > 0 {
		return nil
	}

	create := s.client.FleetRunState.Create().
		SetSessionID(snap.SessionID).
		SetPlanKey(snap.PlanKey).
		SetState(snap.State).
		SetActiveAgents(snap.ActiveAgents).
		SetBall(snap.Ball).
		SetProgress(snap.Progress).
		SetLastHeartbeatAt(snap.LastHeartbeatAt).
		SetUpdatedAt(now)
	if snap.WaitingAgent != "" {
		create.SetWaitingAgent(snap.WaitingAgent)
	}
	if _, err := create.Save(ctx); err != nil {
		return fmt.Errorf("entstore: FleetRunStateStore.Upsert create: %w", err)
	}
	return nil
}

func (s *teamFleetRunStateStore) Get(ctx context.Context, sessionID string) (*store.FleetRunStateSnapshot, error) {
	ent, err := s.client.FleetRunState.Query().
		Where(fleetrunstate.SessionIDEQ(sessionID)).
		Only(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("entstore: FleetRunStateStore.Get: %w", err)
	}
	snap := fleetRunStateToStore(ent)
	return &snap, nil
}

func (s *teamFleetRunStateStore) ListRecoverable(ctx context.Context, planKey string) ([]store.FleetRunStateSnapshot, error) {
	rows, err := s.client.FleetRunState.Query().
		Where(
			fleetrunstate.PlanKeyEQ(planKey),
			fleetrunstate.StateNEQ("stopped"),
		).
		Order(fleetrunstate.ByLastHeartbeatAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: FleetRunStateStore.ListRecoverable: %w", err)
	}
	out := make([]store.FleetRunStateSnapshot, 0, len(rows))
	for _, row := range rows {
		snap := fleetRunStateToStore(row)
		// Ball with the customer means waiting on a human reply — only
		// GitHub human-comment recovery should wake these sessions.
		if snap.Ball == "customer" {
			continue
		}
		out = append(out, snap)
	}
	return out, nil
}

func (s *teamFleetRunStateStore) Heartbeat(ctx context.Context, sessionID string, at time.Time) error {
	if at.IsZero() {
		at = time.Now()
	}
	n, err := s.client.FleetRunState.Update().
		Where(fleetrunstate.SessionIDEQ(sessionID)).
		SetLastHeartbeatAt(at).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: FleetRunStateStore.Heartbeat: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("entstore: FleetRunStateStore.Heartbeat: session %q not found", sessionID)
	}
	return nil
}

func (s *teamFleetRunStateStore) Delete(ctx context.Context, sessionID string) error {
	_, err := s.client.FleetRunState.Delete().
		Where(fleetrunstate.SessionIDEQ(sessionID)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("entstore: FleetRunStateStore.Delete: %w", err)
	}
	return nil
}

func fleetRunStateToStore(ent *teament.FleetRunState) store.FleetRunStateSnapshot {
	waitingAgent := ""
	if ent.WaitingAgent != nil {
		waitingAgent = *ent.WaitingAgent
	}
	progress := ent.Progress
	if progress == nil {
		progress = map[string]any{}
	}
	return store.FleetRunStateSnapshot{
		SessionID:       ent.SessionID,
		PlanKey:         ent.PlanKey,
		State:           ent.State,
		ActiveAgents:    append([]string(nil), ent.ActiveAgents...),
		WaitingAgent:    waitingAgent,
		Ball:            ent.Ball,
		Progress:        progress,
		LastHeartbeatAt: ent.LastHeartbeatAt,
	}
}

type teamFleetMailboxStore struct {
	client *teament.Client
}

var _ store.FleetMailboxStore = (*teamFleetMailboxStore)(nil)

func (s *teamFleetMailboxStore) Deliver(ctx context.Context, sessionID string, msg store.FleetMailboxMessage, recipients []string) error {
	if sessionID == "" {
		return fmt.Errorf("entstore: FleetMailboxStore.Deliver: session_id is required")
	}
	if len(recipients) == 0 {
		recipients = []string{msg.Recipient}
	}
	now := time.Now()
	creates := make([]*teament.FleetMailboxMessageCreate, 0, len(recipients))
	for _, recipient := range recipients {
		if recipient == "" {
			continue
		}
		id := msg.ID
		if len(recipients) > 1 || id == uuid.Nil {
			id = uuid.New()
		}
		status := msg.DeliveryStatus
		if status == "" {
			status = "delivered"
		}
		deliveredAt := msg.DeliveredAt
		if deliveredAt == nil && status == "delivered" {
			deliveredAt = &now
		}
		metadata := msg.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}
		create := s.client.FleetMailboxMessage.Create().
			SetID(id).
			SetSessionID(sessionID).
			SetRecipient(recipient).
			SetSender(msg.Sender).
			SetBody(msg.Body).
			SetMentions(msg.Mentions).
			SetMetadata(metadata).
			SetDeliveryStatus(status).
			SetNillableDeliveredAt(deliveredAt).
			SetNillableReadAt(msg.ReadAt)
		if !msg.CreatedAt.IsZero() {
			create.SetCreatedAt(msg.CreatedAt)
		}
		creates = append(creates, create)
	}
	if len(creates) == 0 {
		return nil
	}
	if _, err := s.client.FleetMailboxMessage.CreateBulk(creates...).Save(ctx); err != nil {
		return fmt.Errorf("entstore: FleetMailboxStore.Deliver: %w", err)
	}
	return nil
}

func (s *teamFleetMailboxStore) Poll(ctx context.Context, sessionID, recipient string, sinceCreatedAt time.Time) ([]store.FleetMailboxMessage, error) {
	query := s.client.FleetMailboxMessage.Query().
		Where(
			fleetmailboxmessage.SessionIDEQ(sessionID),
			fleetmailboxmessage.RecipientEQ(recipient),
		)
	if !sinceCreatedAt.IsZero() {
		query = query.Where(fleetmailboxmessage.CreatedAtGT(sinceCreatedAt))
	}
	rows, err := query.Order(fleetmailboxmessage.ByCreatedAt()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: FleetMailboxStore.Poll: %w", err)
	}
	return fleetMailboxMessagesToStore(rows), nil
}

func (s *teamFleetMailboxStore) MarkRead(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	_, err := s.client.FleetMailboxMessage.Update().
		Where(fleetmailboxmessage.IDIn(ids...)).
		SetDeliveryStatus("read").
		SetReadAt(now).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: FleetMailboxStore.MarkRead: %w", err)
	}
	return nil
}

func (s *teamFleetMailboxStore) ListForSession(ctx context.Context, sessionID string) ([]store.FleetMailboxMessage, error) {
	rows, err := s.client.FleetMailboxMessage.Query().
		Where(fleetmailboxmessage.SessionIDEQ(sessionID)).
		Order(fleetmailboxmessage.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: FleetMailboxStore.ListForSession: %w", err)
	}
	return fleetMailboxMessagesToStore(rows), nil
}

func fleetMailboxMessagesToStore(rows []*teament.FleetMailboxMessage) []store.FleetMailboxMessage {
	out := make([]store.FleetMailboxMessage, 0, len(rows))
	for _, row := range rows {
		metadata := row.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}
		out = append(out, store.FleetMailboxMessage{
			ID:             row.ID,
			SessionID:      row.SessionID,
			Recipient:      row.Recipient,
			Sender:         row.Sender,
			Body:           row.Body,
			Mentions:       append([]string(nil), row.Mentions...),
			Metadata:       metadata,
			DeliveryStatus: row.DeliveryStatus,
			DeliveredAt:    row.DeliveredAt,
			ReadAt:         row.ReadAt,
			CreatedAt:      row.CreatedAt,
		})
	}
	return out
}

type teamFleetTaskBoardStore struct {
	client *teament.Client
}

var _ store.FleetTaskBoardStore = (*teamFleetTaskBoardStore)(nil)

func (s *teamFleetTaskBoardStore) Post(ctx context.Context, task store.FleetTask) (*store.FleetTask, error) {
	if task.SessionID == "" {
		return nil, fmt.Errorf("entstore: FleetTaskBoardStore.Post: session_id is required")
	}
	if task.Title == "" {
		return nil, fmt.Errorf("entstore: FleetTaskBoardStore.Post: title is required")
	}
	id := task.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	status := task.Status
	if status == "" {
		status = "open"
	}
	result := task.Result
	if result == nil {
		result = map[string]any{}
	}
	create := s.client.FleetTask.Create().
		SetID(id).
		SetSessionID(task.SessionID).
		SetTitle(task.Title).
		SetDescription(task.Description).
		SetRequiredCapabilities(task.RequiredCapabilities).
		SetStatus(status).
		SetResult(result).
		SetNillableClaimedAt(task.ClaimedAt).
		SetNillableCompletedAt(task.CompletedAt)
	if task.ClaimedBy != "" {
		create.SetClaimedBy(task.ClaimedBy)
	}
	if task.ParentTaskID != "" {
		create.SetParentTaskID(task.ParentTaskID)
	}
	row, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: FleetTaskBoardStore.Post: %w", err)
	}
	out := fleetTaskToStore(row)
	return &out, nil
}

func (s *teamFleetTaskBoardStore) Claim(ctx context.Context, sessionID, agentKey string, capabilities map[string]bool, policy string) (*store.FleetTask, error) {
	if policy == "supervisor_assigned" && !capabilities["supervisor"] {
		return nil, nil
	}
	rows, err := s.client.FleetTask.Query().
		Where(
			fleettask.SessionIDEQ(sessionID),
			fleettask.StatusEQ("open"),
		).
		Order(fleettask.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: FleetTaskBoardStore.Claim query: %w", err)
	}
	row := selectClaimableTask(rows, capabilities, policy)
	if row == nil {
		return nil, nil
	}
	now := time.Now()
	n, err := s.client.FleetTask.Update().
		Where(
			fleettask.IDEQ(row.ID),
			fleettask.StatusEQ("open"),
		).
		SetClaimedBy(agentKey).
		SetStatus("claimed").
		SetClaimedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: FleetTaskBoardStore.Claim update: %w", err)
	}
	if n == 0 {
		return nil, nil
	}
	claimed, err := s.client.FleetTask.Get(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("entstore: FleetTaskBoardStore.Claim reload: %w", err)
	}
	out := fleetTaskToStore(claimed)
	return &out, nil
}

func (s *teamFleetTaskBoardStore) Complete(ctx context.Context, id uuid.UUID, result map[string]any) error {
	if result == nil {
		result = map[string]any{}
	}
	now := time.Now()
	n, err := s.client.FleetTask.Update().
		Where(fleettask.IDEQ(id)).
		SetStatus("done").
		SetResult(result).
		SetCompletedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: FleetTaskBoardStore.Complete: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("entstore: FleetTaskBoardStore.Complete: task %s not found", id)
	}
	return nil
}

func (s *teamFleetTaskBoardStore) Fail(ctx context.Context, id uuid.UUID, reason string) error {
	now := time.Now()
	result := map[string]any{"reason": reason}
	n, err := s.client.FleetTask.Update().
		Where(fleettask.IDEQ(id)).
		SetStatus("failed").
		SetResult(result).
		SetCompletedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: FleetTaskBoardStore.Fail: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("entstore: FleetTaskBoardStore.Fail: task %s not found", id)
	}
	return nil
}

func (s *teamFleetTaskBoardStore) List(ctx context.Context, sessionID string, statuses ...string) ([]store.FleetTask, error) {
	query := s.client.FleetTask.Query().Where(fleettask.SessionIDEQ(sessionID))
	if len(statuses) > 0 {
		query = query.Where(fleettask.StatusIn(statuses...))
	}
	rows, err := query.Order(fleettask.ByCreatedAt()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: FleetTaskBoardStore.List: %w", err)
	}
	out := make([]store.FleetTask, 0, len(rows))
	for _, row := range rows {
		out = append(out, fleetTaskToStore(row))
	}
	return out, nil
}

func selectClaimableTask(rows []*teament.FleetTask, capabilities map[string]bool, policy string) *teament.FleetTask {
	var best *teament.FleetTask
	bestScore := -1
	for _, row := range rows {
		score, ok := taskCapabilityScore(row.RequiredCapabilities, capabilities)
		if !ok {
			continue
		}
		if policy != "capability_match" {
			return row
		}
		if score > bestScore {
			best = row
			bestScore = score
		}
	}
	return best
}

func taskCapabilityScore(required []string, capabilities map[string]bool) (int, bool) {
	if len(required) == 0 {
		return 0, true
	}
	score := 0
	for _, cap := range required {
		if !capabilities[cap] {
			return 0, false
		}
		score++
	}
	return score, true
}

func fleetTaskToStore(row *teament.FleetTask) store.FleetTask {
	claimedBy := ""
	if row.ClaimedBy != nil {
		claimedBy = *row.ClaimedBy
	}
	parentTaskID := ""
	if row.ParentTaskID != nil {
		parentTaskID = *row.ParentTaskID
	}
	result := row.Result
	if result == nil {
		result = map[string]any{}
	}
	return store.FleetTask{
		ID:                   row.ID,
		SessionID:            row.SessionID,
		Title:                row.Title,
		Description:          row.Description,
		RequiredCapabilities: append([]string(nil), row.RequiredCapabilities...),
		ClaimedBy:            claimedBy,
		Status:               row.Status,
		Result:               result,
		ParentTaskID:         parentTaskID,
		ClaimedAt:            row.ClaimedAt,
		CompletedAt:          row.CompletedAt,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
}
