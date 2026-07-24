package entstore

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/SAP/astonish/pkg/backup"
	"github.com/SAP/astonish/pkg/store"
)

func TestRestorePlatformBackupSQLiteFreshTarget(t *testing.T) {
	ctx := context.Background()
	sourceDir := t.TempDir()
	sourceDSN := "file:" + filepath.Join(sourceDir, "platform.db")
	if err := BootstrapPlatform(ctx, Config{DSN: sourceDSN, DataDir: sourceDir}, nil); err != nil {
		t.Fatalf("BootstrapPlatform(source) error = %v", err)
	}
	source, err := New(ctx, Config{DSN: sourceDSN, DataDir: sourceDir})
	if err != nil {
		t.Fatalf("New(source) error = %v", err)
	}
	defer source.Close()

	userID := uuid.NewString()
	orgID := uuid.NewString()
	if err := source.Users().Create(ctx, &store.User{ID: userID, Email: "restore@example.com", DisplayName: "Restore User", PasswordHash: "hash", Status: "active"}); err != nil {
		t.Fatalf("Create user error = %v", err)
	}
	if err := source.Organizations().Create(ctx, &store.Organization{ID: orgID, Name: "Restore Org", Slug: "restore", Status: "active"}); err != nil {
		t.Fatalf("Create org error = %v", err)
	}
	if err := source.Organizations().AddMember(ctx, userID, orgID, "owner"); err != nil {
		t.Fatalf("AddMember error = %v", err)
	}
	if err := source.ProvisionOrg(ctx, orgID, "restore"); err != nil {
		t.Fatalf("ProvisionOrg error = %v", err)
	}
	orgStore, err := source.ForOrg("restore")
	if err != nil {
		t.Fatalf("ForOrg error = %v", err)
	}
	team := &store.Team{ID: uuid.NewString(), Name: "Ops", Slug: "ops", SchemaName: "ops"}
	if err := orgStore.Teams().CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam error = %v", err)
	}
	if err := orgStore.ProvisionTeam(ctx, "ops"); err != nil {
		t.Fatalf("ProvisionTeam error = %v", err)
	}
	if err := orgStore.ProvisionPersonalSchema(ctx, userID); err != nil {
		t.Fatalf("ProvisionPersonalSchema error = %v", err)
	}
	seedTeamBackupRows(t, filepath.Join(sourceDir, "orgs", "restore", "teams", "ops.db"))
	seedSessionBackupRows(t, filepath.Join(sourceDir, "orgs", "restore", "teams", "ops.db"))
	seedScheduledJob(t, filepath.Join(sourceDir, "orgs", "restore", "teams", "ops.db"))

	archivePath := filepath.Join(t.TempDir(), "restore.astonish-backup")
	if err := source.ExportPlatformBackup(ctx, archivePath, PlatformBackupExportOptions{Backend: "sqlite"}); err != nil {
		t.Fatalf("ExportPlatformBackup() error = %v", err)
	}

	targetDir := t.TempDir()
	targetDSN := "file:" + filepath.Join(targetDir, "platform.db")
	if err := BootstrapPlatform(ctx, Config{DSN: targetDSN, DataDir: targetDir}, nil); err != nil {
		t.Fatalf("BootstrapPlatform(target) error = %v", err)
	}
	target, err := New(ctx, Config{DSN: targetDSN, DataDir: targetDir})
	if err != nil {
		t.Fatalf("New(target) error = %v", err)
	}
	defer target.Close()

	plan, err := target.PlanPlatformRestore(ctx, archivePath, PlatformRestoreOptions{DryRun: true})
	if err != nil {
		t.Fatalf("PlanPlatformRestore() error = %v", err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("plan blockers = %v, want none", plan.Blockers)
	}
	result, err := target.RestorePlatformBackup(ctx, archivePath, PlatformRestoreOptions{})
	if err != nil {
		t.Fatalf("RestorePlatformBackup() error = %v", err)
	}
	if result.RestoredRecords == 0 {
		t.Fatal("RestoredRecords = 0, want records")
	}

	files, err := backup.ReadArchiveFiles(archivePath)
	if err != nil {
		t.Fatalf("ReadArchiveFiles() error = %v", err)
	}
	if !strings.Contains(string(files["orgs/restore/teams/ops/apps.jsonl"]), "backup-app") {
		t.Fatal("source archive missing seeded app")
	}
	assertRestoredTableContains(t, filepath.Join(targetDir, "platform.db"), "users", "password_hash", "hash")
	assertRestoredTableContains(t, filepath.Join(targetDir, "orgs", "restore", "teams", "ops.db"), "apps", "slug", "backup-app")
	assertRestoredTableContains(t, filepath.Join(targetDir, "orgs", "restore", "teams", "ops.db"), "flows", "name", "backup-flow")
	assertRestoredTableContains(t, filepath.Join(targetDir, "orgs", "restore", "teams", "ops.db"), "sessions", "title", "backup-session")
	assertRestoredTableContains(t, filepath.Join(targetDir, "orgs", "restore", "teams", "ops.db"), "session_events", "session_id", "backup-session-id")
	assertRestoredScheduledJobPaused(t, filepath.Join(targetDir, "orgs", "restore", "teams", "ops.db"))
}

