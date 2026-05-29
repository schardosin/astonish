package astonish

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/skills"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
	"github.com/schardosin/astonish/pkg/store/sqlitestore"
)

func handleSkillsCommand(args []string) error {
	if len(args) == 0 {
		return handleSkillsList()
	}

	switch args[0] {
	case "list":
		return handleSkillsList()
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish skills show <name>")
		}
		return handleSkillsShow(args[1])
	case "check":
		return handleSkillsCheck()
	case "create":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish skills create <name>")
		}
		return handleSkillsCreate(args[1])
	case "install":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish skills install <slug|url|clawhub:slug>")
		}
		return handleSkillsInstall(args[1])
	default:
		fmt.Println("usage: astonish skills {list,show,check,create,install}")
		fmt.Println("")
		fmt.Println("  list              List all available skills")
		fmt.Println("  show <name>       Show full skill content")
		fmt.Println("  check             Check which skills are eligible")
		fmt.Println("  create <name>     Create a new skill from template")
		fmt.Println("  install <input>   Install a skill from ClawHub")
		fmt.Println("                    Accepts: slug, clawhub:slug, or full URL")
		return fmt.Errorf("unknown skills command: %s", args[0])
	}
}

func loadAllSkills() ([]skills.Skill, error) {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		slog.Warn("failed to load app config", "error", err)
	}
	var skillsCfg config.SkillsConfig
	if appCfg != nil {
		skillsCfg = appCfg.Skills
	}

	workDir, wdErr := os.Getwd()
	if wdErr != nil {
		slog.Warn("failed to get working directory", "error", wdErr)
	}
	return skills.LoadSkills(
		skillsCfg.GetUserSkillsDir(),
		skillsCfg.ExtraDirs,
		workDir,
		skillsCfg.Allowlist,
	)
}

func handleSkillsList() error {
	allSkills, err := loadAllSkills()
	if err != nil {
		return fmt.Errorf("failed to load skills: %w", err)
	}

	if len(allSkills) == 0 {
		fmt.Println("No skills found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tDESCRIPTION\tSOURCE\tSTATUS")

	eligible := 0
	for _, s := range allSkills {
		status := "eligible"
		if !s.IsEligible() {
			missing := s.MissingRequirements()
			status = fmt.Sprintf("missing: %s", strings.Join(missing, ", "))
		} else {
			eligible++
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, truncate(s.Description, 55), s.Source, status)
	}
	w.Flush()

	fmt.Printf("\n%d eligible, %d total\n", eligible, len(allSkills))
	return nil
}

func handleSkillsShow(name string) error {
	allSkills, err := loadAllSkills()
	if err != nil {
		return fmt.Errorf("failed to load skills: %w", err)
	}

	for _, s := range allSkills {
		if strings.EqualFold(s.Name, name) {
			fmt.Printf("# %s\n\n", s.Name)
			fmt.Printf("Description: %s\n", s.Description)
			fmt.Printf("Source: %s\n", s.Source)
			if len(s.OS) > 0 {
				fmt.Printf("OS: %s\n", strings.Join(s.OS, ", "))
			}
			if len(s.RequireBins) > 0 {
				fmt.Printf("Requires: %s\n", strings.Join(s.RequireBins, ", "))
			}
			if len(s.RequireEnv) > 0 {
				fmt.Printf("Env: %s\n", strings.Join(s.RequireEnv, ", "))
			}
			fmt.Printf("Eligible: %v\n", s.IsEligible())
			fmt.Printf("\n---\n\n%s\n", s.Content)
			return nil
		}
	}

	return fmt.Errorf("skill %q not found", name)
}

func handleSkillsCheck() error {
	allSkills, err := loadAllSkills()
	if err != nil {
		return fmt.Errorf("failed to load skills: %w", err)
	}

	if len(allSkills) == 0 {
		fmt.Println("No skills found.")
		return nil
	}

	eligible := 0
	ineligible := 0
	for _, s := range allSkills {
		if s.IsEligible() {
			fmt.Printf("  ✓ %s\n", s.Name)
			eligible++
		} else {
			missing := s.MissingRequirements()
			fmt.Printf("  ✗ %s (missing: %s)\n", s.Name, strings.Join(missing, ", "))
			ineligible++
		}
	}

	fmt.Printf("\n%d eligible, %d ineligible\n", eligible, ineligible)
	return nil
}

func handleSkillsCreate(name string) error {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		slog.Warn("failed to load app config", "error", err)
	}
	var skillsCfg config.SkillsConfig
	if appCfg != nil {
		skillsCfg = appCfg.Skills
	}

	userDir := skillsCfg.GetUserSkillsDir()
	if userDir == "" {
		return fmt.Errorf("could not determine user skills directory")
	}

	skillDir := fmt.Sprintf("%s/%s", userDir, name)
	skillFile := fmt.Sprintf("%s/SKILL.md", skillDir)

	if _, err := os.Stat(skillFile); err == nil {
		return fmt.Errorf("skill %q already exists at %s", name, skillFile)
	}

	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(skillFile, []byte(skills.NewSkillTemplate(name)), 0644); err != nil {
		return fmt.Errorf("failed to write skill file: %w", err)
	}

	fmt.Printf("Created skill template: %s\n", skillFile)
	fmt.Println("Edit the SKILL.md file to add your skill content.")
	return nil
}

