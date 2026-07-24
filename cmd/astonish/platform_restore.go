package astonish

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/SAP/astonish/pkg/backup"
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/store/entstore"
)

func handlePlatformRestoreCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printPlatformRestoreUsage()
		return nil
	}
	opts, err := parsePlatformRestoreArgs(args)
	if err != nil {
		return err
	}
	if err := validatePlatformRestoreOptions(opts); err != nil {
		return err
	}

	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	ctx := context.Background()
	entCfg, _, err := backupEntstoreConfig(appCfg)
	if err != nil {
		return err
	}
	if err := entstore.BootstrapPlatform(ctx, entCfg, nil); err != nil {
		return fmt.Errorf("failed to bootstrap target platform store: %w", err)
	}
	_, es, err := entstore.NewPlatformServices(ctx, entCfg)
	if err != nil {
		return fmt.Errorf("failed to connect to platform store: %w", err)
	}
	defer es.Close()

	restoreOpts := entstore.PlatformRestoreOptions{
		DryRun:              opts.dryRun,
		ResetTarget:         opts.resetTarget,
		EnableScheduledJobs: opts.enableScheduledJobs,
		IncludeTransient:    opts.includeTransient,
	}
	if opts.dryRun {
		plan, err := es.PlanPlatformRestore(ctx, opts.archivePath, restoreOpts)
		if err != nil {
			return err
		}
		return printRestorePlan(*plan, opts.jsonOut)
	}
	result, err := es.RestorePlatformBackup(ctx, opts.archivePath, restoreOpts)
	if err != nil {
		return err
	}
	return printRestoreResult(*result, opts.jsonOut)
}

type platformRestoreOptions struct {
	archivePath         string
	dryRun              bool
	confirm             bool
	resetTarget         bool
	yes                 bool
	enableScheduledJobs bool
	includeTransient    bool
	jsonOut             bool
}

func validatePlatformRestoreOptions(opts platformRestoreOptions) error {
	if !opts.dryRun && !opts.confirm {
		return fmt.Errorf("restore writes require --confirm; run with --dry-run first to preview")
	}
	if opts.resetTarget && !opts.yes {
		return fmt.Errorf("--reset-target deletes target data and requires --yes")
	}
	return nil
}

func parsePlatformRestoreArgs(args []string) (platformRestoreOptions, error) {
	var opts platformRestoreOptions
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			opts.dryRun = true
		case "--confirm":
			opts.confirm = true
		case "--reset-target":
			opts.resetTarget = true
		case "--yes":
			opts.yes = true
		case "--enable-scheduled-jobs":
			opts.enableScheduledJobs = true
		case "--include-transient":
			opts.includeTransient = true
		case "--json":
			opts.jsonOut = true
		case "-h", "--help":
			return opts, fmt.Errorf("--help must be the only argument")
		default:
			if opts.archivePath != "" {
				return opts, fmt.Errorf("unexpected argument: %s", args[i])
			}
			opts.archivePath = args[i]
		}
	}
	if opts.archivePath == "" {
		return opts, fmt.Errorf("backup archive path is required")
	}
	if opts.dryRun && opts.confirm {
		return opts, fmt.Errorf("--dry-run and --confirm cannot be used together")
	}
	return opts, nil
}

func printRestorePlan(plan backup.RestorePlan, jsonOut bool) error {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(plan)
	}
	fmt.Println("Restore dry run complete.")
	printRestorePlanSummary(plan)
	if len(plan.Blockers) > 0 {
		fmt.Println("Blockers:")
		for _, blocker := range plan.Blockers {
			fmt.Printf("  - %s\n", blocker)
		}
	}
	if len(plan.Warnings) > 0 {
		fmt.Println("Warnings:")
		for _, warning := range plan.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}
	return nil
}

func printRestoreResult(result backup.RestoreResult, jsonOut bool) error {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	fmt.Println("Restore completed successfully.")
	printRestorePlanSummary(result.Plan)
	fmt.Printf("Restored entries: %d\n", result.RestoredEntries)
	fmt.Printf("Restored records: %d\n", result.RestoredRecords)
	if result.SkippedEntries > 0 {
		fmt.Printf("Skipped entries: %d\n", result.SkippedEntries)
	}
	if len(result.Warnings) > 0 {
		fmt.Println("Warnings:")
		for _, warning := range result.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}
	return nil
}

func printRestorePlanSummary(plan backup.RestorePlan) {
	manifest := plan.Archive.Manifest
	fmt.Printf("Archive: %s v%d (%s, %s)\n", manifest.Format, manifest.FormatVersion, manifest.Backend, manifest.Mode)
	fmt.Printf("Target backend: %s\n", plan.TargetBackend)
	fmt.Printf("Target empty: %t\n", plan.TargetEmpty)
	fmt.Printf("Scopes: %d\n", len(plan.Scopes))
	for _, scope := range plan.Scopes {
		fmt.Printf("  - %s", scope.Scope.Kind)
		if scope.Scope.OrgSlug != "" {
			fmt.Printf(" org=%s", scope.Scope.OrgSlug)
		}
		if scope.Scope.TeamSlug != "" {
			fmt.Printf(" team=%s", scope.Scope.TeamSlug)
		}
		if scope.Scope.UserID != "" {
			fmt.Printf(" user=%s", scope.Scope.UserID)
		}
		fmt.Printf(" (%d entries, %d records)\n", scope.Entries, scope.Records)
	}
	fmt.Printf("Entries: %d\n", len(plan.Entries))
}

func printPlatformRestoreUsage() {
	fmt.Println("usage: astonish platform restore <archive> [--dry-run|--confirm] [options]")
	fmt.Println("")
	fmt.Println("Recover a clean platform installation from a logical backup archive.")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  --dry-run                 Validate and preview restore without writing")
	fmt.Println("  --confirm                 Execute restore; required for writes")
	fmt.Println("  --reset-target            Delete and recreate a non-empty SQLite target before restore")
	fmt.Println("  --yes                     Required with --reset-target to acknowledge destructive reset")
	fmt.Println("  --enable-scheduled-jobs   Restore scheduled jobs as active instead of paused")
	fmt.Println("  --include-transient       Restore login/runtime transient tables")
	fmt.Println("  --json                    Print JSON output")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  astonish platform restore backup.astonish-backup --dry-run")
	fmt.Println("  astonish platform restore backup.astonish-backup --confirm")
}
