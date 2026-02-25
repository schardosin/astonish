package skills

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const clawHubDownloadURL = "https://wry-manatee-359.convex.site/api/v1/download"

// InstallResult contains the result of a ClawHub skill installation.
type InstallResult struct {
	Name       string   // Skill name from SKILL.md frontmatter
	Slug       string   // ClawHub slug used for download
	Version    string   // Version from _meta.json
	FilesCount int      // Number of files extracted
	Files      []string // List of extracted file names
	SkillDir   string   // Absolute path to the installed skill directory
}

// ClawHubMeta represents the _meta.json file in a ClawHub download.
type ClawHubMeta struct {
	OwnerID     string `json:"ownerId"`
	Slug        string `json:"slug"`
	Version     string `json:"version"`
	PublishedAt string `json:"publishedAt"`
}

// ParseClawHubInput parses various input formats into a ClawHub slug.
// Accepts:
//   - Full URL: https://clawhub.ai/steipete/github → github
//   - Shorthand: clawhub:github → github
//   - Bare slug: github → github
func ParseClawHubInput(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("empty input")
	}

	// Full URL: https://clawhub.ai/{owner}/{slug}
	if strings.HasPrefix(input, "https://clawhub.ai/") || strings.HasPrefix(input, "http://clawhub.ai/") {
		u, err := url.Parse(input)
		if err != nil {
			return "", fmt.Errorf("invalid URL: %w", err)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 {
			return "", fmt.Errorf("invalid ClawHub URL: expected https://clawhub.ai/{owner}/{slug}")
		}
		slug := parts[len(parts)-1]
		if slug == "" {
			return "", fmt.Errorf("invalid ClawHub URL: empty slug")
		}
		return slug, nil
	}

	// Shorthand: clawhub:slug
	if strings.HasPrefix(input, "clawhub:") {
		slug := strings.TrimPrefix(input, "clawhub:")
		slug = strings.TrimSpace(slug)
		if slug == "" {
			return "", fmt.Errorf("empty slug in shorthand")
		}
		return slug, nil
	}

	// Bare slug — validate it looks reasonable (no spaces, slashes only for owner/slug)
	if strings.Contains(input, " ") {
		return "", fmt.Errorf("invalid slug: contains spaces")
	}

	// If it contains a slash, treat as owner/slug and extract slug part
	if strings.Contains(input, "/") {
		parts := strings.Split(input, "/")
		slug := parts[len(parts)-1]
		if slug == "" {
			return "", fmt.Errorf("invalid input: empty slug after slash")
		}
		return slug, nil
	}

	return input, nil
}

// DownloadFromClawHub downloads a skill from ClawHub and extracts it to destDir/{slug}/.
func DownloadFromClawHub(slug string, destDir string) (*InstallResult, error) {
	// Build download URL
	downloadURL := fmt.Sprintf("%s?slug=%s", clawHubDownloadURL, url.QueryEscape(slug))

	resp, err := http.Get(downloadURL) //nolint:gosec // URL is constructed from constant base + user slug
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("skill %q not found on ClawHub", slug)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ClawHub returned HTTP %d", resp.StatusCode)
	}

	// Read the zip into memory
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Open zip archive
	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("invalid zip archive: %w", err)
	}

	// Create destination directory
	skillDir := filepath.Join(destDir, slug)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return nil, fmt.Errorf("create skill dir: %w", err)
	}

	result := &InstallResult{
		Slug:     slug,
		SkillDir: skillDir,
	}

	// Extract all files
	for _, f := range zipReader.File {
		if f.FileInfo().IsDir() {
			continue
		}

		// Sanitize the filename — strip any leading directory components from zip
		name := filepath.Base(f.Name)
		if name == "" || name == "." || name == ".." {
			continue
		}

		outPath := filepath.Join(skillDir, name)
		if err := extractZipFile(f, outPath); err != nil {
			return nil, fmt.Errorf("extract %s: %w", name, err)
		}

		result.Files = append(result.Files, name)
		result.FilesCount++
	}

	// Parse _meta.json for version info
	metaPath := filepath.Join(skillDir, "_meta.json")
	if metaData, err := os.ReadFile(metaPath); err == nil {
		var meta ClawHubMeta
		if err := json.Unmarshal(metaData, &meta); err == nil {
			result.Version = meta.Version
		}
	}

	// Parse SKILL.md for the skill name
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if skillData, err := os.ReadFile(skillPath); err == nil {
		if skill, err := ParseSkillFile(skillPath, skillData); err == nil {
			result.Name = skill.Name
		}
	}

	if result.Name == "" {
		result.Name = slug
	}

	return result, nil
}

// ReadClawHubMeta reads the _meta.json from an existing skill directory.
func ReadClawHubMeta(skillDir string) (*ClawHubMeta, error) {
	metaPath := filepath.Join(skillDir, "_meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}
	var meta ClawHubMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse _meta.json: %w", err)
	}
	return &meta, nil
}

// extractZipFile extracts a single file from a zip archive to the destination path.
func extractZipFile(f *zip.File, destPath string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc) //nolint:gosec // zip file sizes are bounded by HTTP response
	return err
}
