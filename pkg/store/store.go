// Package store defines the abstract storage interfaces for Astonish.
//
// The store layer provides a database-agnostic abstraction over all persistent
// data operations. Two implementations are planned:
//
//   - filestore: wraps the existing file-based storage (for personal/single-user mode)
//   - pgstore:   PostgreSQL-backed (for multi-tenant platform mode, future)
//
// In personal mode, the PlatformStore and TenantRouter are not used; all
// access goes through a single OrgDataStore backed by the local filesystem.
package store

import (
	"context"
	"time"
)

// PlatformStore manages cross-organization data: users, orgs, login sessions.
// Only used in platform (multi-tenant) mode.
type PlatformStore interface {
	Users() UserStore
	Organizations() OrganizationStore
	LoginSessions() LoginSessionStore
	Close() error
}

// TenantRouter routes requests to the correct organization's data store.
// In PostgreSQL mode, this switches to the org-specific database.
// Only used in platform (multi-tenant) mode.
type TenantRouter interface {
	// ForOrg returns a handle to the org's isolated data store.
	ForOrg(orgID string) (OrgDataStore, error)

	// ProvisionOrg creates a new org data store (database/HDI container) and
	// runs all necessary migrations.
	ProvisionOrg(ctx context.Context, orgID, slug string) error

	// DecommissionOrg removes an org's data store.
	DecommissionOrg(ctx context.Context, orgID string) error
}

// OrgDataStore is the root of all data access within an organization.
// In personal mode, this is the only store needed (backed by the local filesystem).
type OrgDataStore interface {
	// ForTeam returns a TeamDataStore scoped to the given team.
	// In personal mode, teamSlug is ignored (there's only one implicit team).
	ForTeam(teamSlug string) TeamDataStore

	// ForUser returns a PersonalDataStore for the given user's private data.
	// In personal mode, userID is ignored (there's only one implicit user).
	ForUser(userID string) PersonalDataStore

	// Org-wide shared stores (public schema in PG, shared directory in file mode).
	OrgMemories() MemoryStore
	OrgSkills() SkillStore
	OrgMCPServers() MCPServerStore
	OrgApps() AppStore
	OrgAudit() AuditStore
	Teams() TeamManagementStore

	// Provisioning operations.
	ProvisionTeam(ctx context.Context, slug string) error
	ProvisionPersonalSchema(ctx context.Context, userID string) error

	Close() error
}

// TeamDataStore accesses a specific team's data.
type TeamDataStore interface {
	Sessions() SessionStore
	Memories() MemoryStore
	Credentials() CredentialStore
	Apps() AppStore
	AppState() AppStateStore
	Flows() FlowStore
	Skills() SkillStore
	MCPServers() MCPServerStore
	ScheduledJobs() SchedulerStore
	FleetTemplates() FleetTemplateStore
	FleetPlans() FleetPlanStore
	DrillReports() DrillReportStore
	Settings() SettingsStore
	Audit() AuditStore
}

// PersonalDataStore accesses a user's private data.
type PersonalDataStore interface {
	Memories() MemoryStore
	Apps() AppStore
	Sessions() SessionStore
	AppState() AppStateStore
	Flows() FlowStore
	Credentials() CredentialStore
}

// --------------------------------------------------------------------------
// Platform-level stores (multi-tenant only)
// --------------------------------------------------------------------------

