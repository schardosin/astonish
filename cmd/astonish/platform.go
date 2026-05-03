package astonish

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

func handlePlatformCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printPlatformUsage()
		return nil
	}

	switch args[0] {
	case "init":
		return handlePlatformInit(args[1:])
	case "status":
		return handlePlatformStatus(args[1:])
	case "org":
		return handlePlatformOrgCommand(args[1:])
	default:
		printPlatformUsage()
		return fmt.Errorf("unknown platform subcommand: %s", args[0])
	}
}

// ---------------------------------------------------------------------------
// platform init
// ---------------------------------------------------------------------------

func handlePlatformInit(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("usage: astonish platform init [--dsn <dsn>]")
		fmt.Println("")
		fmt.Println("Initialize the Astonish platform database.")
		fmt.Println("Creates required roles, the platform database tables, and runs migrations.")
		fmt.Println("")
		fmt.Println("options:")
		fmt.Println("  --dsn <dsn>   PostgreSQL DSN (overrides config.yaml)")
		return nil
	}

	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Allow --dsn override
	dsn := appCfg.Storage.Postgres.PlatformDSN
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--dsn" {
			dsn = args[i+1]
			break
		}
	}
	if dsn == "" {
		return fmt.Errorf("no PostgreSQL DSN configured\n" +
			"Set storage.postgres.platform_dsn in config.yaml or pass --dsn")
	}

	ctx := context.Background()

	fmt.Println("=== Astonish Platform Init ===")
	fmt.Println()

	// Bootstrap: create database, roles, and run migrations.
	fmt.Print("Initializing platform database... ")
	if err := pgstore.BootstrapPlatform(ctx, dsn); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("bootstrap failed: %w", err)
	}
	fmt.Println("OK")

	// Verify connectivity via PGStore
	fmt.Print("Verifying platform store... ")
	pgCfg := appCfg.Storage.Postgres
	pgCfg.PlatformDSN = dsn
	_, pgStore, err := pgstore.NewPlatformServices(ctx, pgCfg)
	if err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("store verification failed: %w", err)
	}
	pgStore.Close()
	fmt.Println("OK")

	fmt.Println()
	fmt.Println("Platform initialized successfully.")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Set storage.backend: postgres in config.yaml")
	fmt.Println("  2. Create an organization:  astonish platform org create --name 'My Org' --slug my-org")
	fmt.Println("  3. Invite users:            astonish platform org invite --org my-org --email user@example.com")
	fmt.Println("  4. Start the daemon:         astonish daemon run")

	return nil
}

// ---------------------------------------------------------------------------
// platform status
// ---------------------------------------------------------------------------

