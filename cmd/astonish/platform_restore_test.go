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
	opts, err := parsePlatformRestoreArgs([]string{"backup.astonish-backup", "--confirm", "--reset-target", "--yes", "--enable-scheduled-jobs", "--include-transient"})
	if err != nil {
		t.Fatalf("parsePlatformRestoreArgs() error = %v", err)
	}
	if !opts.confirm || !opts.resetTarget || !opts.yes || !opts.enableScheduledJobs || !opts.includeTransient {
		t.Fatalf("opts = %+v, want confirm, resetTarget, yes, enableScheduledJobs, includeTransient", opts)
	}
}

func TestValidatePlatformRestoreOptionsRequiresYesForReset(t *testing.T) {
	err := validatePlatformRestoreOptions(platformRestoreOptions{archivePath: "backup.astonish-backup", confirm: true, resetTarget: true})
	if err == nil {
		t.Fatal("validatePlatformRestoreOptions() error = nil, want --yes requirement")
	}
}

func TestValidatePlatformRestoreOptionsAllowsResetWithYes(t *testing.T) {
	err := validatePlatformRestoreOptions(platformRestoreOptions{archivePath: "backup.astonish-backup", confirm: true, resetTarget: true, yes: true})
	if err != nil {
		t.Fatalf("validatePlatformRestoreOptions() error = %v, want nil", err)
	}
}