func handleSkillsInstall(input string) error {
	slug, err := skills.ParseClawHubInput(input)
	if err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}

	appCfg, err := config.LoadAppConfig()
	if err != nil {
		slog.Warn("failed to load app config", "error", err)
	}

	// Detect platform mode
	isPlatform := false
	var platformStore store.SkillStore
	var platformOrgStore store.SkillStore

	if appCfg != nil && (appCfg.Storage.Backend == "sqlite" || appCfg.Storage.Backend == "postgres") {
		isPlatform = true

		ctx := context.Background()
		if appCfg.Storage.Backend == "sqlite" {
			dataDir := appCfg.Storage.SQLite.GetDataDir()
			if dataDir == "" {
				home, _ := os.UserHomeDir()
				dataDir = home + "/.config/astonish/data"
			}
			svc, _, err := sqlitestore.NewPlatformServices(ctx, dataDir)
			if err != nil {
				return fmt.Errorf("failed to open platform SQLite: %w", err)
			}
			// Note: we intentionally do not close the stores here
			if svc != nil {
				platformOrgStore = svc.Skills
				// For CLI we prefer team if available, but for global install we use org for simplicity
				platformStore = svc.TeamSkills
				if platformStore == nil {
					platformStore = platformOrgStore
				}
			}
		} else if appCfg.Storage.Backend == "postgres" {
			svc, _, err := pgstore.NewPlatformServices(ctx, appCfg.Storage.Postgres)
			if err != nil {
				return fmt.Errorf("failed to open platform Postgres: %w", err)
			}
			if svc != nil {
				platformOrgStore = svc.Skills
				platformStore = svc.TeamSkills
				if platformStore == nil {
					platformStore = platformOrgStore
				}
			}
		}
	}

	if isPlatform {
		fmt.Printf("Platform mode detected — installing into database...\n")
		return installToPlatformStores(slug, platformStore, platformOrgStore)
	}

	// === Personal mode (existing behavior) ===
	var skillsCfg config.SkillsConfig
	if appCfg != nil {
		skillsCfg = appCfg.Skills
	}

	destDir := skillsCfg.GetUserSkillsDir()
	if destDir == "" {
		return fmt.Errorf("could not determine user skills directory")
	}

	existingDir := fmt.Sprintf("%s/%s", destDir, slug)
	if _, err := os.Stat(existingDir); err == nil {
		if meta, err := skills.ReadClawHubMeta(existingDir); err == nil {
			fmt.Printf("Skill %q already installed (version: %s)\n", slug, meta.Version)
		} else {
			fmt.Printf("Skill %q already exists at %s\n", slug, existingDir)
		}
		fmt.Println("Reinstalling (overwriting existing files)...")
	}

	fmt.Printf("Downloading %q from ClawHub...\n", slug)

	result, err := skills.DownloadFromClawHub(slug, destDir)
	if err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	fmt.Printf("\nInstalled: %s\n", result.Name)
	if result.Version != "" {
		fmt.Printf("Version:   %s\n", result.Version)
	}
	fmt.Printf("Files:     %d\n", result.FilesCount)
	fmt.Printf("Location:  %s\n", result.SkillDir)

	if result.FilesCount > 0 {
		fmt.Println("\nFiles:")
		for _, f := range result.Files {
			fmt.Printf("  %s\n", f)
		}
	}

	skillPath := fmt.Sprintf("%s/SKILL.md", result.SkillDir)
	if data, err := os.ReadFile(skillPath); err == nil {
		if skill, err := skills.ParseSkillFile(skillPath, data); err == nil {
			if skill.IsEligible() {
				fmt.Printf("\nStatus: eligible\n")
			} else {
				missing := skill.MissingRequirements()
				fmt.Printf("\nStatus: ineligible (missing: %s)\n", strings.Join(missing, ", "))
			}
		}
	}

	return nil
}

