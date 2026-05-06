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
	"github.com/schardosin/astonish/pkg/migration"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

func handleMigrateCommand(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		printMigrateUsage()
		return nil
	}

	// Subcommands
	if len(args) > 0 {
		switch args[0] {
		case "status":
			return handleMigrateStatus()
		default:
			fmt.Printf("Unknown migrate subcommand: %s\n", args[0])
			printMigrateUsage()
			return fmt.Errorf("unknown subcommand: %s", args[0])
		}
	}

	// Default: run the migration
	return handleMigrateRun()
}

func handleMigrateRun() error {
	// Load app config
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if appCfg.Storage.Backend != "postgres" {
		return fmt.Errorf("migration is only available in platform mode (storage.backend: postgres)\n" +
			"Set storage.backend to 'postgres' in your config and configure the database connection")
	}

	configDir, err := config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	// Check if migration is already done
	if migration.IsMigrationComplete(configDir) {
		fmt.Println("Migration has already been completed.")
		fmt.Println("Marker file: " + configDir + "/.migration-complete")
		return nil
	}

	// Check if file data exists
	if !migration.HasFileData(configDir) {
		fmt.Println("No file-based data found to migrate.")
		return nil
	}

	fmt.Println("=== Astonish: File → Database Migration ===")
	fmt.Println()
	fmt.Println("This will migrate your personal data from the filesystem to PostgreSQL.")
	fmt.Println("After migration, the original files will be renamed with a backup suffix.")
	fmt.Println()

	// Connect to PostgreSQL
	fmt.Print("Connecting to PostgreSQL... ")
	svc, pgStore, pgErr := pgstore.NewPlatformServices(context.Background(), appCfg.Storage.Postgres)
	if pgErr != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("failed to connect to PostgreSQL: %w", pgErr)
	}
	defer pgStore.Close()
	_ = svc // used by NewPlatformServices but not needed directly here
	fmt.Println("OK")
	fmt.Println()

	// Collect user credentials
	email, displayName, password, err := promptCredentials(appCfg)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Create user
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	user := &store.User{
		ID:           uuid.New().String(),
		Email:        strings.ToLower(strings.TrimSpace(email)),
		DisplayName:  strings.TrimSpace(displayName),
		PasswordHash: string(hash),
		Status:       "active",
		CreatedAt:    time.Now(),
	}

	if err := pgStore.Users().Create(ctx, user); err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	fmt.Printf("  User created: %s (%s)\n", user.Email, user.DisplayName)

	// Provision org + team
	orgSlug := appCfg.Storage.Auth.GetDefaultOrgSlug()
	orgName := appCfg.Storage.Auth.GetDefaultOrgName()
	dbName := pgstore.OrgDBName(appCfg.Storage.Postgres.InstanceSuffix, orgSlug)
	teamSlug := "general"

	org := &store.Organization{
		ID:        uuid.New().String(),
		Name:      orgName,
		Slug:      orgSlug,
		DBName:    dbName,
		Status:    "active",
		CreatedAt: time.Now(),
	}

	if err := pgStore.Organizations().Create(ctx, org); err != nil {
		return fmt.Errorf("failed to create organization: %w", err)
	}
	fmt.Printf("  Organization: %s (%s)\n", org.Name, org.Slug)

	if err := pgStore.ProvisionOrg(ctx, org.ID, orgSlug); err != nil {
		return fmt.Errorf("failed to provision org database: %w", err)
	}

	if err := pgStore.Organizations().AddMember(ctx, user.ID, org.ID, "owner"); err != nil {
		return fmt.Errorf("failed to add user to org: %w", err)
	}

	orgDataStore, err := pgStore.ForOrg(orgSlug)
	if err != nil {
		return fmt.Errorf("failed to connect to org database: %w", err)
	}

	defaultTeam := &store.Team{
		ID:         uuid.New().String(),
		Name:       "General",
		Slug:       teamSlug,
		SchemaName: pgstore.TeamSchemaName(teamSlug),
		CreatedAt:  time.Now(),
	}
	if err := orgDataStore.Teams().CreateTeam(ctx, defaultTeam); err != nil {
		return fmt.Errorf("failed to create team: %w", err)
	}

	if err := orgDataStore.ProvisionTeam(ctx, teamSlug); err != nil {
		return fmt.Errorf("failed to provision team schema: %w", err)
	}

	if err := orgDataStore.Teams().AddMember(ctx, &store.TeamMembership{
		UserID:   user.ID,
		TeamID:   defaultTeam.ID,
		Role:     "admin",
		JoinedAt: time.Now(),
	}); err != nil {
		fmt.Printf("  Warning: failed to add user to team: %v\n", err)
	}

	if err := orgDataStore.ProvisionPersonalSchema(ctx, user.ID); err != nil {
		fmt.Printf("  Warning: failed to provision personal schema: %v\n", err)
	}

	fmt.Printf("  Team: %s\n", defaultTeam.Name)
	fmt.Println()

	// Run migration
	fmt.Println("Starting migration...")
	fmt.Println()

	migrator := migration.New(migration.Config{
		ConfigDir: configDir,
		PGStore:   pgStore,
		OrgSlug:   orgSlug,
		TeamSlug:  teamSlug,
		UserID:    user.ID,
		AppCfg:    appCfg,
	})

	// Terminal progress display
	migrator.SetProgressFunc(func(p migration.Progress) {
		printTerminalProgress(p)
	})

	summary, err := migrator.Run(ctx)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	fmt.Println()
	printSummary(summary)

	if summary.Success {
		fmt.Println()
		fmt.Println("You can now start the daemon with: astonish daemon run")
		fmt.Println("Or launch Studio with: astonish studio")
	}

	return nil
}

