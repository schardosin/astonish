package astonish

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"

	"github.com/schardosin/astonish/pkg/client"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/entstore"
	"github.com/schardosin/astonish/pkg/store/pgutil"
)

// redactDSN replaces the password in a PostgreSQL DSN with "***" for safe logging.
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "***"
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "***")
		}
	}
	return u.String()
}

// withPlatformStore is a helper that loads the app config, verifies postgres backend,
// opens a platform store connection, and passes it to the callback. The store is
// automatically closed when the callback returns.
func withPlatformStore(category string, fn func(ctx context.Context, pgCfg config.PostgresConfig, backend store.PlatformBackend) error) error {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if !appCfg.Storage.IsPostgres() {
		return fmt.Errorf("platform %s commands require storage.backend: postgres", category)
	}
	ctx := context.Background()
	_, es, err := entstore.NewPlatformServices(ctx, entstore.Config{
		DSN:            appCfg.Storage.Postgres.PlatformDSN,
		InstanceSuffix: appCfg.Storage.Postgres.InstanceSuffix,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to platform DB: %w", err)
	}
	defer es.Close()
	return fn(ctx, appCfg.Storage.Postgres, es)
}

func handlePlatformCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printPlatformUsage()
		return nil
	}

	switch args[0] {
	case "init":
		return handlePlatformInit(args[1:])
	case "gen-secret":
		return handlePlatformGenSecret()
	case "sandbox-audit":
		return handlePlatformSandboxAudit(args[1:])
	case "status":
		return handlePlatformStatus(args[1:])
	case "org":
		return handlePlatformOrgCommand(args[1:])
	case "user":
		return handlePlatformUserCommand(args[1:])
	case "issue-token":
		return handlePlatformIssueToken(args[1:])
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
		printPlatformInitUsage()
		return nil
	}

	// Parse flags.
	var pgHost, pgUser, pgPassword, pgSSLMode, suffix string
	pgPort := 0

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--host":
			if i+1 < len(args) {
				pgHost = args[i+1]
				i++
			}
		case "--port":
			if i+1 < len(args) {
				if _, err := fmt.Sscanf(args[i+1], "%d", &pgPort); err != nil {
					return fmt.Errorf("invalid port: %s", args[i+1])
				}
				i++
			}
		case "--user":
			if i+1 < len(args) {
				pgUser = args[i+1]
				i++
			}
		case "--password":
			if i+1 < len(args) {
				pgPassword = args[i+1]
				i++
			}
		case "--sslmode":
			if i+1 < len(args) {
				pgSSLMode = args[i+1]
				i++
			}
		case "--suffix":
			if i+1 < len(args) {
				suffix = args[i+1]
				i++
			}
		default:
			return fmt.Errorf("unknown flag: %s\nRun 'astonish platform init --help' for usage", args[i])
		}
	}

	// Env var fallbacks.
	if pgHost == "" {
		pgHost = os.Getenv("PGHOST")
	}
	if pgPort == 0 {
		if envPort := os.Getenv("PGPORT"); envPort != "" {
			if _, err := fmt.Sscanf(envPort, "%d", &pgPort); err != nil {
				return fmt.Errorf("invalid PGPORT env: %s", envPort)
			}
		}
	}
	if pgUser == "" {
		pgUser = os.Getenv("PGUSER")
	}
	if pgPassword == "" {
		pgPassword = os.Getenv("PGPASSWORD")
	}
	if pgSSLMode == "" {
		pgSSLMode = os.Getenv("PGSSLMODE")
	}

	// Defaults.
	if pgHost == "" {
		return fmt.Errorf("PostgreSQL host is required (--host or PGHOST)")
	}
	if pgPort == 0 {
		pgPort = 5432
	}
	if pgUser == "" {
		pgUser = "postgres"
	}
	if pgPassword == "" {
		return fmt.Errorf("PostgreSQL password is required (--password or PGPASSWORD)")
	}
	if pgSSLMode == "" {
		pgSSLMode = "prefer"
	}

	ctx := context.Background()

	fmt.Println("=== Astonish Platform Init ===")
	fmt.Println()

	// Connect to the admin database to check/generate suffix.
	// We open a single connection and reuse it for all existence checks
	// to avoid issues with kubectl port-forward dropping connections.
	fmt.Printf("Connecting to PostgreSQL at %s:%d... ", pgHost, pgPort)
	tempDSN := pgutil.BuildDSN(pgHost, pgPort, pgUser, pgPassword, "postgres", pgSSLMode)
	adminConn, err := pgx.Connect(ctx, tempDSN)
	if err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("cannot connect to PostgreSQL: %w", err)
	}
	defer adminConn.Close(ctx)
	fmt.Println("OK")

	// Determine suffix.
	var dbAlreadyExists bool
	if suffix != "" {
		// User-provided suffix: check if database already exists.
		// If it exists, we run migrations (upgrade path).
		// If it doesn't exist, we create it (fresh install with fixed suffix).
		fmt.Printf("Checking database %s... ", config.PlatformDBName(suffix))
		exists, err := pgutil.PlatformDBExistsConn(ctx, adminConn, suffix)
		if err != nil {
			fmt.Println("FAILED")
			return fmt.Errorf("failed to check database existence: %w", err)
		}
		if exists {
			fmt.Println("exists (will run migrations)")
			dbAlreadyExists = true
		} else {
			fmt.Println("new (will create)")
		}
	} else {
		// Auto-generate suffix with collision avoidance.
		fmt.Print("Generating instance suffix... ")
		suffix = config.GenerateInstanceSuffix()
		for attempts := 0; attempts < 5; attempts++ {
			exists, err := pgutil.PlatformDBExistsConn(ctx, adminConn, suffix)
			if err != nil {
				fmt.Println("FAILED")
				return fmt.Errorf("failed to check database existence: %w", err)
			}
			if !exists {
				break
			}
			suffix = config.GenerateInstanceSuffix()
		}
		fmt.Println(suffix)
	}

	// Build the platform DSN with the actual platform DB name.
	platformDBName := config.PlatformDBName(suffix)
	platformDSN := pgutil.BuildDSN(pgHost, pgPort, pgUser, pgPassword, platformDBName, pgSSLMode)

	// Bootstrap: create database (if needed), ensure roles, and run migrations.
	if dbAlreadyExists {
		fmt.Printf("Running migrations on %s... ", platformDBName)
	} else {
		fmt.Printf("Creating database %s... ", platformDBName)
	}
	if err := entstore.BootstrapPlatform(ctx, entstore.Config{
		DSN:            platformDSN,
		InstanceSuffix: suffix,
	}); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("bootstrap failed: %w", err)
	}
	fmt.Println("OK")

	// Verify connectivity via entstore.
	fmt.Print("Verifying platform store... ")
	_, es, err := entstore.NewPlatformServices(ctx, entstore.Config{
		DSN:            platformDSN,
		InstanceSuffix: suffix,
	})
	if err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("store verification failed: %w", err)
	}
	es.Close()
	fmt.Println("OK")

	fmt.Println()
	if dbAlreadyExists {
		fmt.Println("Platform migrations applied successfully.")
	} else {
		fmt.Println("Platform initialized successfully.")
	}
	fmt.Println()
	fmt.Printf("  Database: %s\n", platformDBName)
	fmt.Printf("  Server:   %s:%d\n", pgHost, pgPort)
	fmt.Printf("  Suffix:   %s\n", suffix)
	fmt.Println()
	fmt.Println("Add to your Helm values file:")
	fmt.Println()
	fmt.Println("  secrets:")
	fmt.Printf("    platformDSN: %q\n", redactDSN(platformDSN))
	fmt.Println("  config:")
	fmt.Println("    storage:")
	fmt.Println("      postgres:")
	fmt.Printf("        instanceSuffix: %q\n", suffix)
	fmt.Println()
	fmt.Println("For single-binary mode, add to ~/.config/astonish/config.yaml:")
	fmt.Println()
	fmt.Println("  storage:")
	fmt.Println("    backend: postgres")
	fmt.Println("    postgres:")
	fmt.Printf("      platform_dsn: %q\n", redactDSN(platformDSN))
	fmt.Printf("      instance_suffix: %q\n", suffix)
	fmt.Println()
	if !dbAlreadyExists {
		fmt.Println("Next steps:")
		fmt.Println("  1. Create an organization:  astonish platform org create --name 'My Org' --slug my-org")
		fmt.Println("  2. Invite users:            astonish platform org invite --org my-org --email user@example.com")
	}

	return nil
}

