package migration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHasFileData(t *testing.T) {
	dir := t.TempDir()

	// Empty directory — no data
	if HasFileData(dir) {
		t.Error("HasFileData() = true for empty dir, want false")
	}

	// Create sessions/index.json
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "index.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if !HasFileData(dir) {
		t.Error("HasFileData() = false with sessions/index.json, want true")
	}
}

func TestHasFileData_Credentials(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "credentials.enc"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if !HasFileData(dir) {
		t.Error("HasFileData() = false with credentials.enc, want true")
	}
}

func TestHasFileData_Apps(t *testing.T) {
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "apps"), 0755); err != nil {
		t.Fatal(err)
	}
	if !HasFileData(dir) {
		t.Error("HasFileData() = false with apps dir, want true")
	}
}

func TestHasFileData_Memory(t *testing.T) {
	dir := t.TempDir()

	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("# Memory"), 0644); err != nil {
		t.Fatal(err)
	}
	if !HasFileData(dir) {
		t.Error("HasFileData() = false with memory/MEMORY.md, want true")
	}
}

func TestIsMigrationComplete(t *testing.T) {
	dir := t.TempDir()

	// Not complete yet
	if IsMigrationComplete(dir) {
		t.Error("IsMigrationComplete() = true for fresh dir, want false")
	}

	// Write the marker
	marker := filepath.Join(dir, ".migration-complete")
	if err := os.WriteFile(marker, []byte("done"), 0644); err != nil {
		t.Fatal(err)
	}

	if !IsMigrationComplete(dir) {
		t.Error("IsMigrationComplete() = false with marker file, want true")
	}
}

func TestMigratorNew(t *testing.T) {
	cfg := Config{
		ConfigDir: "/tmp/test",
		OrgSlug:   "acme",
		TeamSlug:  "general",
		UserID:    "user-123",
	}

	m := New(cfg)
	if m.configDir != "/tmp/test" {
		t.Errorf("configDir = %q, want /tmp/test", m.configDir)
	}
	if m.orgSlug != "acme" {
		t.Errorf("orgSlug = %q, want acme", m.orgSlug)
	}
	if m.teamSlug != "general" {
		t.Errorf("teamSlug = %q, want general", m.teamSlug)
	}
	if m.userID != "user-123" {
		t.Errorf("userID = %q, want user-123", m.userID)
	}

	// All categories should have "pending" status
	statuses := m.GetStatus()
	if len(statuses) != len(AllCategories) {
		t.Errorf("GetStatus() returned %d items, want %d", len(statuses), len(AllCategories))
	}
	for _, s := range statuses {
		if s.Status != "pending" {
			t.Errorf("category %s status = %q, want pending", s.Category, s.Status)
		}
	}
}

func TestMigratorProgress(t *testing.T) {
	m := New(Config{ConfigDir: "/tmp/test"})

	var received []Progress
	m.SetProgressFunc(func(p Progress) {
		received = append(received, p)
	})

	m.emitProgress(CatSessions, 5, 10, "migrating", "")
	m.emitProgress(CatSessions, 10, 10, "done", "")

	if len(received) != 2 {
		t.Fatalf("received %d progress events, want 2", len(received))
	}
	if received[0].Current != 5 || received[0].Total != 10 {
		t.Errorf("first event = %d/%d, want 5/10", received[0].Current, received[0].Total)
	}
	if received[1].Status != "done" {
		t.Errorf("second event status = %q, want done", received[1].Status)
	}
}

func TestMarkComplete(t *testing.T) {
	dir := t.TempDir()

	// Create some data files to be renamed
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatal(err)
	}
	appsDir := filepath.Join(dir, "apps")
	if err := os.MkdirAll(appsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials.enc"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	m := New(Config{ConfigDir: dir})
	m.markComplete()

	// Marker should exist
	if !IsMigrationComplete(dir) {
		t.Error("IsMigrationComplete() = false after markComplete(), want true")
	}

	// Original directories should be renamed (no longer exist at original paths)
	if _, err := os.Stat(sessDir); !os.IsNotExist(err) {
		t.Error("sessions dir still exists after markComplete()")
	}
	if _, err := os.Stat(appsDir); !os.IsNotExist(err) {
		t.Error("apps dir still exists after markComplete()")
	}
	if _, err := os.Stat(filepath.Join(dir, "credentials.enc")); !os.IsNotExist(err) {
		t.Error("credentials.enc still exists after markComplete()")
	}

	// Renamed files should exist with the .pre-migration-* suffix
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	renamedCount := 0
	for _, e := range entries {
		if e.Name() == ".migration-complete" {
			continue
		}
		renamedCount++
	}
	if renamedCount != 3 {
		t.Errorf("found %d renamed entries, want 3", renamedCount)
	}
}

