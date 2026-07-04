package store

import (
	"context"
	"time"
)

// NetworkPolicyAction represents the action for a network policy rule.
type NetworkPolicyAction string

const (
	NetworkPolicyAllow NetworkPolicyAction = "allow"
	NetworkPolicyDeny  NetworkPolicyAction = "deny"
)

// NetworkPolicyRule represents a single network access rule (allow or deny)
// for a specific host:port endpoint. Rules exist at platform, org, and team
// scopes. When merged, deny-wins-from-above: a platform deny cannot be
// overridden by an org or team allow.
type NetworkPolicyRule struct {
	ID        string              `json:"id"`
	Host      string              `json:"host"`   // exact host, *.example.com, or **.example.com
	Port      uint32              `json:"port"`   // 0 = any port
	Action    NetworkPolicyAction `json:"action"` // "allow" or "deny"
	CreatedBy string              `json:"created_by,omitempty"`
	CreatedAt time.Time           `json:"created_at,omitempty"`
	UpdatedAt time.Time           `json:"updated_at,omitempty"`
}

// NetworkPolicyStore manages network policy rules for a single scope
// (platform, org, or team).
type NetworkPolicyStore interface {
	// List returns all rules in this scope.
	List(ctx context.Context) ([]NetworkPolicyRule, error)

	// Get returns a single rule by ID, or nil if not found.
	Get(ctx context.Context, id string) (*NetworkPolicyRule, error)

	// Save creates or updates a rule. If rule.ID is empty, a new rule is created.
	// If a rule with the same host:port already exists, it is updated.
	Save(ctx context.Context, rule *NetworkPolicyRule) error

	// Delete removes a rule by ID.
	Delete(ctx context.Context, id string) error
}