func printPlatformInitUsage() {
	fmt.Println("usage: astonish platform init --host <host> --password <pass> [options]")
	fmt.Println("")
	fmt.Println("Initialize a new Astonish platform database.")
	fmt.Println("Creates the database, required roles, and runs all platform migrations.")
	fmt.Println("")
	fmt.Println("required:")
	fmt.Println("  --host <host>         PostgreSQL server hostname (or PGHOST env)")
	fmt.Println("  --password <pass>     PostgreSQL admin password (or PGPASSWORD env)")
	fmt.Println("")
	fmt.Println("optional:")
	fmt.Println("  --port <port>         PostgreSQL port (default: 5432, or PGPORT env)")
	fmt.Println("  --user <user>         PostgreSQL admin user (default: postgres, or PGUSER env)")
	fmt.Println("  --sslmode <mode>      SSL mode (default: prefer, or PGSSLMODE env)")
	fmt.Println("  --suffix <suffix>     Fixed instance suffix (default: auto-generated)")
	fmt.Println("")
	fmt.Println("The command generates a random 6-character suffix (unless --suffix is given),")
	fmt.Println("creates database 'astonish_<suffix>_platform', and prints the configuration")
	fmt.Println("values to add to your Helm values file or config.yaml.")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  astonish platform init --host 10.0.0.5 --password secret")
	fmt.Println("  astonish platform init --host pg.internal --user admin --password s3cr3t --suffix prod1")
	fmt.Println("")
	fmt.Println("  # Using environment variables:")
	fmt.Println("  PGHOST=10.0.0.5 PGPASSWORD=secret astonish platform init")
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

	_, es, err := entstore.NewPlatformServices(ctx, entstore.Config{
		DSN:            appCfg.Storage.Postgres.PlatformDSN,
		InstanceSuffix: appCfg.Storage.Postgres.InstanceSuffix,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to platform DB: %w", err)
	}
	defer es.Close()

	fmt.Println("=== Astonish Platform Status ===")
	fmt.Println()
	fmt.Println("Mode: platform (PostgreSQL)")
	fmt.Println()

	// Organization count
	orgCount, err := es.Organizations().Count(ctx)
	if err != nil {
		return fmt.Errorf("failed to count orgs: %w", err)
	}
	fmt.Printf("  Organizations:  %d\n", orgCount)

	// User count via raw SQL on the platform DB
	db := es.PlatformDB()
	var userCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		return fmt.Errorf("failed to count users: %w", err)
	}
	fmt.Printf("  Users:          %d\n", userCount)

	// List organizations with their team counts
	orgs, err := es.Organizations().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list orgs: %w", err)
	}

	if len(orgs) > 0 {
		fmt.Println()
		fmt.Println("  Organizations:")
		for _, org := range orgs {
			teamCount := 0
			orgDS, forOrgErr := es.ForOrg(org.Slug)
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
	if err := db.QueryRowContext(ctx, `SELECT version()`).Scan(&pgVersion); err != nil {
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

	return withPlatformStore("org", func(ctx context.Context, pgCfg config.PostgresConfig, backend store.PlatformBackend) error {
		fmt.Printf("Creating organization '%s' (slug: %s)...\n", name, slug)
		fmt.Println()

		// Check if slug is already taken
		if existing, _ := backend.Organizations().GetBySlug(ctx, slug); existing != nil {
			return fmt.Errorf("organization with slug '%s' already exists", slug)
		}

		// Create the org record
		org := &store.Organization{
			ID:        uuid.New().String(),
			Name:      name,
			Slug:      slug,
			DBName:    entstore.OrgDBName(pgCfg.InstanceSuffix, slug),
			Status:    "active",
			CreatedAt: time.Now(),
		}

		if err := backend.Organizations().Create(ctx, org); err != nil {
			return fmt.Errorf("failed to create org record: %w", err)
		}
		fmt.Printf("  Organization record: %s (id: %s)\n", org.Name, org.ID)

		// Provision the org database
		fmt.Print("  Provisioning org database... ")
		if err := backend.ProvisionOrg(ctx, org.ID, slug); err != nil {
			fmt.Println("FAILED")
			return fmt.Errorf("failed to provision org DB: %w", err)
		}
		fmt.Println("OK")

		// Create the default "general" team
		orgDS, err := backend.ForOrg(slug)
		if err != nil {
			return fmt.Errorf("failed to connect to org database: %w", err)
		}

		teamSlug := "general"
		team := &store.Team{
			ID:         uuid.New().String(),
			Name:       "General",
			Slug:       teamSlug,
			SchemaName: entstore.TeamSchemaName(teamSlug),
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
			user, userErr := backend.Users().GetByEmail(ctx, strings.ToLower(strings.TrimSpace(ownerEmail)))
			if userErr != nil {
				fmt.Printf("  Warning: user '%s' not found, skipping owner assignment\n", ownerEmail)
			} else {
				if err := backend.Organizations().AddMember(ctx, user.ID, org.ID, "owner"); err != nil {
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
	})
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

	return withPlatformStore("org", func(ctx context.Context, _ config.PostgresConfig, backend store.PlatformBackend) error {
		orgs, err := backend.Organizations().List(ctx)
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
	})
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

	return withPlatformStore("org", func(ctx context.Context, _ config.PostgresConfig, backend store.PlatformBackend) error {
		// Look up the organization
		org, err := backend.Organizations().GetBySlug(ctx, orgSlug)
		if err != nil {
			return fmt.Errorf("organization '%s' not found: %w", orgSlug, err)
		}

		// Check if user exists or create
		user, err := backend.Users().GetByEmail(ctx, email)
		userIsNew := false
		if err != nil || user == nil {
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

			if createErr := backend.Users().Create(ctx, user); createErr != nil {
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
		if err := backend.Organizations().AddMember(ctx, user.ID, org.ID, role); err != nil {
			return fmt.Errorf("failed to add user to org: %w", err)
		}
		fmt.Printf("  Added to org '%s' as %s\n", org.Name, role)

		// Connect to org DB and add to team
		orgDS, err := backend.ForOrg(orgSlug)
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
	})
}

// ---------------------------------------------------------------------------
// platform user <subcommand>
// ---------------------------------------------------------------------------

func handlePlatformUserCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printPlatformUserUsage()
		return nil
	}

	switch args[0] {
	case "list", "ls":
		return handlePlatformUserList(args[1:])
	case "show":
		return handlePlatformUserShow(args[1:])
	case "delete":
		return handlePlatformUserDelete(args[1:])
	case "set-password":
		return handlePlatformUserSetPassword(args[1:])
	case "disable":
		return handlePlatformUserSetStatus(args[1:], "disabled")
	case "enable":
		return handlePlatformUserSetStatus(args[1:], "active")
	case "promote":
		return handlePlatformUserPromote(args[1:])
	case "demote":
		return handlePlatformUserDemote(args[1:])
	default:
		printPlatformUserUsage()
		return fmt.Errorf("unknown platform user subcommand: %s", args[0])
	}
}

// ---------------------------------------------------------------------------
// platform user list
// ---------------------------------------------------------------------------

func handlePlatformUserList(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("usage: astonish platform user list [--org <slug>]")
		fmt.Println("")
		fmt.Println("List all users, optionally filtered by organization.")
		fmt.Println("")
		fmt.Println("options:")
		fmt.Println("  --org <slug>   Filter by organization slug")
		return nil
	}

	var orgSlug string
	for i := 0; i < len(args); i++ {
		if args[i] == "--org" && i+1 < len(args) {
			i++
			orgSlug = args[i]
		}
	}

	return withPlatformStore("user", func(ctx context.Context, _ config.PostgresConfig, backend store.PlatformBackend) error {
		if orgSlug != "" {
			// List users for a specific org.
			org, orgErr := backend.Organizations().GetBySlug(ctx, orgSlug)
			if orgErr != nil {
				return fmt.Errorf("organization '%s' not found: %w", orgSlug, orgErr)
			}

			members, listErr := backend.Organizations().ListMembers(ctx, org.ID)
			if listErr != nil {
				return fmt.Errorf("failed to list org members: %w", listErr)
			}

			if len(members) == 0 {
				fmt.Printf("No users found in organization '%s'.\n", orgSlug)
				return nil
			}

			fmt.Printf("Users in organization '%s':\n\n", org.Name)
			fmt.Printf("%-36s  %-30s  %-25s  %-10s  %-10s  %s\n", "ID", "EMAIL", "NAME", "ROLE", "STATUS", "CREATED")
			fmt.Println(strings.Repeat("-", 150))
			for _, m := range members {
				fmt.Printf("%-36s  %-30s  %-25s  %-10s  %-10s  %s\n",
					m.ID, truncateStr(m.Email, 30), truncateStr(m.DisplayName, 25),
					m.Role, m.Status, m.CreatedAt.Format("2006-01-02"))
			}
			return nil
		}

		// List all users.
		users, err := backend.Users().List(ctx)
		if err != nil {
			return fmt.Errorf("failed to list users: %w", err)
		}

		if len(users) == 0 {
			fmt.Println("No users found.")
			fmt.Println("Invite users with: astonish platform org invite --org <slug> --email <email>")
			return nil
		}

		fmt.Printf("%-36s  %-30s  %-25s  %-12s  %-10s  %s\n", "ID", "EMAIL", "NAME", "PLATFORM", "STATUS", "CREATED")
		fmt.Println(strings.Repeat("-", 155))
		for _, u := range users {
			prole := ""
			if u.PlatformRole != "" {
				prole = u.PlatformRole
			}
			fmt.Printf("%-36s  %-30s  %-25s  %-12s  %-10s  %s\n",
				u.ID, truncateStr(u.Email, 30), truncateStr(u.DisplayName, 25),
				prole, u.Status, u.CreatedAt.Format("2006-01-02"))
		}

		return nil
	})
}

// ---------------------------------------------------------------------------
// platform user show
// ---------------------------------------------------------------------------

func handlePlatformUserShow(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("usage: astonish platform user show <email>")
		fmt.Println("")
		fmt.Println("Show details for a user by email address.")
		return nil
	}

	email := strings.ToLower(strings.TrimSpace(args[0]))

	return withPlatformStore("user", func(ctx context.Context, _ config.PostgresConfig, backend store.PlatformBackend) error {
		user, err := backend.Users().GetByEmail(ctx, email)
		if err != nil {
			return fmt.Errorf("user '%s' not found: %w", email, err)
		}

		fmt.Printf("User Details:\n\n")
		fmt.Printf("  ID:           %s\n", user.ID)
		fmt.Printf("  Email:        %s\n", user.Email)
		fmt.Printf("  Display Name: %s\n", user.DisplayName)
		fmt.Printf("  Status:       %s\n", user.Status)
		fmt.Printf("  Created:      %s\n", user.CreatedAt.Format("2006-01-02 15:04:05"))
		if !user.LastLoginAt.IsZero() {
			fmt.Printf("  Last Login:   %s\n", user.LastLoginAt.Format("2006-01-02 15:04:05"))
		}
		if user.OIDCIssuer != "" {
			fmt.Printf("  OIDC Issuer:  %s\n", user.OIDCIssuer)
			fmt.Printf("  OIDC Subject: %s\n", user.OIDCSubject)
		}

		// Show org memberships.
		orgs, err := backend.Organizations().GetUserOrgs(ctx, user.ID)
		if err != nil {
			fmt.Printf("\n  (failed to load org memberships: %v)\n", err)
			return nil
		}

		if len(orgs) > 0 {
			fmt.Printf("\n  Organizations:\n")
			for _, om := range orgs {
				fmt.Printf("    - %s (%s) — role: %s, joined: %s\n",
					om.OrgName, om.OrgSlug, om.Role, om.JoinedAt.Format("2006-01-02"))
			}
		} else {
			fmt.Printf("\n  Not a member of any organization.\n")
		}

		return nil
	})
}

// ---------------------------------------------------------------------------
// platform user delete
// ---------------------------------------------------------------------------

func handlePlatformUserDelete(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("usage: astonish platform user delete <email>")
		fmt.Println("")
		fmt.Println("Delete a user by email address.")
		fmt.Println("This removes the user from all organizations and teams.")
		return nil
	}

	email := strings.ToLower(strings.TrimSpace(args[0]))

	return withPlatformStore("user", func(ctx context.Context, _ config.PostgresConfig, backend store.PlatformBackend) error {
		user, err := backend.Users().GetByEmail(ctx, email)
		if err != nil {
			return fmt.Errorf("user '%s' not found: %w", email, err)
		}

		if err := backend.Users().Delete(ctx, user.ID); err != nil {
			return fmt.Errorf("failed to delete user: %w", err)
		}

		fmt.Printf("Deleted user: %s (%s)\n", user.Email, user.DisplayName)
		return nil
	})
}

// ---------------------------------------------------------------------------
// platform user set-password
// ---------------------------------------------------------------------------

func handlePlatformUserSetPassword(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("usage: astonish platform user set-password <email>")
		fmt.Println("")
		fmt.Println("Interactively set a new password for a user.")
		return nil
	}

	email := strings.ToLower(strings.TrimSpace(args[0]))

	return withPlatformStore("user", func(ctx context.Context, _ config.PostgresConfig, backend store.PlatformBackend) error {
		user, err := backend.Users().GetByEmail(ctx, email)
		if err != nil {
			return fmt.Errorf("user '%s' not found: %w", email, err)
		}

		password, err := promptNewPassword()
		if err != nil {
			return err
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("failed to hash password: %w", err)
		}

		user.PasswordHash = string(hash)
		if err := backend.Users().Update(ctx, user); err != nil {
			return fmt.Errorf("failed to update password: %w", err)
		}

		fmt.Printf("Password updated for user: %s\n", user.Email)
		return nil
	})
}

// ---------------------------------------------------------------------------
// platform user disable / enable
// ---------------------------------------------------------------------------

func handlePlatformUserSetStatus(args []string, status string) error {
	verb := "disable"
	if status == "active" {
		verb = "enable"
	}

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Printf("usage: astonish platform user %s <email>\n", verb)
		fmt.Println("")
		fmt.Printf("%s a user account.\n", strings.ToUpper(verb[:1])+verb[1:])
		return nil
	}

	email := strings.ToLower(strings.TrimSpace(args[0]))

	return withPlatformStore("user", func(ctx context.Context, _ config.PostgresConfig, backend store.PlatformBackend) error {
		user, err := backend.Users().GetByEmail(ctx, email)
		if err != nil {
			return fmt.Errorf("user '%s' not found: %w", email, err)
		}

		user.Status = status
		if err := backend.Users().Update(ctx, user); err != nil {
			return fmt.Errorf("failed to %s user: %w", verb, err)
		}

		fmt.Printf("User %sd: %s (%s)\n", verb, user.Email, user.DisplayName)
		return nil
	})
}