func handleMigrateStatus() error {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	if migration.IsMigrationComplete(configDir) {
		fmt.Println("Status: COMPLETE")
		fmt.Println("Marker: " + configDir + "/.migration-complete")
		return nil
	}

	hasData := migration.HasFileData(configDir)
	appCfg, _ := config.LoadAppConfig()
	isPlatform := appCfg != nil && appCfg.Storage.Backend == "postgres"

	if !hasData {
		fmt.Println("Status: NOT NEEDED (no file data found)")
		return nil
	}

	if !isPlatform {
		fmt.Println("Status: NOT AVAILABLE")
		fmt.Println("  File data exists but storage backend is not PostgreSQL.")
		fmt.Println("  Set storage.backend to 'postgres' in your config to enable migration.")
		return nil
	}

	fmt.Println("Status: AVAILABLE")
	fmt.Println("  File data exists and can be migrated to PostgreSQL.")
	fmt.Println("  Run 'astonish migrate' to start the migration.")
	return nil
}

// promptCredentials prompts for email, display name, and password from the terminal.
func promptCredentials(appCfg *config.AppConfig) (email, displayName, password string, err error) {
	// Pre-fill from config identity
	defaultEmail := ""
	defaultName := ""
	if appCfg.AgentIdentity.Email != "" {
		defaultEmail = appCfg.AgentIdentity.Email
	}
	if appCfg.AgentIdentity.Name != "" {
		defaultName = appCfg.AgentIdentity.Name
	}

	// Email
	if defaultEmail != "" {
		fmt.Printf("Email [%s]: ", defaultEmail)
	} else {
		fmt.Print("Email: ")
	}
	email = readLine()
	if email == "" {
		email = defaultEmail
	}
	if email == "" {
		return "", "", "", fmt.Errorf("email is required")
	}

	// Display name
	if defaultName != "" {
		fmt.Printf("Display name [%s]: ", defaultName)
	} else {
		fmt.Print("Display name: ")
	}
	displayName = readLine()
	if displayName == "" {
		displayName = defaultName
	}
	if displayName == "" {
		displayName = strings.Split(email, "@")[0]
	}

	// Password (hidden input)
	for {
		fmt.Print("Password (min 8 chars): ")
		pwd1, readErr := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if readErr != nil {
			return "", "", "", fmt.Errorf("failed to read password: %w", readErr)
		}
		if len(pwd1) < 8 {
			fmt.Println("  Password must be at least 8 characters. Try again.")
			continue
		}

		fmt.Print("Confirm password: ")
		pwd2, readErr := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if readErr != nil {
			return "", "", "", fmt.Errorf("failed to read password: %w", readErr)
		}

		if string(pwd1) != string(pwd2) {
			fmt.Println("  Passwords do not match. Try again.")
			continue
		}

		password = string(pwd1)
		break
	}

	return email, displayName, password, nil
}

