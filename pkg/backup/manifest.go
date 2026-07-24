package backup

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	ArchiveFormat        = "astonish.backup"
	ArchiveFormatVersion = 1
)

var ErrUnsupportedArchiveVersion = errors.New("unsupported backup archive version")

type Scope struct {
	Kind     string `json:"kind"`
	OrgSlug  string `json:"orgSlug,omitempty"`
	TeamSlug string `json:"teamSlug,omitempty"`
	UserID   string `json:"userId,omitempty"`
}

type SchemaVersion struct {
	Scope      string   `json:"scope"`
	Version    string   `json:"version,omitempty"`
	Migrations []string `json:"migrations,omitempty"`
	Hash       string   `json:"hash,omitempty"`
}

type Entry struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Scope    Scope  `json:"scope"`
	Entity   string `json:"entity,omitempty"`
	Records  int64  `json:"records,omitempty"`
	Redacted bool   `json:"redacted,omitempty"`
}

type Manifest struct {
	Format         string                   `json:"format"`
	FormatVersion  int                      `json:"formatVersion"`
	CreatedAt      time.Time                `json:"createdAt"`
	Astonish       VersionInfo              `json:"astonish"`
	Backend        string                   `json:"backend"`
	Mode           string                   `json:"mode"`
	Compression    string                   `json:"compression,omitempty"`
	Scopes         []Scope                  `json:"scopes"`
	SchemaVersions map[string]SchemaVersion `json:"schemaVersions,omitempty"`
	Features       []string                 `json:"features,omitempty"`
	Entries        []Entry                  `json:"entries,omitempty"`
}

type VersionInfo struct {
	Version   string `json:"version,omitempty"`
	GitCommit string `json:"gitCommit,omitempty"`
}

type TargetCompatibility struct {
	FormatVersion  int
	SchemaVersions map[string]string
}

func NewManifest(backend, mode string, scopes []Scope) Manifest {
	return Manifest{
		Format:        ArchiveFormat,
		FormatVersion: ArchiveFormatVersion,
		CreatedAt:     time.Now().UTC(),
		Backend:       backend,
		Mode:          mode,
		Scopes:        scopes,
	}
}

func (m Manifest) Validate() error {
	if m.Format != ArchiveFormat {
		return fmt.Errorf("invalid backup format %q", m.Format)
	}
	if m.FormatVersion != ArchiveFormatVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedArchiveVersion, m.FormatVersion, ArchiveFormatVersion)
	}
	if m.CreatedAt.IsZero() {
		return errors.New("manifest createdAt is required")
	}
	if strings.TrimSpace(m.Backend) == "" {
		return errors.New("manifest backend is required")
	}
	if strings.TrimSpace(m.Mode) == "" {
		return errors.New("manifest mode is required")
	}
	if strings.TrimSpace(m.Compression) != "" {
		if _, err := ParseCompression(m.Compression); err != nil {
			return err
		}
	}
	for _, scope := range m.Scopes {
		if err := scope.Validate(); err != nil {
			return err
		}
	}
	seen := make(map[string]struct{}, len(m.Entries))
	for _, entry := range m.Entries {
		if err := entry.Validate(); err != nil {
			return err
		}
		if _, ok := seen[entry.Path]; ok {
			return fmt.Errorf("duplicate manifest entry path %q", entry.Path)
		}
		seen[entry.Path] = struct{}{}
	}
	return nil
}

func (m Manifest) CheckCompatible(target TargetCompatibility) error {
	if target.FormatVersion != 0 && target.FormatVersion != m.FormatVersion {
		return fmt.Errorf("%w: archive format version %d cannot restore into target format version %d", ErrUnsupportedArchiveVersion, m.FormatVersion, target.FormatVersion)
	}
	for scope, archiveVersion := range m.SchemaVersions {
		targetVersion, ok := target.SchemaVersions[scope]
		if !ok {
			return fmt.Errorf("target schema version for scope %q is missing", scope)
		}
		if archiveVersion.Version != "" && targetVersion != "" && targetVersion < archiveVersion.Version {
			return fmt.Errorf("target schema version for scope %q (%s) is older than archive schema version %s", scope, targetVersion, archiveVersion.Version)
		}
	}
	return nil
}

func (s Scope) Validate() error {
	switch s.Kind {
	case "platform":
		return nil
	case "org":
		if s.OrgSlug == "" {
			return errors.New("org backup scope requires orgSlug")
		}
	case "team":
		if s.OrgSlug == "" || s.TeamSlug == "" {
			return errors.New("team backup scope requires orgSlug and teamSlug")
		}
	case "personal":
		if s.OrgSlug == "" || s.UserID == "" {
			return errors.New("personal backup scope requires orgSlug and userId")
		}
	default:
		return fmt.Errorf("unknown backup scope kind %q", s.Kind)
	}
	return nil
}

func (e Entry) Validate() error {
	if strings.TrimSpace(e.Path) == "" {
		return errors.New("manifest entry path is required")
	}
	if filepath.IsAbs(e.Path) || strings.Contains(e.Path, "..") || strings.Contains(e.Path, "\\") {
		return fmt.Errorf("manifest entry path %q must be a relative archive path", e.Path)
	}
	if strings.TrimSpace(e.Kind) == "" {
		return fmt.Errorf("manifest entry %q kind is required", e.Path)
	}
	if e.Records < 0 {
		return fmt.Errorf("manifest entry %q records cannot be negative", e.Path)
	}
	return e.Scope.Validate()
}