// ---------------------------------------------------------------------------
// platform user promote
// ---------------------------------------------------------------------------

func handlePlatformUserPromote(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("usage: astonish platform user promote <email>")
		fmt.Println("")
		fmt.Println("Promote a user to platform superadmin.")
		fmt.Println("Superadmins have full access to manage all organizations and users.")
		return nil
	}

	email := strings.ToLower(strings.TrimSpace(args[0]))

	return withPlatformStore("user", func(ctx context.Context, _ config.PostgresConfig, backend store.PlatformBackend) error {
		user, err := backend.Users().GetByEmail(ctx, email)
		if err != nil {
			return fmt.Errorf("user '%s' not found: %w", email, err)
		}

		if user.PlatformRole == "superadmin" {
			fmt.Printf("User '%s' is already a platform superadmin.\n", email)
			return nil
		}

		if err := backend.Users().SetPlatformRole(ctx, user.ID, "superadmin"); err != nil {
			return fmt.Errorf("failed to promote user: %w", err)
		}

		fmt.Printf("Promoted %s (%s) to platform superadmin.\n", user.Email, user.DisplayName)
		return nil
	})
}

// ---------------------------------------------------------------------------
// platform user demote
// ---------------------------------------------------------------------------

func handlePlatformUserDemote(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("usage: astonish platform user demote <email>")
		fmt.Println("")
		fmt.Println("Demote a platform superadmin to regular user.")
		fmt.Println("Cannot demote the last superadmin.")
		return nil
	}

	email := strings.ToLower(strings.TrimSpace(args[0]))

	return withPlatformStore("user", func(ctx context.Context, _ config.PostgresConfig, backend store.PlatformBackend) error {
		user, err := backend.Users().GetByEmail(ctx, email)
		if err != nil {
			return fmt.Errorf("user '%s' not found: %w", email, err)
		}

		if user.PlatformRole != "superadmin" {
			fmt.Printf("User '%s' is not a platform superadmin.\n", email)
			return nil
		}

		// Safety: prevent demoting the last superadmin
		count, countErr := backend.Users().CountByPlatformRole(ctx, "superadmin")
		if countErr != nil {
			return fmt.Errorf("failed to count superadmins: %w", countErr)
		}
		if count <= 1 {
			return fmt.Errorf("cannot demote the last platform superadmin — promote another user first")
		}

		if err := backend.Users().SetPlatformRole(ctx, user.ID, ""); err != nil {
			return fmt.Errorf("failed to demote user: %w", err)
		}

		fmt.Printf("Demoted %s (%s) from platform superadmin.\n", user.Email, user.DisplayName)
		return nil
	})
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
// ---------------------------------------------------------------------------
// platform issue-token
// ---------------------------------------------------------------------------