func TestRestorePlatformBackupSQLiteMappedOrgAndTeam(t *testing.T) {
	ctx := context.Background()
	sourceDir := t.TempDir()
	sourceDSN := "file:" + filepath.Join(sourceDir, "platform.db")
	if err := BootstrapPlatform(ctx, Config{DSN: sourceDSN, DataDir: sourceDir}, nil); err != nil {
		t.Fatalf("BootstrapPlatform(source) error = %v", err)
	}
	source, err := New(ctx, Config{DSN: sourceDSN, DataDir: sourceDir})
	if err != nil {
		t.Fatalf("New(source) error = %v", err)
	}
	defer source.Close()

	userID := uuid.NewString()
	orgID := uuid.NewString()
	if err := source.Users().Create(ctx, &store.User{ID: userID, Email: "mapped@example.com", DisplayName: "Mapped User", PasswordHash: "hash", Status: "active"}); err != nil {
		t.Fatalf("Create user error = %v", err)
	}
	if err := source.Organizations().Create(ctx, &store.Organization{ID: orgID, Name: "Original Org", Slug: "oldorg", Status: "active"}); err != nil {
		t.Fatalf("Create org error = %v", err)
	}
	if err := source.ProvisionOrg(ctx, orgID, "oldorg"); err != nil {
		t.Fatalf("ProvisionOrg error = %v", err)
	}
	orgStore, err := source.ForOrg("oldorg")
	if err != nil {
		t.Fatalf("ForOrg error = %v", err)
	}
	team := &store.Team{ID: uuid.NewString(), Name: "Ops", Slug: "ops", SchemaName: "ops"}
	if err := orgStore.Teams().CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam error = %v", err)
	}
	if err := orgStore.ProvisionTeam(ctx, "ops"); err != nil {
		t.Fatalf("ProvisionTeam error = %v", err)
	}
	seedTeamBackupRows(t, filepath.Join(sourceDir, "orgs", "oldorg", "teams", "ops.db"))

	archivePath := filepath.Join(t.TempDir(), "mapped.astonish-backup")
	if err := source.ExportPlatformBackup(ctx, archivePath, PlatformBackupExportOptions{Backend: "sqlite"}); err != nil {
		t.Fatalf("ExportPlatformBackup() error = %v", err)
	}

	targetDir := t.TempDir()
	targetDSN := "file:" + filepath.Join(targetDir, "platform.db")
	if err := BootstrapPlatform(ctx, Config{DSN: targetDSN, DataDir: targetDir}, nil); err != nil {
		t.Fatalf("BootstrapPlatform(target) error = %v", err)
	}
	target, err := New(ctx, Config{DSN: targetDSN, DataDir: targetDir})
	if err != nil {
		t.Fatalf("New(target) error = %v", err)
	}
	defer target.Close()

	result, err := target.RestorePlatformBackup(ctx, archivePath, PlatformRestoreOptions{
		MapOrg:  map[string]string{"oldorg": "neworg"},
		MapTeam: map[string]string{"oldorg/ops": "neworg/platform"},
	})
	if err != nil {
		t.Fatalf("RestorePlatformBackup(mapped) error = %v", err)
	}
	if result.RestoredRecords == 0 {
		t.Fatal("RestoredRecords = 0, want records")
	}
	assertRestoredTableContains(t, filepath.Join(targetDir, "platform.db"), "organizations", "slug", "neworg")
	assertRestoredTableNotContains(t, filepath.Join(targetDir, "platform.db"), "organizations", "slug", "oldorg")
	assertRestoredTableContains(t, filepath.Join(targetDir, "orgs", "neworg", "org.db"), "teams", "slug", "platform")
	assertRestoredTableContains(t, filepath.Join(targetDir, "orgs", "neworg", "org.db"), "teams", "schema_name", "team_platform")
	assertRestoredTableContains(t, filepath.Join(targetDir, "orgs", "neworg", "teams", "platform.db"), "apps", "slug", "backup-app")
	if _, err := os.Stat(filepath.Join(targetDir, "orgs", "oldorg")); !os.IsNotExist(err) {
		t.Fatalf("old org directory exists after mapped restore, stat error = %v", err)
	}
}

