package fleet

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/schardosin/astonish/pkg/store"
)

type memMailbox struct {
	msgs []store.FleetMailboxMessage
}

func (m *memMailbox) Deliver(_ context.Context, sessionID string, msg store.FleetMailboxMessage, recipients []string) error {
	for _, r := range recipients {
		cp := msg
		cp.ID = uuid.New()
		cp.SessionID = sessionID
		cp.Recipient = r
		cp.DeliveryStatus = "delivered"
		m.msgs = append(m.msgs, cp)
	}
	return nil
}
func (m *memMailbox) Poll(_ context.Context, sessionID, recipient string, _ time.Time) ([]store.FleetMailboxMessage, error) {
	var out []store.FleetMailboxMessage
	for _, msg := range m.msgs {
		if msg.SessionID == sessionID && msg.Recipient == recipient {
			out = append(out, msg)
		}
	}
	return out, nil
}
func (m *memMailbox) MarkRead(_ context.Context, _ []uuid.UUID) error { return nil }
func (m *memMailbox) ListForSession(_ context.Context, sessionID string) ([]store.FleetMailboxMessage, error) {
	var out []store.FleetMailboxMessage
	for _, msg := range m.msgs {
		if msg.SessionID == sessionID {
			out = append(out, msg)
		}
	}
	return out, nil
}

type memTaskBoard struct {
	tasks []store.FleetTask
}