func handlePlatformStatus(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("usage: astonish platform status")
		fmt.Println("")
		fmt.Println("Show platform status including organization, team, and user counts.")
		return nil
	}

	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !appCfg.Storage.IsPostgres() {
		fmt.Println("Mode: personal (file-based)")
		fmt.Println("  Platform features are not enabled.")
		fmt.Println("  Set storage.backend to 'postgres' in config.yaml to enable platform mode.")
		return nil
	}

	ctx := context.Background()

	_, pgStore, err := pgstore.NewPlatformServices(ctx, appCfg.Storage.Postgres)
	if err != nil {
		return fmt.Errorf("failed to connect to platform DB: %w", err)
	}
	defer pgStore.Close()

	fmt.Println("=== Astonish Platform Status ===")
	fmt.Println()
	fmt.Println("Mode: platform (PostgreSQL)")
	fmt.Println()

	// Organization count
	orgCount, err := pgStore.Organizations().Count(ctx)
	if err != nil {
		return fmt.Errorf("failed to count orgs: %w", err)
	}
	fmt.Printf("  Organizations:  %d\n", orgCount)

	// User count (query directly since there's no Count on UserStore)
	pool, err := pgStore.PoolManager().PlatformPool(ctx)
	if err != nil {
		return fmt.Errorf("failed to get platform pool: %w", err)
	}
	var userCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		return fmt.Errorf("failed to count users: %w", err)
	}
	fmt.Printf("  Users:          %d\n", userCount)

	// List organizations with their team counts
	orgs, err := pgStore.Organizations().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list orgs: %w", err)
	}

	if len(orgs) > 0 {
		fmt.Println()
		fmt.Println("  Organizations:")
		for _, org := range orgs {
			teamCount := 0
			orgDS, forOrgErr := pgStore.ForOrg(org.Slug)
			if forOrgErr == nil {
				teams, listErr := orgDS.Teams().ListTeams(ctx)
				if listErr == nil {
					teamCount = len(teams)
				}
			}
			fmt.Printf("    %-20s %-15s %d teams  (status: %s)\n",
				org.Name, org.Slug, teamCount, org.Status)
		}
	}

	// Database connectivity check
	fmt.Println()
	fmt.Print("  Database:       ")
	var pgVersion string
	if err := pool.QueryRow(ctx, `SELECT version()`).Scan(&pgVersion); err != nil {
		fmt.Println("ERROR - " + err.Error())
	} else {
		// Truncate to just the main version line
		if idx := strings.Index(pgVersion, ","); idx > 0 {
			pgVersion = pgVersion[:idx]
		}
		fmt.Println(pgVersion)
	}

	return nil
}

// ---------------------------------------------------------------------------
// platform org <subcommand>
// ---------------------------------------------------------------------------

func handlePlatformOrgCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printPlatformOrgUsage()
		return nil
	}

	switch args[0] {
	case "create":
		return handlePlatformOrgCreate(args[1:])
	case "list", "ls":
		return handlePlatformOrgList(args[1:])
	case "invite":
		return handlePlatformOrgInvite(args[1:])
	default:
		printPlatformOrgUsage()
		return fmt.Errorf("unknown platform org subcommand: %s", args[0])
	}
}

// ---------------------------------------------------------------------------
// platform org create
// ---------------------------------------------------------------------------

