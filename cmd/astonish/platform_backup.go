package astonish

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SAP/astonish/pkg/backup"
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/store/entstore"
)

func handlePlatformBackupCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printPlatformBackupUsage()
		return nil
	}

	switch args[0] {
	case "create":
		return handlePlatformBackupCreate(args[1:])
	case "inspect":
		return handlePlatformBackupInspect(args[1:])
	case "verify":
		return handlePlatformBackupVerify(args[1:])
	default:
		printPlatformBackupUsage()
		return fmt.Errorf("unknown platform backup subcommand: %s", args[0])
	}
}

func handlePlatformBackupCreate(args []string) error {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		printPlatformBackupCreateUsage()
		return nil
	}
	opts, err := parseBackupCreateArgs(args)
	if err != nil {
		return err
	}
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	ctx := context.Background()
	entCfg, backend, err := backupEntstoreConfig(appCfg)
	if err != nil {
		return err
	}
	_, es, err := entstore.NewPlatformServices(ctx, entCfg)
	if err != nil {
		return fmt.Errorf("failed to connect to platform store: %w", err)
	}
	defer es.Close()

	if err := es.ExportPlatformBackup(ctx, opts.output, entstore.PlatformBackupExportOptions{Backend: backend, Compression: opts.compression, RedactSecrets: opts.redactSecrets}); err != nil {
		return err
	}
	fmt.Printf("Created backup archive: %s\n", opts.output)
	return nil
}

func handlePlatformBackupInspect(args []string) error {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		printPlatformBackupInspectUsage()
		return nil
	}
	archivePath, jsonOut, err := parseBackupReadArgs(args)
	if err != nil {
		return err
	}
	summary, err := backup.Inspect(archivePath)
	if err != nil {
		return err
	}
	return printBackupSummary(summary, jsonOut, false)
}

func handlePlatformBackupVerify(args []string) error {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		printPlatformBackupVerifyUsage()
		return nil
	}
	archivePath, jsonOut, err := parseBackupReadArgs(args)
	if err != nil {
		return err
	}
	summary, err := backup.Verify(archivePath)
	if err != nil {
		return err
	}
	return printBackupSummary(summary, jsonOut, true)
}

type backupCreateOptions struct {
	output        string
	compression   backup.Compression
	redactSecrets bool
}

func parseBackupCreateArgs(args []string) (backupCreateOptions, error) {
	var opts backupCreateOptions
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--output", "-o":
			values := args[i+1:]
			if len(values) == 0 {
				return opts, fmt.Errorf("%s requires a value", args[i])
			}
			opts.output = values[0]
			i++
		case "--compression":
			values := args[i+1:]
			if len(values) == 0 {
				return opts, fmt.Errorf("%s requires a value", args[i])
			}
			compression, err := backup.ParseCompression(values[0])
			if err != nil {
				return opts, err
			}
			opts.compression = compression
			i++
		case "--redact-secrets":
			opts.redactSecrets = true
		case "--org", "--team", "--user":
			return opts, fmt.Errorf("%s scoped backups are not implemented yet; this slice supports platform metadata export", args[i])
		case "--physical":
			return opts, fmt.Errorf("physical backups are not implemented yet")
		case "-h", "--help":
			return opts, fmt.Errorf("--help must be the only argument")
		default:
			return opts, fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	if opts.output == "" {
		return opts, fmt.Errorf("--output is required")
	}
	if opts.compression == "" {
		opts.compression = backup.CompressionGzip
	}
	return opts, nil
}

func backupEntstoreConfig(appCfg *config.AppConfig) (entstore.Config, string, error) {
	backend := appCfg.Storage.Backend
	if backend == "" || backend == "file" {
		backend = "sqlite"
	}
	switch backend {
	case "postgres":
		return entstore.Config{
			DSN:             appCfg.Storage.Postgres.GetPlatformDSN(),
			InstanceSuffix:  appCfg.Storage.Postgres.InstanceSuffix,
			MaxOpenConns:    appCfg.Storage.Postgres.GetMaxOpenConns(),
			MaxIdleConns:    appCfg.Storage.Postgres.GetMaxIdleConns(),
			ConnMaxLifetime: appCfg.Storage.Postgres.GetConnMaxLifetime(),
		}, "postgres", nil
	case "sqlite":
		dataDir := appCfg.Storage.SQLite.GetDataDir()
		return entstore.Config{
			DSN:     "file:" + filepath.Join(dataDir, "platform.db"),
			DataDir: dataDir,
		}, "sqlite", nil
	default:
		return entstore.Config{}, "", fmt.Errorf("platform backup create requires storage.backend: sqlite or postgres, got %q", appCfg.Storage.Backend)
	}
}

