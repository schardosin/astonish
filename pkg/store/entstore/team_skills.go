package entstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	teament "github.com/schardosin/astonish/ent/team"
	"github.com/schardosin/astonish/ent/team/skill"
	"github.com/schardosin/astonish/ent/team/skillfile"
	"github.com/schardosin/astonish/pkg/store"
)

// teamSkillStore implements store.SkillStore using the Ent team client.
type teamSkillStore struct {
	client *teament.Client
}

var _ store.SkillStore = (*teamSkillStore)(nil)

func (s *teamSkillStore) LoadAll(ctx context.Context) ([]store.Skill, error) {
	skills, err := s.client.Skill.Query().
		Order(skill.ByName()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: SkillStore.LoadAll: %w", err)
	}
	return entSkillsToStore(skills), nil
}

func (s *teamSkillStore) Get(ctx context.Context, name string) (*store.Skill, error) {
	ent, err := s.client.Skill.Query().
		Where(skill.NameEQ(name)).
		Only(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return nil, fmt.Errorf("skill %q not found", name)
		}
		return nil, fmt.Errorf("entstore: SkillStore.Get: %w", err)
	}
	return entSkillToStore(ent), nil
}

func (s *teamSkillStore) Save(ctx context.Context, sk *store.Skill) error {
	frontmatter := map[string]any{}
	if sk.Description != "" {
		frontmatter["description"] = sk.Description
	}
	if len(sk.OS) > 0 {
		frontmatter["os"] = sk.OS
	}
	if len(sk.RequireBins) > 0 {
		frontmatter["require_bins"] = sk.RequireBins
	}
	if len(sk.RequireEnv) > 0 {
		frontmatter["require_env"] = sk.RequireEnv
	}
	if sk.Metadata != nil {
		frontmatter["metadata"] = sk.Metadata
	}

	createdBy := uuid.Nil
	if sk.CreatedBy != "" {
		if id, err := uuid.Parse(sk.CreatedBy); err == nil {
			createdBy = id
		}
	}

	// Try update first.
	n, err := s.client.Skill.Update().
		Where(skill.NameEQ(sk.Name)).
		SetContent(sk.Content).
		SetFrontmatter(frontmatter).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: SkillStore.Save: update: %w", err)
	}
	if n == 0 {
		_, err = s.client.Skill.Create().
			SetName(sk.Name).
			SetContent(sk.Content).
			SetFrontmatter(frontmatter).
			SetCreatedBy(createdBy).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("entstore: SkillStore.Save: create: %w", err)
		}
	}
	return nil
}

func (s *teamSkillStore) Delete(ctx context.Context, name string) error {
	_, err := s.client.Skill.Delete().
		Where(skill.NameEQ(name)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("entstore: SkillStore.Delete: %w", err)
	}
	return nil
}

func (s *teamSkillStore) List(ctx context.Context) ([]store.Skill, error) {
	return s.LoadAll(ctx)
}

func (s *teamSkillStore) UpdateValidationStatus(ctx context.Context, name, status, meta string) error {
	var validationMeta map[string]any
	if meta != "" {
		if err := json.Unmarshal([]byte(meta), &validationMeta); err != nil {
			validationMeta = map[string]any{"raw": meta}
		}
	}

	n, err := s.client.Skill.Update().
		Where(skill.NameEQ(name)).
		SetValidationStatus(status).
		SetValidationMeta(validationMeta).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: SkillStore.UpdateValidationStatus: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("skill %q not found", name)
	}
	return nil
}

func (s *teamSkillStore) ListFiles(ctx context.Context, skillName string) ([]store.SkillFile, error) {
	sk, err := s.client.Skill.Query().
		Where(skill.NameEQ(skillName)).
		WithFiles().
		Only(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return nil, fmt.Errorf("skill %q not found", skillName)
		}
		return nil, fmt.Errorf("entstore: SkillStore.ListFiles: %w", err)
	}

	files := make([]store.SkillFile, len(sk.Edges.Files))
	for i, f := range sk.Edges.Files {
		files[i] = entSkillFileToStore(f)
	}
	return files, nil
}