func handlePlatformOrgCreate(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("usage: astonish platform org create --name <name> --slug <slug> [--owner-email <email>]")
		fmt.Println("")
		fmt.Println("Create a new organization with its database, default team, and schemas.")
		fmt.Println("")
		fmt.Println("options:")
		fmt.Println("  --name <name>           Organization display name (required)")
		fmt.Println("  --slug <slug>           URL-safe slug (required, lowercase alphanumeric)")
		fmt.Println("  --owner-email <email>   Set an existing user as org owner")
		return nil
	}

	var name, slug, ownerEmail string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			if i+1 < len(args) {
				i++
				name = args[i]
			}
		case "--slug":
			if i+1 < len(args) {
				i++
				slug = args[i]
			}
		case "--owner-email":
			if i+1 < len(args) {
				i++
				ownerEmail = args[i]
			}
		}
	}

	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if slug == "" {
		return fmt.Errorf("--slug is required")
	}

	// Validate slug format
	for _, r := range slug {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return fmt.Errorf("invalid slug: must be lowercase alphanumeric with hyphens/underscores")
		}
	}

	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if !appCfg.Storage.IsPostgres() {
		return fmt.Errorf("platform org commands require storage.backend: postgres")
	}

	ctx := context.Background()

	_, pgStore, err := pgstore.NewPlatformServices(ctx, appCfg.Storage.Postgres)
	if err != nil {
		return fmt.Errorf("failed to connect to platform DB: %w", err)
	}
	defer pgStore.Close()

	fmt.Printf("Creating organization '%s' (slug: %s)...\n", name, slug)
	fmt.Println()

	// Check if slug is already taken
	if existing, _ := pgStore.Organizations().GetBySlug(ctx, slug); existing != nil {
		return fmt.Errorf("organization with slug '%s' already exists", slug)
	}

	// Create the org record
	org := &store.Organization{
		ID:        uuid.New().String(),
		Name:      name,
		Slug:      slug,
		DBName:    pgstore.OrgDBName(slug),
		Status:    "active",
		CreatedAt: time.Now(),
	}

	if err := pgStore.Organizations().Create(ctx, org); err != nil {
		return fmt.Errorf("failed to create org record: %w", err)
	}
	fmt.Printf("  Organization record: %s (id: %s)\n", org.Name, org.ID)

	// Provision the org database
	fmt.Print("  Provisioning org database... ")
	if err := pgStore.ProvisionOrg(ctx, org.ID, slug); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("failed to provision org DB: %w", err)
	}
	fmt.Println("OK")

	// Create the default "general" team
	orgDS, err := pgStore.ForOrg(slug)
	if err != nil {
		return fmt.Errorf("failed to connect to org database: %w", err)
	}

	teamSlug := "general"
	team := &store.Team{
		ID:         uuid.New().String(),
		Name:       "General",
		Slug:       teamSlug,
		SchemaName: pgstore.TeamSchemaName(teamSlug),
		CreatedAt:  time.Now(),
	}

	if err := orgDS.Teams().CreateTeam(ctx, team); err != nil {
		return fmt.Errorf("failed to create default team: %w", err)
	}

	fmt.Print("  Provisioning team schema... ")
	if err := orgDS.ProvisionTeam(ctx, teamSlug); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("failed to provision team schema: %w", err)
	}
	fmt.Println("OK")

	fmt.Printf("  Default team: %s\n", team.Name)

	// If owner email is specified, add them
	if ownerEmail != "" {
		user, userErr := pgStore.Users().GetByEmail(ctx, strings.ToLower(strings.TrimSpace(ownerEmail)))
		if userErr != nil {
			fmt.Printf("  Warning: user '%s' not found, skipping owner assignment\n", ownerEmail)
		} else {
			if err := pgStore.Organizations().AddMember(ctx, user.ID, org.ID, "owner"); err != nil {
				fmt.Printf("  Warning: failed to add owner: %v\n", err)
			} else {
				fmt.Printf("  Owner: %s (%s)\n", user.DisplayName, user.Email)
			}

			// Add to default team and provision personal schema
			if err := orgDS.Teams().AddMember(ctx, &store.TeamMembership{
				UserID:   user.ID,
				TeamID:   team.ID,
				Role:     "admin",
				JoinedAt: time.Now(),
			}); err != nil {
				fmt.Printf("  Warning: failed to add owner to team: %v\n", err)
			}

			if err := orgDS.ProvisionPersonalSchema(ctx, user.ID); err != nil {
				fmt.Printf("  Warning: failed to provision personal schema: %v\n", err)
			}
		}
	}

	fmt.Println()
	fmt.Println("Organization created successfully.")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  Invite users:  astonish platform org invite --org %s --email user@example.com\n", slug)

	return nil
}

// ---------------------------------------------------------------------------
// platform org list
// ---------------------------------------------------------------------------

func handlePlatformOrgList(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("usage: astonish platform org list")
		fmt.Println("")
		fmt.Println("List all organizations.")
		return nil
	}

	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if !appCfg.Storage.IsPostgres() {
		return fmt.Errorf("platform org commands require storage.backend: postgres")
	}

	ctx := context.Background()

	_, pgStore, err := pgstore.NewPlatformServices(ctx, appCfg.Storage.Postgres)
	if err != nil {
		return fmt.Errorf("failed to connect to platform DB: %w", err)
	}
	defer pgStore.Close()

	orgs, err := pgStore.Organizations().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list organizations: %w", err)
	}

	if len(orgs) == 0 {
		fmt.Println("No organizations found.")
		fmt.Println("Create one with: astonish platform org create --name 'My Org' --slug my-org")
		return nil
	}

	fmt.Printf("%-36s  %-20s  %-15s  %-10s  %s\n", "ID", "NAME", "SLUG", "STATUS", "CREATED")
	fmt.Println(strings.Repeat("-", 100))
	for _, org := range orgs {
		fmt.Printf("%-36s  %-20s  %-15s  %-10s  %s\n",
			org.ID, truncateStr(org.Name, 20), org.Slug, org.Status,
			org.CreatedAt.Format("2006-01-02"))
	}

	return nil
}

