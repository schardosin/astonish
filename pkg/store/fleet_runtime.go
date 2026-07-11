package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// FleetRunStateSnapshot is a durable snapshot of a fleet session's runtime state.
type FleetRunStateSnapshot struct {
	SessionID       string
	PlanKey         string
	State           string // "idle" | "processing" | "waiting_for_customer" | "stopped"
	ActiveAgents    []string
	WaitingAgent    string
	Ball            string
	Progress        map[string]any
	LastHeartbeatAt time.Time
}

// FleetRunStateStore persists fleet session runtime state for recovery.
type FleetRunStateStore interface {
	Upsert(ctx context.Context, snap FleetRunStateSnapshot) error
	Get(ctx context.Context, sessionID string) (*FleetRunStateSnapshot, error)
	ListRecoverable(ctx context.Context, planKey string) ([]FleetRunStateSnapshot, error)
	Heartbeat(ctx context.Context, sessionID string, at time.Time) error
	Delete(ctx context.Context, sessionID string) error
}

// FleetMailboxMessage is a durable per-recipient mailbox message.
type FleetMailboxMessage struct {
	ID             uuid.UUID
	SessionID      string
	Recipient      string // agent key OR "customer"
	Sender         string // agent key, "customer", or "system"
	Body           string
	Mentions       []string
	Metadata       map[string]any
	DeliveryStatus string // "pending" | "delivered" | "read"
	DeliveredAt    *time.Time
	ReadAt         *time.Time
	CreatedAt      time.Time
}

// FleetMailboxStore persists per-recipient mailbox messages for fleet handoffs.
type FleetMailboxStore interface {
	Deliver(ctx context.Context, sessionID string, msg FleetMailboxMessage, recipients []string) error
	Poll(ctx context.Context, sessionID, recipient string, sinceCreatedAt time.Time) ([]FleetMailboxMessage, error)
	MarkRead(ctx context.Context, ids []uuid.UUID) error
	ListForSession(ctx context.Context, sessionID string) ([]FleetMailboxMessage, error)
}

// FleetTask is a durable task-board entry.
type FleetTask struct {
	ID                   uuid.UUID
	SessionID            string
	Title                string
	Description          string
	RequiredCapabilities []string
	ClaimedBy            string
	Status               string // "open" | "claimed" | "in_progress" | "done" | "failed" | "cancelled"
	Result               map[string]any
	ParentTaskID         string
	ClaimedAt            *time.Time
	CompletedAt          *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// FleetTaskBoardStore persists the durable task board for a fleet session.
type FleetTaskBoardStore interface {
	Post(ctx context.Context, task FleetTask) (*FleetTask, error)
	Claim(ctx context.Context, sessionID, agentKey string, capabilities map[string]bool, policy string) (*FleetTask, error)
	Complete(ctx context.Context, id uuid.UUID, result map[string]any) error
	Fail(ctx context.Context, id uuid.UUID, reason string) error
	List(ctx context.Context, sessionID string, statuses ...string) ([]FleetTask, error)
}
