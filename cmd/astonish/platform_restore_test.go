package astonish

import "testing"

func TestParsePlatformRestoreArgsDryRun(t *testing.T) {
	opts, err := parsePlatformRestoreArgs([]string{"backup.astonish-backup", "--dry-run", "--json"})
	if err != nil {
		t.Fatalf("parsePlatformRestoreArgs() error = %v", err)
	}
	if opts.archivePath != "backup.astonish-backup" {
		t.Fatalf("archivePath = %q, want backup.astonish-backup", opts.archivePath)
	}
	if !opts.dryRun || !opts.jsonOut {
		t.Fatalf("opts = %+v, want dryRun and jsonOut", opts)
	}
}

func TestParsePlatformRestoreArgsRequiresArchive(t *testing.T) {
	if _, err := parsePlatformRestoreArgs([]string{"--dry-run"}); err == nil {
		t.Fatal("parsePlatformRestoreArgs() error = nil, want missing archive error")
	}
}

func TestParsePlatformRestoreArgsRejectsDryRunAndConfirm(t *testing.T) {
	if _, err := parsePlatformRestoreArgs([]string{"backup.astonish-backup", "--dry-run", "--confirm"}); err == nil {
		t.Fatal("parsePlatformRestoreArgs() error = nil, want conflict error")
	}
}

func TestParsePlatformRestoreArgsOptions(t *testing.T) {
	opts, err := parsePlatformRestoreArgs([]string{"backup.astonish-backup", "--confirm", "--reset-target", "--enable-scheduled-jobs", "--include-transient"})
	if err != nil {
		t.Fatalf("parsePlatformRestoreArgs() error = %v", err)
	}
	if !opts.confirm || !opts.resetTarget || !opts.enableScheduledJobs || !opts.includeTransient {
		t.Fatalf("opts = %+v, want confirm, resetTarget, enableScheduledJobs, includeTransient", opts)
	}
}

func TestParsePlatformRestoreArgsMappings(t *testing.T) {
	opts, err := parsePlatformRestoreArgs([]string{
		"backup.astonish-backup",
		"--dry-run",
		"--map-org", "old:new",
		"--map-team", "old/ops:new/platform",
		"--map-user", "old/user-1:new/user-2",
	})
	if err != nil {
		t.Fatalf("parsePlatformRestoreArgs() error = %v", err)
	}
	if opts.mapOrg["old"] != "new" {
		t.Fatalf("mapOrg = %#v, want old:new", opts.mapOrg)
	}
	if opts.mapTeam["old/ops"] != "new/platform" {
		t.Fatalf("mapTeam = %#v, want old/ops:new/platform", opts.mapTeam)
	}
	if opts.mapUser["old/user-1"] != "new/user-2" {
		t.Fatalf("mapUser = %#v, want old/user-1:new/user-2", opts.mapUser)
	}
}

func TestParsePlatformRestoreArgsRejectsInvalidMapping(t *testing.T) {
	if _, err := parsePlatformRestoreArgs([]string{"backup.astonish-backup", "--dry-run", "--map-team", "old:new/team"}); err == nil {
		t.Fatal("parsePlatformRestoreArgs() error = nil, want invalid team mapping error")
	}
}

func TestValidatePlatformRestoreOptionsAllowsResetWithoutYes(t *testing.T) {
	err := validatePlatformRestoreOptions(platformRestoreOptions{archivePath: "backup.astonish-backup", confirm: true, resetTarget: true})
	if err != nil {
		t.Fatalf("validatePlatformRestoreOptions() error = %v, want nil", err)
	}
}

func TestParsePlatformRestoreArgsRejectsYes(t *testing.T) {
	if _, err := parsePlatformRestoreArgs([]string{"backup.astonish-backup", "--confirm", "--yes"}); err == nil {
		t.Fatal("parsePlatformRestoreArgs() error = nil, want unknown --yes error")
	}
}