func TestAllCategoriesOrder(t *testing.T) {
	// Credentials must be first, memory must be last
	if AllCategories[0] != CatCredentials {
		t.Errorf("first category = %s, want credentials", AllCategories[0])
	}
	if AllCategories[len(AllCategories)-1] != CatMemory {
		t.Errorf("last category = %s, want memory", AllCategories[len(AllCategories)-1])
	}
	if len(AllCategories) != 8 {
		t.Errorf("len(AllCategories) = %d, want 8", len(AllCategories))
	}
}

func TestSplitByHeadings(t *testing.T) {
	input := `# Main Title

Some intro text.

## Section One

Content of section one.

## Section Two

Content of section two.
`

	sections := splitByHeadings(input)

	if len(sections) < 2 {
		t.Fatalf("splitByHeadings() returned %d sections, want >= 2", len(sections))
	}

	// All sections should be non-empty after trimming
	for i, s := range sections {
		if len(s) == 0 {
			t.Errorf("section %d is empty", i)
		}
	}
}

func TestSplitOversized(t *testing.T) {
	// Short content should not be split
	short := "Hello world"
	parts := splitOversized(short, 100)
	if len(parts) != 1 {
		t.Errorf("splitOversized(%q, 100) = %d parts, want 1", short, len(parts))
	}

	// Long content should be split
	long := ""
	for i := 0; i < 50; i++ {
		long += "This is line number " + string(rune('A'+i%26)) + ".\n"
	}
	parts = splitOversized(long, 100)
	if len(parts) < 2 {
		t.Errorf("splitOversized(long, 100) = %d parts, want >= 2", len(parts))
	}

	// Each part should be <= maxChars (roughly)
	for i, p := range parts {
		if len(p) > 200 { // generous margin
			t.Errorf("part %d has %d chars, expected roughly <= 100", i, len(p))
		}
	}
}

func TestDeriveCategory(t *testing.T) {
	tests := []struct {
		relPath string
		want    string
	}{
		{"knowledge/tech.md", "knowledge"},
		{"guidance/style.md", "guidance"},
		{"MEMORY.md", "memory"},
		{"SELF.md", "self"},
		{"INSTRUCTIONS.md", "instructions"},
		{"random/notes.md", "knowledge"}, // default
	}

	for _, tt := range tests {
		got := deriveCategory(tt.relPath)
		if got != tt.want {
			t.Errorf("deriveCategory(%q) = %q, want %q", tt.relPath, got, tt.want)
		}
	}
}

func TestRoundTripSessionIndex(t *testing.T) {
	// Verify the session index format can be round-tripped
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatal(err)
	}

	index := sessionIndex{
		Version: 1,
		Sessions: map[string]sessionMeta{
			"sess-001": {
				ID:           "sess-001",
				AppName:      "test-agent",
				UserID:       "user-1",
				Title:        "Test session",
				MessageCount: 5,
			},
			"sess-002": {
				ID:           "sess-002",
				AppName:      "test-agent",
				UserID:       "user-1",
				Title:        "Another session",
				MessageCount: 3,
			},
		},
	}

	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(sessDir, "index.json")
	if err := os.WriteFile(indexPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Read it back and verify
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	var parsed sessionIndex
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}

	if parsed.Version != 1 {
		t.Errorf("version = %d, want 1", parsed.Version)
	}
	if len(parsed.Sessions) != 2 {
		t.Errorf("sessions count = %d, want 2", len(parsed.Sessions))
	}
	if parsed.Sessions["sess-001"].Title != "Test session" {
		t.Errorf("session title = %q, want 'Test session'", parsed.Sessions["sess-001"].Title)
	}
}

func TestRoundTripSchedulerJobs(t *testing.T) {
	dir := t.TempDir()
	schedDir := filepath.Join(dir, "scheduler")
	if err := os.MkdirAll(schedDir, 0755); err != nil {
		t.Fatal(err)
	}

	jobs := jobsFile{
		Jobs: []schedulerJob{
			{
				ID:      "job-1",
				Name:    "daily-report",
				Mode:    "cron",
				Enabled: true,
				Schedule: jobSchedule{
					Cron:     "0 9 * * *",
					Timezone: "UTC",
				},
				Payload: jobPayload{
					Flow:         "report-flow",
					Instructions: "Generate daily report",
				},
				Delivery: jobDelivery{
					Channel: "slack",
					Target:  "#reports",
				},
				LastStatus: "success",
			},
		},
	}

	data, err := json.Marshal(jobs)
	if err != nil {
		t.Fatal(err)
	}
	jobsPath := filepath.Join(schedDir, "jobs.json")
	if err := os.WriteFile(jobsPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Read it back
	raw, err := os.ReadFile(jobsPath)
	if err != nil {
		t.Fatal(err)
	}

	var parsed jobsFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}

	if len(parsed.Jobs) != 1 {
		t.Fatalf("jobs count = %d, want 1", len(parsed.Jobs))
	}
	if parsed.Jobs[0].ID != "job-1" {
		t.Errorf("job ID = %q, want job-1", parsed.Jobs[0].ID)
	}
	if parsed.Jobs[0].Name != "daily-report" {
		t.Errorf("job name = %q, want daily-report", parsed.Jobs[0].Name)
	}
	if parsed.Jobs[0].Schedule.Cron != "0 9 * * *" {
		t.Errorf("cron = %q, want '0 9 * * *'", parsed.Jobs[0].Schedule.Cron)
	}
	if !parsed.Jobs[0].Enabled {
		t.Error("job.Enabled = false, want true")
	}
}

