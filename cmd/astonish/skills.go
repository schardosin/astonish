package astonish

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/skills"
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
	appCfg, _ := config.LoadAppConfig()
	var skillsCfg config.SkillsConfig
	if appCfg != nil {
		skillsCfg = appCfg.Skills
	}

	workDir, _ := os.Getwd()
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
	appCfg, _ := config.LoadAppConfig()
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

	template := fmt.Sprintf(`---
name: %s
description: "TODO: One-line description of what this skill does"
require_bins: []
---

# %s

## When to Use
- TODO: Describe when this skill should be used

## When NOT to Use
- TODO: Describe when other tools are more appropriate

## Common Commands
`+"```"+`
# TODO: Add common commands and patterns
`+"```"+`

## Tips
- TODO: Add tips and best practices
`, name, name)

	if err := os.WriteFile(skillFile, []byte(template), 0644); err != nil {
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

	appCfg, _ := config.LoadAppConfig()
	var skillsCfg config.SkillsConfig
	if appCfg != nil {
		skillsCfg = appCfg.Skills
	}

	destDir := skillsCfg.GetUserSkillsDir()
	if destDir == "" {
		return fmt.Errorf("could not determine user skills directory")
	}

	// Check if skill already exists
	existingDir := fmt.Sprintf("%s/%s", destDir, slug)
	if _, err := os.Stat(existingDir); err == nil {
		// Read existing _meta.json for version comparison
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

	// Check eligibility of the installed skill
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

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