// ---------------------------------------------------------------------------
// platform org invite
// ---------------------------------------------------------------------------

func handlePlatformOrgInvite(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("usage: astonish platform org invite --org <slug> --email <email> [--role <role>] [--name <name>]")
		fmt.Println("")
		fmt.Println("Invite a user to an organization. If the user doesn't exist, they are created")
		fmt.Println("with a temporary password that must be changed on first login.")
		fmt.Println("")
		fmt.Println("options:")
		fmt.Println("  --org <slug>      Organization slug (required)")
		fmt.Println("  --email <email>   User's email address (required)")
		fmt.Println("  --role <role>     Role in the org: owner, admin, member (default: member)")
		fmt.Println("  --name <name>     Display name for new users (defaults to email prefix)")
		fmt.Println("  --team <slug>     Also add user to this team (default: general)")
		fmt.Println("  --password        Prompt for password instead of generating one")
		return nil
	}

	var orgSlug, email, role, displayName, teamSlug string
	promptPassword := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--org":
			if i+1 < len(args) {
				i++
				orgSlug = args[i]
			}
		case "--email":
			if i+1 < len(args) {
				i++
				email = args[i]
			}
		case "--role":
			if i+1 < len(args) {
				i++
				role = args[i]
			}
		case "--name":
			if i+1 < len(args) {
				i++
				displayName = args[i]
			}
		case "--team":
			if i+1 < len(args) {
				i++
				teamSlug = args[i]
			}
		case "--password":
			promptPassword = true
		}
	}

	if orgSlug == "" {
		return fmt.Errorf("--org is required")
	}
	if email == "" {
		return fmt.Errorf("--email is required")
	}
	email = strings.ToLower(strings.TrimSpace(email))

	if role == "" {
		role = "member"
	}
	if role != "owner" && role != "admin" && role != "member" {
		return fmt.Errorf("invalid role: must be owner, admin, or member")
	}
	if teamSlug == "" {
		teamSlug = "general"
	}

	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if !appCfg.Storage.IsPostgres() {
		return fmt.Errorf("platform org commands require storage.backend: postgres")
	}

	ctx := context.Background()

	_, pgStore, err := pgstore.NewPlatformServices(ctx, appCfg.Storage.Postgres)
	if err != nil {
		return fmt.Errorf("failed to connect to platform DB: %w", err)
	}
	defer pgStore.Close()

	// Look up the organization
	org, err := pgStore.Organizations().GetBySlug(ctx, orgSlug)
	if err != nil {
		return fmt.Errorf("organization '%s' not found: %w", orgSlug, err)
	}

	// Check if user exists or create
	user, err := pgStore.Users().GetByEmail(ctx, email)
	userIsNew := false
	if err != nil {
		// User doesn't exist — create
		userIsNew = true

		if displayName == "" {
			displayName = strings.Split(email, "@")[0]
		}

		var password string
		if promptPassword {
			password, err = promptNewPassword()
			if err != nil {
				return err
			}
		} else {
			password = generateTempPassword()
		}

		hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			return fmt.Errorf("failed to hash password: %w", hashErr)
		}

		user = &store.User{
			ID:           uuid.New().String(),
			Email:        email,
			DisplayName:  displayName,
			PasswordHash: string(hash),
			Status:       "active",
			CreatedAt:    time.Now(),
		}

		if createErr := pgStore.Users().Create(ctx, user); createErr != nil {
			return fmt.Errorf("failed to create user: %w", createErr)
		}

		fmt.Printf("Created new user: %s (%s)\n", user.Email, user.DisplayName)
		if !promptPassword {
			fmt.Printf("  Temporary password: %s\n", password)
			fmt.Println("  (User should change this on first login)")
		}
	} else {
		fmt.Printf("Existing user: %s (%s)\n", user.Email, user.DisplayName)
	}

	// Add user to org
	if err := pgStore.Organizations().AddMember(ctx, user.ID, org.ID, role); err != nil {
		return fmt.Errorf("failed to add user to org: %w", err)
	}
	fmt.Printf("  Added to org '%s' as %s\n", org.Name, role)

	// Connect to org DB and add to team
	orgDS, err := pgStore.ForOrg(orgSlug)
	if err != nil {
		return fmt.Errorf("failed to connect to org database: %w", err)
	}

	// Provision personal schema
	if err := orgDS.ProvisionPersonalSchema(ctx, user.ID); err != nil {
		fmt.Printf("  Warning: failed to provision personal schema: %v\n", err)
	}

	// Look up team
	team, err := orgDS.Teams().GetTeamBySlug(ctx, teamSlug)
	if err != nil {
		if userIsNew {
			fmt.Printf("  Warning: team '%s' not found, skipping team assignment\n", teamSlug)
		}
	} else {
		teamRole := "member"
		if role == "owner" || role == "admin" {
			teamRole = "admin"
		}
		if err := orgDS.Teams().AddMember(ctx, &store.TeamMembership{
			UserID:   user.ID,
			TeamID:   team.ID,
			Role:     teamRole,
			JoinedAt: time.Now(),
		}); err != nil {
			fmt.Printf("  Warning: failed to add to team '%s': %v\n", teamSlug, err)
		} else {
			fmt.Printf("  Added to team '%s' as %s\n", team.Name, teamRole)
		}
	}

	fmt.Println()
	fmt.Printf("User '%s' is now a %s of '%s'.\n", user.Email, role, org.Name)

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// promptNewPassword interactively prompts for a new password with confirmation.
func promptNewPassword() (string, error) {
	for {
		fmt.Print("Password (min 8 chars): ")
		pwd1, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("failed to read password: %w", err)
		}
		if len(pwd1) < 8 {
			fmt.Println("  Password must be at least 8 characters. Try again.")
			continue
		}

		fmt.Print("Confirm password: ")
		pwd2, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("failed to read password: %w", err)
		}

		if string(pwd1) != string(pwd2) {
			fmt.Println("  Passwords do not match. Try again.")
			continue
		}

		return string(pwd1), nil
	}
}