// handlePlatformIssueToken obtains an access token via browser-based authentication
// and prints it to stdout. No credentials are persisted locally.
// Supports both SSO (device-code flow) and password authentication.
func handlePlatformIssueToken(args []string) error {
	var serverURL, email, password, org, team string
	useSSO := false
	outputJSON := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			printPlatformIssueTokenUsage()
			return nil
		case "--server":
			if i+1 < len(args) {
				serverURL = args[i+1]
				i++
			}
		case "--sso":
			useSSO = true
		case "--email":
			if i+1 < len(args) {
				email = args[i+1]
				i++
			}
		case "--password":
			if i+1 < len(args) {
				password = args[i+1]
				i++
			}
		case "--org":
			if i+1 < len(args) {
				org = args[i+1]
				i++
			}
		case "--team":
			if i+1 < len(args) {
				team = args[i+1]
				i++
			}
		case "--json":
			outputJSON = true
		}
	}

	// Resolve server URL: explicit flag > remote config > error
	if serverURL == "" {
		cfg, _ := client.LoadRemoteConfig()
		if cfg != nil && cfg.URL != "" {
			serverURL = cfg.URL
			if org == "" {
				org = cfg.Org
			}
			if team == "" {
				team = cfg.Team
			}
		}
	}
	if serverURL == "" {
		return fmt.Errorf("--server is required (no remote config found)")
	}
	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		serverURL = "https://" + serverURL
	}
	serverURL = strings.TrimRight(serverURL, "/")

	// Auto-detect mode: if SSO providers exist and no explicit email, use SSO.
	if !useSSO && email == "" {
		providers, _ := client.ListSSOProviders(serverURL)
		if len(providers) > 0 {
			useSSO = true
		}
	}

	var result *client.TokenResult
	var err error

	if useSSO {
		result, err = client.IssueTokenSSO(serverURL, "", func(status string) {
			switch status {
			case "opening_browser":
				fmt.Fprintln(os.Stderr, "Opening browser for authentication...")
			case "browser_failed":
				fmt.Fprintln(os.Stderr, "Could not open browser. Please open the URL printed in your terminal.")
			case "polling":
				fmt.Fprintln(os.Stderr, "Waiting for authentication (complete login in browser)...")
			}
		})
	} else {
		// Password mode — prompt if not provided
		if email == "" {
			fmt.Fprint(os.Stderr, "Email: ")
			reader := bufio.NewReader(os.Stdin)
			email, _ = reader.ReadString('\n')
			email = strings.TrimSpace(email)
		}
		if password == "" {
			fmt.Fprint(os.Stderr, "Password: ")
			passwordBytes, passErr := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr) // newline
			if passErr != nil {
				return fmt.Errorf("failed to read password: %w", passErr)
			}
			password = string(passwordBytes)
		}
		if email == "" || password == "" {
			return fmt.Errorf("email and password are required")
		}
		result, err = client.IssueTokenPassword(serverURL, email, password, org, team)
	}

	if err != nil {
		return err
	}

	if outputJSON {
		out := map[string]any{
			"access_token": result.AccessToken,
			"expires_in":   result.ExpiresIn,
			"user_email":   result.UserEmail,
			"org_slug":     result.OrgSlug,
			"team_slug":    result.TeamSlug,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	// Default: print bare token to stdout (pipe-friendly)
	fmt.Print(result.AccessToken)
	return nil
}

func printPlatformIssueTokenUsage() {
	fmt.Println("usage: astonish platform issue-token [options]")
	fmt.Println("")
	fmt.Println("Obtain an access token via browser-based authentication.")
	fmt.Println("The token is printed to stdout; status messages go to stderr.")
	fmt.Println("No credentials are stored locally.")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  --server <url>     Server URL (uses remote config if omitted)")
	fmt.Println("  --sso              Force SSO device-code flow")
	fmt.Println("  --email <email>    Email for password auth (prompts if omitted)")
	fmt.Println("  --password <pass>  Password (prompts interactively if omitted)")
	fmt.Println("  --org <slug>       Organization scope")
	fmt.Println("  --team <slug>      Team scope")
	fmt.Println("  --json             Output full JSON (token + metadata)")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  # SSO (auto-detected if server has SSO providers):")
	fmt.Println("  astonish platform issue-token --server https://astonish.internal")
	fmt.Println("")
	fmt.Println("  # Password auth:")
	fmt.Println("  astonish platform issue-token --server https://astonish.internal --email admin@co.com")
	fmt.Println("")
	fmt.Println("  # Use in scripts:")
	fmt.Println("  export ASTONISH_TEST_TOKEN=$(astonish platform issue-token --server $URL)")
}

// ---------------------------------------------------------------------------
// Usage printers
// ---------------------------------------------------------------------------

// handlePlatformGenSecret generates a cryptographically secure secret suitable
// for masterKey or jwtSecret. Prints a single 64-character hex string (32 bytes).
func handlePlatformGenSecret() error {
	fmt.Println(config.GenerateJWTSecret())
	return nil
}

func printPlatformUsage() {
	fmt.Println("usage: astonish platform <command> [options]")
	fmt.Println("")
	fmt.Println("Manage the Astonish multi-tenant platform.")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  init              Initialize the platform database")
	fmt.Println("  gen-secret        Generate a random secret (for masterKey or jwtSecret)")
	fmt.Println("  issue-token       Obtain an access token via browser auth (no local persistence)")
	fmt.Println("  sandbox-audit     Audit sandbox PVCs for orphaned data")
	fmt.Println("  status            Show platform status and counts")
	fmt.Println("  org               Manage organizations")
	fmt.Println("  user              Manage users")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  astonish platform init --host 10.0.0.5 --password secret")
	fmt.Println("  astonish platform status")
	fmt.Println("  astonish platform issue-token --server https://astonish.internal")
	fmt.Println("  astonish platform org create --name 'Acme' --slug acme")
	fmt.Println("  astonish platform org invite --org acme --email alice@acme.com")
	fmt.Println("  astonish platform org list")
	fmt.Println("  astonish platform user list")
	fmt.Println("  astonish platform user show alice@acme.com")
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

func printPlatformUserUsage() {
	fmt.Println("usage: astonish platform user <command> [options]")
	fmt.Println("")
	fmt.Println("Manage platform users.")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  list              List all users (optionally by org)")
	fmt.Println("  show              Show user details")
	fmt.Println("  delete            Delete a user")
	fmt.Println("  set-password      Set a user's password")
	fmt.Println("  disable           Disable a user account")
	fmt.Println("  enable            Enable a disabled user account")
	fmt.Println("  promote           Promote a user to platform superadmin")
	fmt.Println("  demote            Demote a platform superadmin to regular user")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  astonish platform user list")
	fmt.Println("  astonish platform user list --org acme")
	fmt.Println("  astonish platform user show alice@acme.com")
	fmt.Println("  astonish platform user set-password alice@acme.com")
	fmt.Println("  astonish platform user disable alice@acme.com")
	fmt.Println("  astonish platform user enable alice@acme.com")
	fmt.Println("  astonish platform user delete alice@acme.com")
	fmt.Println("  astonish platform user promote alice@acme.com")
	fmt.Println("  astonish platform user demote alice@acme.com")
}