func (b *memTaskBoard) Post(_ context.Context, task store.FleetTask) (*store.FleetTask, error) {
	task.ID = uuid.New()
	task.Status = "open"
	task.CreatedAt = time.Now()
	task.UpdatedAt = task.CreatedAt
	b.tasks = append(b.tasks, task)
	cp := task
	return &cp, nil
}
func (b *memTaskBoard) Claim(_ context.Context, sessionID, agentKey string, capabilities map[string]bool, _ string) (*store.FleetTask, error) {
	for i := range b.tasks {
		t := &b.tasks[i]
		if t.SessionID != sessionID || t.Status != "open" {
			continue
		}
		ok := true
		for _, req := range t.RequiredCapabilities {
			if !capabilities[req] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		t.Status = "claimed"
		t.ClaimedBy = agentKey
		now := time.Now()
		t.ClaimedAt = &now
		cp := *t
		return &cp, nil
	}
	return nil, nil
}
func (b *memTaskBoard) Complete(_ context.Context, id uuid.UUID, result map[string]any) error {
	for i := range b.tasks {
		if b.tasks[i].ID == id {
			b.tasks[i].Status = "done"
			b.tasks[i].Result = result
			return nil
		}
	}
	return nil
}
func (b *memTaskBoard) Fail(_ context.Context, id uuid.UUID, reason string) error {
	for i := range b.tasks {
		if b.tasks[i].ID == id {
			b.tasks[i].Status = "failed"
			b.tasks[i].Result = map[string]any{"reason": reason}
			return nil
		}
	}
	return nil
}
func (b *memTaskBoard) List(_ context.Context, sessionID string, statuses ...string) ([]store.FleetTask, error) {
	want := map[string]bool{}
	for _, s := range statuses {
		want[s] = true
	}
	var out []store.FleetTask
	for _, t := range b.tasks {
		if t.SessionID != sessionID {
			continue
		}
		if len(want) == 0 || want[t.Status] {
			out = append(out, t)
		}
	}
	return out, nil
}

func TestBuildMailboxThreadContext(t *testing.T) {
	mb := &memMailbox{}
	_ = mb.Deliver(context.Background(), "s1", store.FleetMailboxMessage{
		Sender: "customer", Body: "please design the API", CreatedAt: time.Now(),
	}, []string{"architect"})
	ctxText, err := BuildMailboxThreadContext(context.Background(), mb, "s1", "architect")
	if err != nil {
		t.Fatal(err)
	}
	if ctxText == "" || !strings.Contains(ctxText, "please design the API") {
		t.Fatalf("unexpected context: %q", ctxText)
	}
}

func TestClaimAndEnqueueTasks(t *testing.T) {
	board := &memTaskBoard{}
	_, err := board.Post(context.Background(), store.FleetTask{
		SessionID:            "s1",
		Title:                "Design",
		RequiredCapabilities: []string{"design.architecture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	fs := &FleetSession{
		ID: "s1",
		FleetConfig: &FleetConfig{
			Agents: map[string]FleetAgentConfig{
				"architect": {
					Name:         "Architect",
					Capabilities: map[string]bool{"design.architecture": true},
					TaskPolicy:   &AgentTaskPolicy{Claims: []string{"design.architecture"}},
				},
				"dev": {
					Name:         "Dev",
					Capabilities: map[string]bool{"code.write": true},
					TaskPolicy:   &AgentTaskPolicy{Claims: []string{"code.write"}},
				},
			},
			Settings: FleetSettings{TaskBoard: &TaskBoardConfig{ClaimPolicy: "capability_match"}},
		},
	}
	ctx := store.WithFleetTaskBoardStore(context.Background(), board)
	claimed := fs.claimAndEnqueueTasks(ctx)
	if len(claimed) != 1 || claimed[0] != "architect" {
		t.Fatalf("claimed = %v, want [architect]", claimed)
	}
}

func TestApplyQuietExitGate_ContinuesWhenOpenTaskClaimable(t *testing.T) {
	board := &memTaskBoard{}
	_, err := board.Post(context.Background(), store.FleetTask{
		SessionID:            "s1",
		Title:                "Design",
		RequiredCapabilities: []string{"design.architecture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	fs := &FleetSession{
		ID: "s1",
		FleetConfig: &FleetConfig{
			Agents: map[string]FleetAgentConfig{
				"architect": {
					Name:         "Architect",
					Capabilities: map[string]bool{"design.architecture": true},
					TaskPolicy:   &AgentTaskPolicy{Claims: []string{"design.architecture"}},
				},
			},
			Communication: &CommunicationConfig{
				Flow: []CommunicationNode{{Role: "architect", EntryPoint: true, TalksTo: []string{"customer"}}},
			},
			Settings: FleetSettings{TaskBoard: &TaskBoardConfig{ClaimPolicy: "capability_match"}},
		},
		Headless: true,
	}
	// Route "none" exit: ball may be customer but waitingAgent is empty.
	fs.mu.Lock()
	fs.ballWithCustomer = true
	fs.mu.Unlock()

	ctx := store.WithFleetTaskBoardStore(context.Background(), board)
	next, stop := fs.applyQuietExitGate(ctx, nil, true)
	if stop {
		t.Fatal("expected continue, got stop")
	}
	if len(next) != 1 || next[0] != "architect" {
		t.Fatalf("next = %v, want [architect]", next)
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if fs.ballWithCustomer {
		t.Fatal("expected ball back with agents after continuing")
	}
}

func TestApplyQuietExitGate_StopsWhenWaitingOnCustomerEvenWithTasks(t *testing.T) {
	board := &memTaskBoard{}
	_, err := board.Post(context.Background(), store.FleetTask{
		SessionID:            "s1",
		Title:                "Design",
		RequiredCapabilities: []string{"design.architecture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	fs := &FleetSession{
		ID: "s1",
		FleetConfig: &FleetConfig{
			Agents: map[string]FleetAgentConfig{
				"po": {
					Name:       "PO",
					TaskPolicy: &AgentTaskPolicy{Claims: []string{"general"}},
				},
				"architect": {
					Name:         "Architect",
					Capabilities: map[string]bool{"design.architecture": true},
					TaskPolicy:   &AgentTaskPolicy{Claims: []string{"design.architecture"}},
				},
			},
			Communication: &CommunicationConfig{
				Flow: []CommunicationNode{{Role: "po", EntryPoint: true, TalksTo: []string{"customer", "architect"}}},
			},
			Settings: FleetSettings{TaskBoard: &TaskBoardConfig{ClaimPolicy: "capability_match"}},
		},
		Headless: true,
	}
	fs.mu.Lock()
	fs.waitingAgent = "po"
	fs.ballWithCustomer = true
	fs.mu.Unlock()

	ctx := store.WithFleetTaskBoardStore(context.Background(), board)
	next, stop := fs.applyQuietExitGate(ctx, nil, true)
	if !stop {
		t.Fatalf("expected hard stop while waiting on customer, next=%v", next)
	}
	if len(next) != 0 {
		t.Fatalf("next = %v, want empty", next)
	}
	if state, _ := fs.GetState(); state != StateStopped {
		t.Fatalf("state = %q, want stopped", state)
	}
}

func TestApplyQuietExitGate_StopsWhenNoIncompleteTasks(t *testing.T) {
	board := &memTaskBoard{}
	fs := &FleetSession{
		ID: "s1",
		FleetConfig: &FleetConfig{
			Agents: map[string]FleetAgentConfig{
				"po": {
					Name:       "PO",
					TaskPolicy: &AgentTaskPolicy{Claims: []string{"general"}},
				},
			},
			Communication: &CommunicationConfig{
				Flow: []CommunicationNode{{Role: "po", EntryPoint: true, TalksTo: []string{"customer"}}},
			},
			Settings: FleetSettings{TaskBoard: &TaskBoardConfig{ClaimPolicy: "capability_match"}},
		},
		Headless: true,
	}
	ctx := store.WithFleetTaskBoardStore(context.Background(), board)
	next, stop := fs.applyQuietExitGate(ctx, nil, true)
	if !stop {
		t.Fatalf("expected stop, next=%v", next)
	}
	if len(next) != 0 {
		t.Fatalf("next = %v, want empty", next)
	}
	if state, _ := fs.GetState(); state != StateStopped {
		t.Fatalf("state = %q, want stopped", state)
	}
}

func TestApplyQuietExitGate_TriagesUnclaimableIncompleteTasks(t *testing.T) {
	board := &memTaskBoard{}
	_, err := board.Post(context.Background(), store.FleetTask{
		SessionID:            "s1",
		Title:                "Need security review",
		RequiredCapabilities: []string{"security.review"},
	})
	if err != nil {
		t.Fatal(err)
	}
	fs := &FleetSession{
		ID: "s1",
		FleetConfig: &FleetConfig{
			Agents: map[string]FleetAgentConfig{
				"po": {
					Name:       "PO",
					TaskPolicy: &AgentTaskPolicy{Claims: []string{"general"}},
				},
			},
			Communication: &CommunicationConfig{
				Flow: []CommunicationNode{{Role: "po", EntryPoint: true, TalksTo: []string{"customer"}}},
			},
			Settings: FleetSettings{TaskBoard: &TaskBoardConfig{ClaimPolicy: "capability_match"}},
		},
		Headless: true,
	}
	ctx := store.WithFleetTaskBoardStore(context.Background(), board)
	next, stop := fs.applyQuietExitGate(ctx, nil, true)
	if stop {
		t.Fatal("expected triage continue, got stop")
	}
	if len(next) != 1 || next[0] != "po" {
		t.Fatalf("next = %v, want [po]", next)
	}
}

func TestHasIncompleteTasks(t *testing.T) {
	board := &memTaskBoard{}
	fs := &FleetSession{ID: "s1"}
	ctx := store.WithFleetTaskBoardStore(context.Background(), board)
	if fs.hasIncompleteTasks(ctx) {
		t.Fatal("empty board should have no incomplete tasks")
	}
	_, err := board.Post(context.Background(), store.FleetTask{SessionID: "s1", Title: "Open"})
	if err != nil {
		t.Fatal(err)
	}
	if !fs.hasIncompleteTasks(ctx) {
		t.Fatal("open task should count as incomplete")
	}
}