func TestRestorePlatformBackupSQLiteRestoreCredentialPreflightValidKey(t *testing.T) {
	ctx := context.Background()
	t.Setenv("ASTONISH_MASTER_KEY", testMasterKeyHex)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	archivePath := createSQLiteBackupWithPersonalCredential(t, ctx)

	targetDir := t.TempDir()
	targetDSN := "file:" + filepath.Join(targetDir, "platform.db")
	if err := BootstrapPlatform(ctx, Config{DSN: targetDSN, DataDir: targetDir}, nil); err != nil {
		t.Fatalf("BootstrapPlatform(target) error = %v", err)
	}
	target, err := New(ctx, Config{DSN: targetDSN, DataDir: targetDir})
	if err != nil {
		t.Fatalf("New(target) error = %v", err)
	}
	defer target.Close()

	plan, err := target.PlanPlatformRestore(ctx, archivePath, PlatformRestoreOptions{DryRun: true})
	if err != nil {
		t.Fatalf("PlanPlatformRestore() error = %v", err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("plan blockers = %v, want none", plan.Blockers)
	}
}

func TestRestorePlatformBackupSQLiteRestoreCredentialPreflightWrongKey(t *testing.T) {
	ctx := context.Background()
	t.Setenv("ASTONISH_MASTER_KEY", testMasterKeyHex)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	archivePath := createSQLiteBackupWithPersonalCredential(t, ctx)

	t.Setenv("ASTONISH_MASTER_KEY", "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	targetDir := t.TempDir()
	targetDSN := "file:" + filepath.Join(targetDir, "platform.db")
	if err := BootstrapPlatform(ctx, Config{DSN: targetDSN, DataDir: targetDir}, nil); err != nil {
		t.Fatalf("BootstrapPlatform(target) error = %v", err)
	}
	target, err := New(ctx, Config{DSN: targetDSN, DataDir: targetDir})
	if err != nil {
		t.Fatalf("New(target) error = %v", err)
	}
	defer target.Close()

	plan, err := target.PlanPlatformRestore(ctx, archivePath, PlatformRestoreOptions{DryRun: true})
	if err != nil {
		t.Fatalf("PlanPlatformRestore() error = %v", err)
	}
	if !restorePlanHasBlocker(plan, "cannot decrypt its credential key") {
		t.Fatalf("plan blockers = %v, want credential key mismatch blocker", plan.Blockers)
	}
	if _, err := target.RestorePlatformBackup(ctx, archivePath, PlatformRestoreOptions{}); err == nil || !strings.Contains(err.Error(), "cannot decrypt its credential key") {
		t.Fatalf("RestorePlatformBackup() error = %v, want credential key blocker", err)
	}
}

func TestRestorePlatformBackupSQLiteRestoreCredentialPreflightMissingKey(t *testing.T) {
	ctx := context.Background()
	t.Setenv("ASTONISH_MASTER_KEY", testMasterKeyHex)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	archivePath := createSQLiteBackupWithPersonalCredential(t, ctx)

	t.Setenv("ASTONISH_MASTER_KEY", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	targetDir := t.TempDir()
	targetDSN := "file:" + filepath.Join(targetDir, "platform.db")
	if err := BootstrapPlatform(ctx, Config{DSN: targetDSN, DataDir: targetDir}, nil); err != nil {
		t.Fatalf("BootstrapPlatform(target) error = %v", err)
	}
	target, err := New(ctx, Config{DSN: targetDSN, DataDir: targetDir})
	if err != nil {
		t.Fatalf("New(target) error = %v", err)
	}
	defer target.Close()

	plan, err := target.PlanPlatformRestore(ctx, archivePath, PlatformRestoreOptions{DryRun: true})
	if err != nil {
		t.Fatalf("PlanPlatformRestore() error = %v", err)
	}
	if !restorePlanHasBlocker(plan, "no ASTONISH_MASTER_KEY or ~/.config/astonish/.store_key is configured") {
		t.Fatalf("plan blockers = %v, want missing key blocker", plan.Blockers)
	}
}

func TestLogicalSQLQuotesSQLiteIdentifiers(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "quoted.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite error = %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE "odd "" table" ("select" TEXT, "quote "" col" BLOB)`); err != nil {
		t.Fatalf("create quoted table error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO "odd "" table" ("select", "quote "" col") VALUES ('value', x'0102')`); err != nil {
		t.Fatalf("insert quoted table error = %v", err)
	}
	data, records, _, err := exportLogicalTable(ctx, db, DialectSQLite, "", `odd " table`, false)
	if err != nil {
		t.Fatalf("exportLogicalTable() error = %v", err)
	}
	if records != 1 {
		t.Fatalf("records = %d, want 1", records)
	}
	if !strings.Contains(string(data), `"select":"value"`) {
		t.Fatalf("exported data = %s, want quoted identifier row", data)
	}
}

func TestRestorePlatformBackupSQLiteResetTarget(t *testing.T) {
	ctx := context.Background()
	sourceDir := t.TempDir()
	sourceDSN := "file:" + filepath.Join(sourceDir, "platform.db")
	if err := BootstrapPlatform(ctx, Config{DSN: sourceDSN, DataDir: sourceDir}, nil); err != nil {
		t.Fatalf("BootstrapPlatform(source) error = %v", err)
	}
	source, err := New(ctx, Config{DSN: sourceDSN, DataDir: sourceDir})
	if err != nil {
		t.Fatalf("New(source) error = %v", err)
	}
	defer source.Close()

	userID := uuid.NewString()
	orgID := uuid.NewString()
	if err := source.Users().Create(ctx, &store.User{ID: userID, Email: "reset-restore@example.com", DisplayName: "Reset Restore User", PasswordHash: "hash", Status: "active"}); err != nil {
		t.Fatalf("Create source user error = %v", err)
	}
	if err := source.Organizations().Create(ctx, &store.Organization{ID: orgID, Name: "Reset Restore Org", Slug: "reset-restore", Status: "active"}); err != nil {
		t.Fatalf("Create source org error = %v", err)
	}
	if err := source.Organizations().AddMember(ctx, userID, orgID, "owner"); err != nil {
		t.Fatalf("AddMember error = %v", err)
	}
	if err := source.ProvisionOrg(ctx, orgID, "reset-restore"); err != nil {
		t.Fatalf("ProvisionOrg error = %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "reset-restore.astonish-backup")
	if err := source.ExportPlatformBackup(ctx, archivePath, PlatformBackupExportOptions{Backend: "sqlite"}); err != nil {
		t.Fatalf("ExportPlatformBackup() error = %v", err)
	}

	targetDir := t.TempDir()
	targetDSN := "file:" + filepath.Join(targetDir, "platform.db")
	if err := BootstrapPlatform(ctx, Config{DSN: targetDSN, DataDir: targetDir}, nil); err != nil {
		t.Fatalf("BootstrapPlatform(target) error = %v", err)
	}
	target, err := New(ctx, Config{DSN: targetDSN, DataDir: targetDir})
	if err != nil {
		t.Fatalf("New(target) error = %v", err)
	}
	defer target.Close()
	if err := target.Users().Create(ctx, &store.User{ID: uuid.NewString(), Email: "junk@example.com", DisplayName: "Junk User", PasswordHash: "junk", Status: "active"}); err != nil {
		t.Fatalf("Create target junk user error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(targetDir, "orgs", "junk"), 0o750); err != nil {
		t.Fatalf("create target junk org dir error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(targetDir, "fleet_state", "junk"), 0o750); err != nil {
		t.Fatalf("create target junk fleet state dir error = %v", err)
	}

	blockedPlan, err := target.PlanPlatformRestore(ctx, archivePath, PlatformRestoreOptions{})
	if err != nil {
		t.Fatalf("PlanPlatformRestore() error = %v", err)
	}
	if len(blockedPlan.Blockers) == 0 {
		t.Fatal("PlanPlatformRestore() blockers = none, want non-empty target blocker")
	}

	result, err := target.RestorePlatformBackup(ctx, archivePath, PlatformRestoreOptions{ResetTarget: true})
	if err != nil {
		t.Fatalf("RestorePlatformBackup(ResetTarget) error = %v", err)
	}
	if !result.Plan.TargetEmpty {
		t.Fatal("result.Plan.TargetEmpty = false, want true after reset")
	}
	assertRestoredTableContains(t, filepath.Join(targetDir, "platform.db"), "users", "email", "reset-restore@example.com")
	assertRestoredTableNotContains(t, filepath.Join(targetDir, "platform.db"), "users", "email", "junk@example.com")
	if _, err := os.Stat(filepath.Join(targetDir, "orgs", "junk")); !os.IsNotExist(err) {
		t.Fatalf("junk org directory still exists after reset, stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "fleet_state", "junk")); !os.IsNotExist(err) {
		t.Fatalf("junk fleet state directory still exists after reset, stat error = %v", err)
	}
}

func createSQLiteBackupWithPersonalCredential(t *testing.T, ctx context.Context) string {
	t.Helper()
	sourceDir := t.TempDir()
	sourceDSN := "file:" + filepath.Join(sourceDir, "platform.db")
	if err := BootstrapPlatform(ctx, Config{DSN: sourceDSN, DataDir: sourceDir}, nil); err != nil {
		t.Fatalf("BootstrapPlatform(source) error = %v", err)
	}
	source, err := New(ctx, Config{DSN: sourceDSN, DataDir: sourceDir})
	if err != nil {
		t.Fatalf("New(source) error = %v", err)
	}
	defer source.Close()

	userID := uuid.NewString()
	orgID := uuid.NewString()
	if err := source.Users().Create(ctx, &store.User{ID: userID, Email: "cred-restore@example.com", DisplayName: "Cred Restore User", PasswordHash: "hash", Status: "active"}); err != nil {
		t.Fatalf("Create source user error = %v", err)
	}
	if err := source.Organizations().Create(ctx, &store.Organization{ID: orgID, Name: "Cred Restore Org", Slug: "cred-restore", Status: "active"}); err != nil {
		t.Fatalf("Create source org error = %v", err)
	}
	if err := source.Organizations().AddMember(ctx, userID, orgID, "owner"); err != nil {
		t.Fatalf("AddMember error = %v", err)
	}
	if err := source.ProvisionOrg(ctx, orgID, "cred-restore"); err != nil {
		t.Fatalf("ProvisionOrg error = %v", err)
	}
	orgStore, err := source.ForOrg("cred-restore")
	if err != nil {
		t.Fatalf("ForOrg error = %v", err)
	}
	if err := orgStore.ProvisionPersonalSchema(ctx, userID); err != nil {
		t.Fatalf("ProvisionPersonalSchema error = %v", err)
	}
	personalStore := orgStore.ForUser(userID)
	if err := personalStore.Credentials().Set(ctx, "sap-github-bearer", &store.Credential{Type: store.CredBearer, Token: "restored-token"}); err != nil {
		t.Fatalf("Set personal credential error = %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "credential-restore.astonish-backup")
	if err := source.ExportPlatformBackup(ctx, archivePath, PlatformBackupExportOptions{Backend: "sqlite"}); err != nil {
		t.Fatalf("ExportPlatformBackup() error = %v", err)
	}
	return archivePath
}

func restorePlanHasBlocker(plan *backup.RestorePlan, want string) bool {
	for _, blocker := range plan.Blockers {
		if strings.Contains(blocker, want) {
			return true
		}
	}
	return false
}

func seedSessionBackupRows(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open team db error = %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO sessions (id, title, message_count, metadata, last_seq, created_at, updated_at) VALUES ('backup-session-id', 'backup-session', 1, '{}', 1, datetime('now'), datetime('now'))`); err != nil {
		t.Fatalf("seed session failed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO session_events (id, session_id, event_data, created_at) VALUES (137, 'backup-session-id', '{}', datetime('now'))`); err != nil {
		t.Fatalf("seed session event failed: %v", err)
	}
}

func seedScheduledJob(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open team db error = %v", err)
	}
	defer db.Close()
	stmt := `INSERT INTO scheduled_jobs (id, name, schedule, mode, payload, status, last_status, last_error, consecutive_failures, created_at, updated_at) VALUES (?, 'backup-job', '* * * * *', 'routine', '{}', 'active', 'pending', '', 0, datetime('now'), datetime('now'))`
	if _, err := db.Exec(stmt, uuid.NewString()); err != nil {
		t.Fatalf("seed scheduled job failed: %v", err)
	}
}

func assertRestoredTableContains(t *testing.T, dbPath, table, column, want string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open restored db error = %v", err)
	}
	defer db.Close()
	var count int
	query := "SELECT COUNT(*) FROM " + quoteSQLiteIdent(table) + " WHERE " + quoteSQLiteIdent(column) + " = ?"
	if err := db.QueryRow(query, want).Scan(&count); err != nil {
		t.Fatalf("query restored table error = %v", err)
	}
	if count != 1 {
		t.Fatalf("%s.%s=%q count = %d, want 1", table, column, want, count)
	}
}

func assertRestoredTableNotContains(t *testing.T, dbPath, table, column, want string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open restored db error = %v", err)
	}
	defer db.Close()
	var count int
	query := "SELECT COUNT(*) FROM " + quoteSQLiteIdent(table) + " WHERE " + quoteSQLiteIdent(column) + " = ?"
	if err := db.QueryRow(query, want).Scan(&count); err != nil {
		t.Fatalf("query restored table error = %v", err)
	}
	if count != 0 {
		t.Fatalf("%s.%s=%q count = %d, want 0", table, column, want, count)
	}
}

func assertRestoredScheduledJobPaused(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open restored db error = %v", err)
	}
	defer db.Close()
	var status string
	if err := db.QueryRow("SELECT status FROM scheduled_jobs WHERE name = 'backup-job'").Scan(&status); err != nil {
		t.Fatalf("query scheduled job error = %v", err)
	}
	if status != "paused" {
		t.Fatalf("scheduled job status = %q, want paused", status)
	}
}