func (s *teamSkillStore) GetFile(ctx context.Context, skillName, path, filename string) (*store.SkillFile, error) {
	sk, err := s.client.Skill.Query().
		Where(skill.NameEQ(skillName)).
		Only(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return nil, fmt.Errorf("skill %q not found", skillName)
		}
		return nil, fmt.Errorf("entstore: SkillStore.GetFile: %w", err)
	}

	f, err := s.client.SkillFile.Query().
		Where(
			skillfile.SkillIDEQ(sk.ID),
			skillfile.PathEQ(path),
			skillfile.FilenameEQ(filename),
		).
		Only(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return nil, fmt.Errorf("skill file %q/%q not found", path, filename)
		}
		return nil, fmt.Errorf("entstore: SkillStore.GetFile: %w", err)
	}
	result := entSkillFileToStore(f)
	return &result, nil
}

func (s *teamSkillStore) SaveFile(ctx context.Context, skillName string, file *store.SkillFile) error {
	sk, err := s.client.Skill.Query().
		Where(skill.NameEQ(skillName)).
		Only(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return fmt.Errorf("skill %q not found", skillName)
		}
		return fmt.Errorf("entstore: SkillStore.SaveFile: %w", err)
	}

	// Try update existing file.
	n, err := s.client.SkillFile.Update().
		Where(
			skillfile.SkillIDEQ(sk.ID),
			skillfile.PathEQ(file.Path),
			skillfile.FilenameEQ(file.Filename),
		).
		SetContent(file.Content).
		SetIsExecutable(file.IsExecutable).
		SetSizeBytes(file.SizeBytes).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: SkillStore.SaveFile: update: %w", err)
	}
	if n == 0 {
		_, err = s.client.SkillFile.Create().
			SetSkillID(sk.ID).
			SetPath(file.Path).
			SetFilename(file.Filename).
			SetContent(file.Content).
			SetIsExecutable(file.IsExecutable).
			SetSizeBytes(file.SizeBytes).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("entstore: SkillStore.SaveFile: create: %w", err)
		}
	}
	return nil
}

func (s *teamSkillStore) DeleteFile(ctx context.Context, skillName, path, filename string) error {
	sk, err := s.client.Skill.Query().
		Where(skill.NameEQ(skillName)).
		Only(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return fmt.Errorf("skill %q not found", skillName)
		}
		return fmt.Errorf("entstore: SkillStore.DeleteFile: %w", err)
	}

	_, err = s.client.SkillFile.Delete().
		Where(
			skillfile.SkillIDEQ(sk.ID),
			skillfile.PathEQ(path),
			skillfile.FilenameEQ(filename),
		).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("entstore: SkillStore.DeleteFile: %w", err)
	}
	return nil
}

// --- Helpers ---

func entSkillsToStore(skills []*teament.Skill) []store.Skill {
	result := make([]store.Skill, len(skills))
	for i, sk := range skills {
		result[i] = *entSkillToStore(sk)
	}
	return result
}

func entSkillToStore(sk *teament.Skill) *store.Skill {
	s := &store.Skill{
		Name:             sk.Name,
		Content:          sk.Content,
		CreatedBy:        sk.CreatedBy.String(),
		ValidationStatus: sk.ValidationStatus,
		Source:           "db",
	}
	if sk.Frontmatter != nil {
		if desc, ok := sk.Frontmatter["description"].(string); ok {
			s.Description = desc
		}
		if os, ok := sk.Frontmatter["os"].([]any); ok {
			for _, v := range os {
				if str, ok := v.(string); ok {
					s.OS = append(s.OS, str)
				}
			}
		}
		if bins, ok := sk.Frontmatter["require_bins"].([]any); ok {
			for _, v := range bins {
				if str, ok := v.(string); ok {
					s.RequireBins = append(s.RequireBins, str)
				}
			}
		}
		if env, ok := sk.Frontmatter["require_env"].([]any); ok {
			for _, v := range env {
				if str, ok := v.(string); ok {
					s.RequireEnv = append(s.RequireEnv, str)
				}
			}
		}
		if meta, ok := sk.Frontmatter["metadata"]; ok {
			s.Metadata = meta
		}
	}
	if sk.ValidationMeta != nil {
		if raw, err := json.Marshal(sk.ValidationMeta); err == nil {
			s.ValidationMeta = string(raw)
		}
	}
	return s
}

func entSkillFileToStore(f *teament.SkillFile) store.SkillFile {
	return store.SkillFile{
		ID:           f.ID.String(),
		SkillID:      f.SkillID.String(),
		Path:         f.Path,
		Filename:     f.Filename,
		Content:      f.Content,
		IsExecutable: f.IsExecutable,
		SizeBytes:    f.SizeBytes,
		CreatedAt:    f.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:    f.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}