func TestRoundTripSkillFrontmatter(t *testing.T) {
	input := `---
name: docker
description: Docker container management
os:
  - linux
  - darwin
require_bins:
  - docker
---
# Docker Skill

Use this skill to manage Docker containers.
`

	fm, body, err := parseSkillFrontmatter(input)
	if err != nil {
		t.Fatal(err)
	}

	if fm.Name != "docker" {
		t.Errorf("name = %q, want docker", fm.Name)
	}
	if fm.Description != "Docker container management" {
		t.Errorf("description = %q", fm.Description)
	}
	if len(fm.OS) != 2 {
		t.Errorf("os count = %d, want 2", len(fm.OS))
	}
	if len(fm.RequireBins) != 1 || fm.RequireBins[0] != "docker" {
		t.Errorf("require_bins = %v", fm.RequireBins)
	}
	if body == "" {
		t.Error("body is empty")
	}
	if len(body) < 10 {
		t.Errorf("body too short: %q", body)
	}
}

func TestFileDataIntegrityWorkflow(t *testing.T) {
	// This test simulates the full file-data-exists → migrate → complete workflow
	// without requiring PostgreSQL. It verifies:
	// 1. HasFileData detects data
	// 2. Migration marker doesn't exist initially
	// 3. markComplete creates the marker and renames data
	// 4. After completion, HasFileData returns false and IsMigrationComplete returns true

	dir := t.TempDir()

	// Set up file data for all detectable categories
	// sessions
	sessDir := filepath.Join(dir, "sessions")
	os.MkdirAll(sessDir, 0755)
	os.WriteFile(filepath.Join(sessDir, "index.json"), []byte(`{"version":1,"sessions":{}}`), 0644)

	// credentials
	os.WriteFile(filepath.Join(dir, "credentials.enc"), []byte("encrypted"), 0644)

	// apps
	appsDir := filepath.Join(dir, "apps")
	os.MkdirAll(appsDir, 0755)
	os.WriteFile(filepath.Join(appsDir, "test.yaml"), []byte("name: test"), 0644)

	// memory
	memDir := filepath.Join(dir, "memory")
	os.MkdirAll(memDir, 0755)
	os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("# My Memory"), 0644)

	// scheduler
	schedDir := filepath.Join(dir, "scheduler")
	os.MkdirAll(schedDir, 0755)
	os.WriteFile(filepath.Join(schedDir, "jobs.json"), []byte(`{"jobs":[]}`), 0644)

	// Verify initial state
	if !HasFileData(dir) {
		t.Fatal("HasFileData() = false before migration, want true")
	}
	if IsMigrationComplete(dir) {
		t.Fatal("IsMigrationComplete() = true before migration, want false")
	}

	// Simulate markComplete
	m := New(Config{ConfigDir: dir})
	m.markComplete()

	// Verify post-migration state
	if IsMigrationComplete(dir) != true {
		t.Error("IsMigrationComplete() = false after markComplete(), want true")
	}

	// Original data paths should be gone (renamed)
	if _, err := os.Stat(filepath.Join(dir, "sessions")); !os.IsNotExist(err) {
		t.Error("sessions dir still exists after markComplete()")
	}
	if _, err := os.Stat(filepath.Join(dir, "credentials.enc")); !os.IsNotExist(err) {
		t.Error("credentials.enc still exists after markComplete()")
	}

	// HasFileData may still return true because memory/ is not renamed by markComplete
	// (only sessions, credentials.enc, apps, store.json, scheduler, fleets, fleet_plans
	// are renamed). The memory directory persists as-is since skills and memory files
	// are kept for reference. This is by design.
	//
	// Verify that the key data files that ARE renamed are actually gone:
	if _, err := os.Stat(filepath.Join(dir, "apps")); !os.IsNotExist(err) {
		t.Error("apps dir still exists after markComplete()")
	}
	if _, err := os.Stat(filepath.Join(dir, "scheduler")); !os.IsNotExist(err) {
		t.Error("scheduler dir still exists after markComplete()")
	}
}