// readLine reads a single line from stdin.
func readLine() string {
	var buf [1024]byte
	n, _ := os.Stdin.Read(buf[:])
	return strings.TrimSpace(string(buf[:n]))
}

// categoryLabel returns a human-readable label for a migration category.
func categoryLabel(cat migration.Category) string {
	labels := map[migration.Category]string{
		migration.CatCredentials: "Credentials",
		migration.CatSessions:   "Chat Sessions",
		migration.CatApps:       "Apps",
		migration.CatFlows:      "Flows",
		migration.CatScheduler:  "Scheduled Jobs",
		migration.CatFleets:     "Fleet Templates",
		migration.CatSkills:     "Skills",
		migration.CatMemory:     "Memory & Knowledge",
	}
	if label, ok := labels[cat]; ok {
		return label
	}
	return string(cat)
}

// printTerminalProgress displays a progress update in the terminal.
func printTerminalProgress(p migration.Progress) {
	label := categoryLabel(p.Category)

	switch p.Status {
	case "counting":
		fmt.Printf("  [..] %s: scanning...\n", label)
	case "migrating":
		if p.Total > 0 {
			fmt.Printf("\r  [>>] %s: %d/%d", label, p.Current, p.Total)
		}
	case "done":
		if p.Total > 0 {
			fmt.Printf("\r  [OK] %s: %d items migrated\n", label, p.Total)
		} else {
			fmt.Printf("  [OK] %s: done\n", label)
		}
	case "skipped":
		fmt.Printf("  [--] %s: skipped\n", label)
	case "error":
		fmt.Printf("\r  [!!] %s: %s\n", label, p.Error)
	}
}

// printSummary prints the migration summary to the terminal.
func printSummary(s *migration.Summary) {
	if s.Success {
		fmt.Println("=== Migration Complete ===")
	} else {
		fmt.Println("=== Migration Complete (with errors) ===")
	}
	fmt.Printf("  Duration: %s\n", s.Duration.Round(time.Millisecond))
	fmt.Println()

	for _, cat := range migration.AllCategories {
		count := s.Categories[cat]
		if count > 0 {
			fmt.Printf("  %-20s %d items\n", categoryLabel(cat), count)
		}
	}

	if len(s.Errors) > 0 {
		fmt.Println()
		fmt.Println("  Errors:")
		for _, e := range s.Errors {
			fmt.Printf("    - %s\n", e)
		}
	}
}

func printMigrateUsage() {
	fmt.Println("usage: astonish migrate [command]")
	fmt.Println("")
	fmt.Println("Migrate data from file-based personal mode to PostgreSQL platform mode.")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  (none)     Run the migration interactively")
	fmt.Println("  status     Check migration status")
	fmt.Println("")
	fmt.Println("The migration reads all data from ~/.config/astonish/ and writes it into")
	fmt.Println("the configured PostgreSQL database. After migration, original files are")
	fmt.Println("renamed with a backup suffix.")
	fmt.Println("")
	fmt.Println("Prerequisites:")
	fmt.Println("  1. Configure storage.backend: postgres in config.yaml")
	fmt.Println("  2. Configure the PostgreSQL connection in storage.postgres")
	fmt.Println("  3. Run this command before starting the daemon in platform mode")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  astonish migrate          # Start interactive migration")
	fmt.Println("  astonish migrate status    # Check if migration is needed")
}
