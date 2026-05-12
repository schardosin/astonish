package filestore

import (
	"context"
	"fmt"

	"github.com/schardosin/astonish/pkg/skills"
	"github.com/schardosin/astonish/pkg/store"
)

// SkillStoreWrapper wraps the existing skills package functions behind the
// store.SkillStore interface.
type SkillStoreWrapper struct {
	userDir   string
	extraDirs []string
	allowlist []string
}

// NewSkillStore creates a SkillStore backed by the existing file-based skill loader.
func NewSkillStore(userDir string, extraDirs []string, allowlist []string) store.SkillStore {
	return &SkillStoreWrapper{
		userDir:   userDir,
		extraDirs: extraDirs,
		allowlist: allowlist,
	}
}

func (w *SkillStoreWrapper) LoadAll(_ context.Context) ([]store.Skill, error) {
	loaded, err := skills.LoadSkills(w.userDir, w.extraDirs, "", w.allowlist)
	if err != nil {
		return nil, err
	}
	return convertSkills(loaded), nil
}

func (w *SkillStoreWrapper) Get(_ context.Context, name string) (*store.Skill, error) {
	loaded, err := skills.LoadSkills(w.userDir, w.extraDirs, "", w.allowlist)
	if err != nil {
		return nil, err
	}
	for _, s := range loaded {
		if s.Name == name {
			cs := convertSkill(s)
			return &cs, nil
		}
	}
	return nil, fmt.Errorf("skill %q not found", name)
}

func (w *SkillStoreWrapper) Save(_ context.Context, _ *store.Skill) error {
	// File-based skill saving is handled directly by the skills API handlers.
	// This will be implemented properly in the PG store.
	return fmt.Errorf("save not implemented in file store; use skills API handlers directly")
}

func (w *SkillStoreWrapper) Delete(_ context.Context, _ string) error {
	// File-based skill deletion is handled directly by the skills API handlers.
	return fmt.Errorf("delete not implemented in file store; use skills API handlers directly")
}

func (w *SkillStoreWrapper) List(_ context.Context) ([]store.Skill, error) {
	return w.LoadAll(context.Background())
}

func convertSkills(in []skills.Skill) []store.Skill {
	out := make([]store.Skill, len(in))
	for i, s := range in {
		out[i] = convertSkill(s)
	}
	return out
}

func convertSkill(s skills.Skill) store.Skill {
	return store.Skill{
		Name:        s.Name,
		Description: s.Description,
		OS:          s.OS,
		RequireBins: s.RequireBins,
		RequireEnv:  s.RequireEnv,
		Metadata:    s.Metadata,
		Content:     s.Content,
		FilePath:    s.FilePath,
		Source:      s.Source,
		Directory:   s.Directory,
	}
}

// Compile-time check.
var _ store.SkillStore = (*SkillStoreWrapper)(nil)