// User represents a platform user.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	PasswordHash string    `json:"-"`
	OIDCSubject  string    `json:"oidc_subject,omitempty"`
	OIDCIssuer   string    `json:"oidc_issuer,omitempty"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	LastLoginAt  time.Time `json:"last_login_at,omitempty"`
}

// Organization represents a tenant organization.
type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	DBName    string    `json:"db_name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// LoginSession represents an active login session.
type LoginSession struct {
	TokenHash string    `json:"token_hash"`
	UserID    string    `json:"user_id"`
	OrgID     string    `json:"org_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	UserAgent string    `json:"user_agent,omitempty"`
	IPAddress string    `json:"ip_address,omitempty"`
}

// UserStore manages user records.
type UserStore interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByOIDC(ctx context.Context, issuer, subject string) (*User, error)
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id string) error

	// List returns all users, ordered by email.
	List(ctx context.Context) ([]*User, error)

	// ListByOrg returns all users who are members of the given org, with their role.
	ListByOrg(ctx context.Context, orgID string) ([]*UserWithRole, error)
}

// OrganizationStore manages organization records.
type OrganizationStore interface {
	Create(ctx context.Context, org *Organization) error
	GetByID(ctx context.Context, id string) (*Organization, error)
	GetBySlug(ctx context.Context, slug string) (*Organization, error)
	List(ctx context.Context) ([]*Organization, error)
	Update(ctx context.Context, org *Organization) error
	Count(ctx context.Context) (int, error)

	// Org membership management.
	AddMember(ctx context.Context, userID, orgID, role string) error
	RemoveMember(ctx context.Context, userID, orgID string) error
	GetUserOrgs(ctx context.Context, userID string) ([]*OrgMembership, error)
	GetMemberRole(ctx context.Context, userID, orgID string) (string, error)

	// ListMembers returns all users who are members of the given org, with their role.
	ListMembers(ctx context.Context, orgID string) ([]*UserWithRole, error)
}

// OrgMembership represents a user's membership in an organization.
type OrgMembership struct {
	UserID   string    `json:"user_id"`
	OrgID    string    `json:"org_id"`
	OrgSlug  string    `json:"org_slug"`
	OrgName  string    `json:"org_name"`
	Role     string    `json:"role"` // owner, admin, member
	JoinedAt time.Time `json:"joined_at"`
}

// UserWithRole is a User with their role within an organization.
type UserWithRole struct {
	User
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

// LoginSessionStore manages login sessions.
type LoginSessionStore interface {
	Create(ctx context.Context, session *LoginSession) error
	Validate(ctx context.Context, tokenHash string) (*LoginSession, error)
	Delete(ctx context.Context, tokenHash string) error
	DeleteExpired(ctx context.Context) error
}

// --------------------------------------------------------------------------
// Team management store (within an org)
// --------------------------------------------------------------------------

// Team represents a team within an organization.
type Team struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	SchemaName string    `json:"schema_name"`
	CreatedAt  time.Time `json:"created_at"`
}

// TeamMembership represents a user's membership in a team.
type TeamMembership struct {
	UserID      string    `json:"user_id"`
	TeamID      string    `json:"team_id"`
	Role        string    `json:"role"` // admin, member, viewer
	JoinedAt    time.Time `json:"joined_at"`
	Email       string    `json:"email,omitempty"`
	DisplayName string    `json:"display_name,omitempty"`
}

// TeamManagementStore manages teams and memberships within an org.
type TeamManagementStore interface {
	CreateTeam(ctx context.Context, team *Team) error
	GetTeam(ctx context.Context, id string) (*Team, error)
	GetTeamBySlug(ctx context.Context, slug string) (*Team, error)
	ListTeams(ctx context.Context) ([]*Team, error)
	ListTeamsForUser(ctx context.Context, userID string) ([]*Team, error)
	DeleteTeam(ctx context.Context, id string) error

	AddMember(ctx context.Context, membership *TeamMembership) error
	RemoveMember(ctx context.Context, userID, teamID string) error
	SetRole(ctx context.Context, userID, teamID, role string) error
	ListMembers(ctx context.Context, teamID string) ([]*TeamMembership, error)
	GetUserTeams(ctx context.Context, userID string) ([]*TeamMembership, error)
	IsTeamMember(ctx context.Context, userID, teamSlug string) (bool, error)
	GetMemberRole(ctx context.Context, userID, teamID string) (string, error)
}

// --------------------------------------------------------------------------
// Audit store
// --------------------------------------------------------------------------

// AuditEntry represents an immutable audit log entry.
type AuditEntry struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	UserID    string    `json:"user_id"`
	TeamID    string    `json:"team_id,omitempty"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Detail    any       `json:"detail,omitempty"`
	IPAddress string    `json:"ip_address,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
}

// AuditStore provides append-only audit logging.
type AuditStore interface {
	// Log appends an audit entry. This is the only write operation.
	Log(ctx context.Context, entry *AuditEntry) error

	// Query retrieves audit entries matching the filter.
	Query(ctx context.Context, filter AuditFilter) ([]*AuditEntry, error)
}

// AuditFilter specifies criteria for querying audit entries.
type AuditFilter struct {
	UserID    string
	Action    string
	Resource  string
	Since     time.Time
	Until     time.Time
	Limit     int
	Offset    int
}
