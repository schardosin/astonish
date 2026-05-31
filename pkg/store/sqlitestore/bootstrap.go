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
// This function is idempotent: if interrupted mid-way and re-run with the same
// parameters, it will skip steps that have already completed and continue from
// where it left off. This handles the case where setup is interrupted (e.g.,
// user presses Ctrl+C after the user is created but before org/team setup).
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

	now := time.Now()

	// Step 1: Create user (or retrieve existing one by email)
	var userID string
	existingUser, err := s.Users().GetByEmail(ctx, cfg.Email)
	if err != nil {
		return fmt.Errorf("check existing user: %w", err)
	}
	if existingUser != nil {
		userID = existingUser.ID
	} else {
		hash, err := bcrypt.GenerateFromPassword([]byte(cfg.Password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		userID = uuid.New().String()
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
	}

	// Step 2: Create organization (or retrieve existing one by slug)
	var orgID string
	existingOrg, err := s.Organizations().GetBySlug(ctx, cfg.OrgSlug)
	if err != nil {
		return fmt.Errorf("check existing org: %w", err)
	}
	if existingOrg != nil {
		orgID = existingOrg.ID
	} else {
		orgID = uuid.New().String()
		org := &store.Organization{
			ID:        orgID,
			Name:      cfg.OrgName,
			Slug:      cfg.OrgSlug,
			CreatedAt: now,
		}
		if err := s.Organizations().Create(ctx, org); err != nil {
			return fmt.Errorf("create organization: %w", err)
		}
	}

	// Step 3: Create membership (INSERT OR REPLACE — already idempotent)
	if err := s.Organizations().AddMember(ctx, userID, orgID, "owner"); err != nil {
		return fmt.Errorf("add org membership: %w", err)
	}

	// Step 4: Provision org data directory and database (idempotent — uses MkdirAll + migrate)
	if err := s.ProvisionOrg(ctx, orgID, cfg.OrgSlug); err != nil {
		return fmt.Errorf("provision org: %w", err)
	}

	// Step 5: Open org store and create team if needed
	orgStore, err := s.ForOrg(cfg.OrgSlug)
	if err != nil {
		return fmt.Errorf("open org store: %w", err)
	}

	existingTeam, err := orgStore.Teams().GetTeamBySlug(ctx, cfg.TeamSlug)
	if err != nil {
		return fmt.Errorf("check existing team: %w", err)
	}
	if existingTeam == nil {
		team := &store.Team{
			ID:        uuid.New().String(),
			Name:      cfg.TeamName,
			Slug:      cfg.TeamSlug,
			CreatedAt: now,
		}
		if err := orgStore.Teams().CreateTeam(ctx, team); err != nil {
			return fmt.Errorf("create team: %w", err)
		}
	}

	// Step 6: Provision team database (idempotent — uses MkdirAll + migrate)
	if err := orgStore.ProvisionTeam(ctx, cfg.TeamSlug); err != nil {
		return fmt.Errorf("provision team: %w", err)
	}

	// Step 7: Provision personal database (idempotent — uses MkdirAll + migrate)
	if err := orgStore.ProvisionPersonalSchema(ctx, userID); err != nil {
		return fmt.Errorf("provision personal schema: %w", err)
	}

	return nil
}