// installToPlatformStores installs a ClawHub skill into the platform database stores.
func installToPlatformStores(slug string, teamStore, orgStore store.SkillStore) error {
	// For platform installs we use a temp dir just for download
	tmpDir, err := os.MkdirTemp("", "astonish-clawhub-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	fmt.Printf("Downloading %q from ClawHub...\n", slug)

	result, err := skills.DownloadFromClawHub(slug, tmpDir)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Read SKILL.md
	skillPath := fmt.Sprintf("%s/%s/SKILL.md", tmpDir, slug)
	skillData, err := os.ReadFile(skillPath)
	if err != nil {
		return fmt.Errorf("read SKILL.md: %w", err)
	}

	parsed, err := skills.ParseSkillFile(skillPath, skillData)
	if err != nil {
		return fmt.Errorf("parse SKILL.md: %w", err)
	}

	// Choose target store (prefer team if available)
	targetStore := teamStore
	scope := "team"
	if targetStore == nil {
		targetStore = orgStore
		scope = "org"
	}
	if targetStore == nil {
		return fmt.Errorf("no platform skill store available")
	}

	// Save main skill
	skill := &store.Skill{
		Name:        parsed.Name,
		Description: parsed.Description,
		Content:     string(skillData),
		OS:          parsed.OS,
		RequireBins: parsed.RequireBins,
		RequireEnv:  parsed.RequireEnv,
		Metadata:    parsed.Metadata,
	}

	if err := targetStore.Save(context.Background(), skill); err != nil {
		return fmt.Errorf("save skill to platform: %w", err)
	}

	// Save auxiliary files
	filesSaved := 0
	skillDir := fmt.Sprintf("%s/%s", tmpDir, slug)

	err = filepath.Walk(skillDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		rel, _ := filepath.Rel(skillDir, path)
		rel = filepath.ToSlash(rel)

		if rel == "SKILL.md" || rel == "_meta.json" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		dir := filepath.Dir(rel)
		if dir == "." {
			dir = ""
		}
		fname := filepath.Base(rel)

		sf := &store.SkillFile{
			Path:         dir,
			Filename:     fname,
			Content:      string(content),
			IsExecutable: info.Mode().Perm()&0111 != 0,
			SizeBytes:    info.Size(),
		}

		if err := targetStore.SaveFile(context.Background(), parsed.Name, sf); err != nil {
			slog.Warn("failed to save auxiliary file", "file", rel, "error", err)
			return nil
		}
		filesSaved++
		return nil
	})
	if err != nil {
		slog.Warn("error walking skill files", "error", err)
	}

	fmt.Printf("\nInstalled to platform (%s scope): %s\n", scope, parsed.Name)
	if result.Version != "" {
		fmt.Printf("Version: %s\n", result.Version)
	}
	fmt.Printf("Main skill + %d auxiliary files saved to database.\n", filesSaved)

	// Eligibility check
	if parsed.IsEligible() {
		fmt.Println("Status: eligible")
	} else {
		fmt.Printf("Status: ineligible (missing: %s)\n", strings.Join(parsed.MissingRequirements(), ", "))
	}

	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