func parseBackupReadArgs(args []string) (string, bool, error) {
	var archivePath string
	var jsonOut bool
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			return "", false, fmt.Errorf("--help must be the only argument")
		case "--json":
			jsonOut = true
		default:
			if archivePath != "" {
				return "", false, fmt.Errorf("unexpected argument: %s", args[i])
			}
			archivePath = args[i]
		}
	}
	if archivePath == "" {
		return "", false, fmt.Errorf("backup archive path is required")
	}
	return archivePath, jsonOut, nil
}

func printBackupSummary(summary backup.Summary, jsonOut bool, verified bool) error {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}
	if verified {
		fmt.Println("Backup archive verified successfully.")
	}
	manifest := summary.Manifest
	fmt.Printf("Format: %s v%d\n", manifest.Format, manifest.FormatVersion)
	fmt.Printf("Created: %s\n", manifest.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Printf("Backend: %s\n", manifest.Backend)
	fmt.Printf("Mode: %s\n", manifest.Mode)
	if manifest.Compression != "" {
		fmt.Printf("Compression: %s\n", manifest.Compression)
	}
	fmt.Printf("Scopes: %d\n", len(manifest.Scopes))
	for _, scope := range manifest.Scopes {
		fmt.Printf("  - %s", scope.Kind)
		if scope.OrgSlug != "" {
			fmt.Printf(" org=%s", scope.OrgSlug)
		}
		if scope.TeamSlug != "" {
			fmt.Printf(" team=%s", scope.TeamSlug)
		}
		if scope.UserID != "" {
			fmt.Printf(" user=%s", scope.UserID)
		}
		fmt.Println()
	}
	fmt.Printf("Entries: %d\n", len(manifest.Entries))
	for _, entry := range manifest.Entries {
		fmt.Printf("  - %s (%s", entry.Path, entry.Kind)
		if entry.Entity != "" {
			fmt.Printf(", %s", entry.Entity)
		}
		if entry.Records > 0 {
			fmt.Printf(", %d records", entry.Records)
		}
		if entry.Redacted {
			fmt.Print(", redacted")
		}
		fmt.Println(")")
	}
	fmt.Printf("Checksums: %d\n", len(summary.Checksums))
	return nil
}

func printPlatformBackupUsage() {
	fmt.Println("usage: astonish platform backup <command> [options]")
	fmt.Println("")
	fmt.Println("Create, inspect, and verify Astonish backup archives.")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  create --output <archive>    Export platform data to a compressed logical backup archive")
	fmt.Println("  inspect <archive> [--json]   Show backup archive metadata")
	fmt.Println("  verify <archive> [--json]    Validate archive manifest and checksums")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  astonish platform backup create --output backup.astonish-backup")
	fmt.Println("  astonish platform backup inspect backup.astonish-backup")
	fmt.Println("  astonish platform backup verify backup.astonish-backup")
}

func printPlatformBackupCreateUsage() {
	fmt.Println("usage: astonish platform backup create --output <archive> [--compression gzip|none] [--redact-secrets]")
	fmt.Println("")
	fmt.Println("Create a logical recovery backup archive containing exported platform data.")
	fmt.Println("Archives are gzip-compressed by default and contain multiple JSONL files plus manifest and checksum files.")
	fmt.Println("Use --redact-secrets only for portable/support exports that do not need full recovery of protected values.")
	fmt.Println("Restore and scoped backup selection are planned follow-up work.")
}

func printPlatformBackupInspectUsage() {
	fmt.Println("usage: astonish platform backup inspect <archive> [--json]")
	fmt.Println("")
	fmt.Println("Show manifest metadata for an Astonish backup archive.")
}

func printPlatformBackupVerifyUsage() {
	fmt.Println("usage: astonish platform backup verify <archive> [--json]")
	fmt.Println("")
	fmt.Println("Validate manifest metadata and SHA-256 checksums for an Astonish backup archive.")
}
