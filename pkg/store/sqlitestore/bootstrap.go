package sqlitestore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/schardosin/astonish/pkg/store"
)

// BootstrapConfig holds the parameters for first-run bootstrap.
type BootstrapConfig struct {
	Email       string
	DisplayName string
	Password    string
	OrgName     string
	OrgSlug     string
	TeamName    string
	TeamSlug    string
}

// Bootstrap creates the first user, organization, and team for a fresh installation.
// It provisions the org and team databases and creates all initial records.
//
// This should only be called when NeedsBootstrap() returns true.
func (s *SQLiteStore) Bootstrap(ctx context.Context, cfg BootstrapConfig) error {
	// Validate inputs
	if cfg.Email == "" || cfg.Password == "" {
		return fmt.Errorf("email and password are required")
	}
	if cfg.OrgSlug == "" {
		return fmt.Errorf("organization slug is required")
	}
	if cfg.TeamSlug == "" {
		cfg.TeamSlug = "default"
	}
	if cfg.TeamName == "" {
		cfg.TeamName = "Default"
	}
	if cfg.OrgName == "" {
		cfg.OrgName = cfg.OrgSlug
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = cfg.Email
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	now := time.Now()
	userID := uuid.New().String()
	orgID := uuid.New().String()

	// Create user
	user := &store.User{
		ID:           userID,
		Email:        cfg.Email,
		DisplayName:  cfg.DisplayName,
		PasswordHash: string(hash),
		PlatformRole: "superadmin",
		Status:       "active",
		CreatedAt:    now,
	}
	if err := s.Users().Create(ctx, user); err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	// Create organization
	org := &store.Organization{
		ID:        orgID,
		Name:      cfg.OrgName,
		Slug:      cfg.OrgSlug,
		CreatedAt: now,
	}
	if err := s.Organizations().Create(ctx, org); err != nil {
		return fmt.Errorf("create organization: %w", err)
	}

	// Create membership (owner role)
	if err := s.Organizations().AddMember(ctx, userID, orgID, "owner"); err != nil {
		return fmt.Errorf("add org membership: %w", err)
	}

	// Provision the org's data directory and database
	if err := s.ProvisionOrg(ctx, orgID, cfg.OrgSlug); err != nil {
		return fmt.Errorf("provision org: %w", err)
	}

	// Get the org data store and provision team + personal schemas
	orgStore, err := s.ForOrg(cfg.OrgSlug)
	if err != nil {
		return fmt.Errorf("open org store: %w", err)
	}

	// Create team in the org database
	team := &store.Team{
		ID:        uuid.New().String(),
		Name:      cfg.TeamName,
		Slug:      cfg.TeamSlug,
		CreatedAt: now,
	}
	if err := orgStore.Teams().CreateTeam(ctx, team); err != nil {
		return fmt.Errorf("create team: %w", err)
	}

	// Provision the team database
	if err := orgStore.ProvisionTeam(ctx, cfg.TeamSlug); err != nil {
		return fmt.Errorf("provision team: %w", err)
	}

	// Provision personal database for the user
	if err := orgStore.ProvisionPersonalSchema(ctx, userID); err != nil {
		return fmt.Errorf("provision personal schema: %w", err)
	}

	return nil
}