// generateTempPassword creates a temporary password for invited users.
func generateTempPassword() string {
	// Use UUID as a temporary password — it's random enough and always >= 8 chars
	return uuid.New().String()[:16]
}

// truncateStr truncates a string to maxLen, adding "..." if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// ---------------------------------------------------------------------------
// Usage printers
// ---------------------------------------------------------------------------

func printPlatformUsage() {
	fmt.Println("usage: astonish platform <command> [options]")
	fmt.Println("")
	fmt.Println("Manage the Astonish multi-tenant platform.")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  init              Initialize the platform database")
	fmt.Println("  status            Show platform status and counts")
	fmt.Println("  org               Manage organizations")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  astonish platform init")
	fmt.Println("  astonish platform status")
	fmt.Println("  astonish platform org create --name 'Acme' --slug acme")
	fmt.Println("  astonish platform org invite --org acme --email alice@acme.com")
	fmt.Println("  astonish platform org list")
}

func printPlatformOrgUsage() {
	fmt.Println("usage: astonish platform org <command> [options]")
	fmt.Println("")
	fmt.Println("Manage platform organizations.")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  create            Create a new organization")
	fmt.Println("  list              List all organizations")
	fmt.Println("  invite            Invite a user to an organization")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  astonish platform org create --name 'Acme Corp' --slug acme")
	fmt.Println("  astonish platform org list")
	fmt.Println("  astonish platform org invite --org acme --email alice@acme.com --role admin")
}
