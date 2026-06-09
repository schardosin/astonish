package entstore

import (
	"context"
	"encoding/json"
	"fmt"

	platforment "github.com/schardosin/astonish/ent/platform"
	"github.com/schardosin/astonish/ent/platform/platformskill"
	"github.com/schardosin/astonish/ent/platform/platformskillfile"
	"github.com/schardosin/astonish/pkg/store"
)

// platformSkillStore implements store.SkillStore for platform-level skills.
type platformSkillStore struct {
	client *platforment.Client
}

// PlatformSkills returns the platform-level SkillStore.
func (s *Store) PlatformSkills() store.SkillStore {
	return &platformSkillStore{client: s.platformClient}
}

// Compile-time assertion.
var _ store.SkillStore = (*platformSkillStore)(nil)

func (ps *platformSkillStore) LoadAll(ctx context.Context) ([]store.Skill, error) {
	return ps.List(ctx)
}

func (ps *platformSkillStore) Get(ctx context.Context, name string) (*store.Skill, error) {
	ent, err := ps.client.PlatformSkill.Query().
		Where(platformskill.NameEQ(name)).
		WithFiles().
		Only(ctx)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	s := entPlatformSkillToStore(ent)
	return &s, nil
}

func (ps *platformSkillStore) Save(ctx context.Context, skill *store.Skill) error {
	// Parse frontmatter.
	var frontmatter map[string]any
	if skill.Metadata != nil {
		if data, err := json.Marshal(skill.Metadata); err == nil {
			_ = json.Unmarshal(data, &frontmatter)
		}
	}

	// Check if skill exists.
	existing, err := ps.client.PlatformSkill.Query().
		Where(platformskill.NameEQ(skill.Name)).
		Only(ctx)
	if err != nil && !platforment.IsNotFound(err) {
		return err
	}

	if existing != nil {
		// Update.
		update := existing.Update().
			SetContent(skill.Content)
		if frontmatter != nil {
			update.SetFrontmatter(frontmatter)
		}
		return update.Exec(ctx)
	}

	// Create.
	create := ps.client.PlatformSkill.Create().
		SetName(skill.Name).
		SetContent(skill.Content)
	if frontmatter != nil {
		create.SetFrontmatter(frontmatter)
	}
	if skill.CreatedBy != "" {
		create.SetNillableCreatedBy(&skill.CreatedBy)
	}
	if skill.ValidationStatus != "" {
		create.SetValidationStatus(skill.ValidationStatus)
	}

	_, err = create.Save(ctx)
	return err
}

func (ps *platformSkillStore) Delete(ctx context.Context, name string) error {
	_, err := ps.client.PlatformSkill.Delete().
		Where(platformskill.NameEQ(name)).
		Exec(ctx)
	return err
}

func (ps *platformSkillStore) List(ctx context.Context) ([]store.Skill, error) {
	ents, err := ps.client.PlatformSkill.Query().
		Order(platformskill.ByName()).
		All(ctx)
	if err != nil {
		return nil, err
	}

	skills := make([]store.Skill, len(ents))
	for i, e := range ents {
		skills[i] = entPlatformSkillToStore(e)
	}
	return skills, nil
}

func (ps *platformSkillStore) UpdateValidationStatus(ctx context.Context, name, status, meta string) error {
	update := ps.client.PlatformSkill.Update().
		Where(platformskill.NameEQ(name)).
		SetValidationStatus(status)

	if meta != "" {
		var validationMeta map[string]any
		if err := json.Unmarshal([]byte(meta), &validationMeta); err == nil {
			update.SetValidationMeta(validationMeta)
		}
	}

	_, err := update.Save(ctx)
	return err
}

func (ps *platformSkillStore) ListFiles(ctx context.Context, skillName string) ([]store.SkillFile, error) {
	skill, err := ps.client.PlatformSkill.Query().
		Where(platformskill.NameEQ(skillName)).
		Only(ctx)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	ents, err := ps.client.PlatformSkillFile.Query().
		Where(platformskillfile.SkillIDEQ(skill.ID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	files := make([]store.SkillFile, len(ents))
	for i, e := range ents {
		files[i] = entPlatformSkillFileToStore(e)
	}
	return files, nil
}

func (ps *platformSkillStore) GetFile(ctx context.Context, skillName, path, filename string) (*store.SkillFile, error) {
	skill, err := ps.client.PlatformSkill.Query().
		Where(platformskill.NameEQ(skillName)).
		Only(ctx)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	ent, err := ps.client.PlatformSkillFile.Query().
		Where(
			platformskillfile.SkillIDEQ(skill.ID),
			platformskillfile.PathEQ(path),
			platformskillfile.FilenameEQ(filename),
		).
		Only(ctx)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	f := entPlatformSkillFileToStore(ent)
	return &f, nil
}

func (ps *platformSkillStore) SaveFile(ctx context.Context, skillName string, file *store.SkillFile) error {
	skill, err := ps.client.PlatformSkill.Query().
		Where(platformskill.NameEQ(skillName)).
		Only(ctx)
	if err != nil {
		return fmt.Errorf("skill %q not found: %w", skillName, err)
	}

	// Check if file exists.
	existing, err := ps.client.PlatformSkillFile.Query().
		Where(
			platformskillfile.SkillIDEQ(skill.ID),
			platformskillfile.PathEQ(file.Path),
			platformskillfile.FilenameEQ(file.Filename),
		).
		Only(ctx)
	if err != nil && !platforment.IsNotFound(err) {
		return err
	}

	if existing != nil {
		return existing.Update().
			SetContent(file.Content).
			SetIsExecutable(file.IsExecutable).
			SetSizeBytes(file.SizeBytes).
			Exec(ctx)
	}

	_, err = ps.client.PlatformSkillFile.Create().
		SetSkillID(skill.ID).
		SetPath(file.Path).
		SetFilename(file.Filename).
		SetContent(file.Content).
		SetIsExecutable(file.IsExecutable).
		SetSizeBytes(file.SizeBytes).
		Save(ctx)
	return err
}

func (ps *platformSkillStore) DeleteFile(ctx context.Context, skillName, path, filename string) error {
	skill, err := ps.client.PlatformSkill.Query().
		Where(platformskill.NameEQ(skillName)).
		Only(ctx)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil
		}
		return err
	}

	_, err = ps.client.PlatformSkillFile.Delete().
		Where(
			platformskillfile.SkillIDEQ(skill.ID),
			platformskillfile.PathEQ(path),
			platformskillfile.FilenameEQ(filename),
		).
		Exec(ctx)
	return err
}

func entPlatformSkillToStore(e *platforment.PlatformSkill) store.Skill {
	s := store.Skill{
		Name:             e.Name,
		Content:          e.Content,
		ValidationStatus: e.ValidationStatus,
	}
	if e.CreatedBy != nil {
		s.CreatedBy = *e.CreatedBy
	}
	if e.Frontmatter != nil {
		s.Metadata = e.Frontmatter
	}
	if e.ValidationMeta != nil {
		if data, err := json.Marshal(e.ValidationMeta); err == nil {
			s.ValidationMeta = string(data)
		}
	}
	return s
}

func entPlatformSkillFileToStore(e *platforment.PlatformSkillFile) store.SkillFile {
	return store.SkillFile{
		ID:           e.ID.String(),
		SkillID:      e.SkillID.String(),
		Path:         e.Path,
		Filename:     e.Filename,
		Content:      e.Content,
		IsExecutable: e.IsExecutable,
		SizeBytes:    e.SizeBytes,
		CreatedAt:    e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:    e.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
