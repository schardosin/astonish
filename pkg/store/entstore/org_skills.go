package entstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	orgent "github.com/SAP/astonish/ent/org"
	"github.com/SAP/astonish/ent/org/orgskill"
	"github.com/SAP/astonish/ent/org/orgskillfile"
	"github.com/SAP/astonish/pkg/store"
)

// orgSkillStore implements store.SkillStore for org-level skills.
type orgSkillStore struct {
	client *orgent.Client
}

var _ store.SkillStore = (*orgSkillStore)(nil)

func (ss *orgSkillStore) LoadAll(ctx context.Context) ([]store.Skill, error) {
	return ss.List(ctx)
}

func (ss *orgSkillStore) Get(ctx context.Context, name string) (*store.Skill, error) {
	ent, err := ss.client.OrgSkill.Query().
		Where(orgskill.NameEQ(name)).
		WithFiles().
		Only(ctx)
	if err != nil {
		if orgent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	s := entOrgSkillToStore(ent)
	return &s, nil
}

func (ss *orgSkillStore) Save(ctx context.Context, skill *store.Skill) error {
	// Parse created_by.
	var createdBy uuid.UUID
	if skill.CreatedBy != "" {
		uid, err := uuid.Parse(skill.CreatedBy)
		if err == nil {
			createdBy = uid
		}
	}

	// Parse frontmatter.
	var frontmatter map[string]any
	if skill.Metadata != nil {
		if data, err := json.Marshal(skill.Metadata); err == nil {
			_ = json.Unmarshal(data, &frontmatter)
		}
	}

	// Check if skill exists.
	existing, err := ss.client.OrgSkill.Query().
		Where(orgskill.NameEQ(skill.Name)).
		Only(ctx)
	if err != nil && !orgent.IsNotFound(err) {
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
	create := ss.client.OrgSkill.Create().
		SetName(skill.Name).
		SetContent(skill.Content).
		SetCreatedBy(createdBy)
	if frontmatter != nil {
		create.SetFrontmatter(frontmatter)
	}
	if skill.ValidationStatus != "" {
		create.SetValidationStatus(skill.ValidationStatus)
	}

	_, err = create.Save(ctx)
	return err
}

func (ss *orgSkillStore) Delete(ctx context.Context, name string) error {
	_, err := ss.client.OrgSkill.Delete().
		Where(orgskill.NameEQ(name)).
		Exec(ctx)
	return err
}

func (ss *orgSkillStore) List(ctx context.Context) ([]store.Skill, error) {
	ents, err := ss.client.OrgSkill.Query().
		Order(orgskill.ByName()).
		All(ctx)
	if err != nil {
		return nil, err
	}

	skills := make([]store.Skill, len(ents))
	for i, e := range ents {
		skills[i] = entOrgSkillToStore(e)
	}
	return skills, nil
}

func (ss *orgSkillStore) UpdateValidationStatus(ctx context.Context, name, status, meta string) error {
	update := ss.client.OrgSkill.Update().
		Where(orgskill.NameEQ(name)).
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

func (ss *orgSkillStore) ListFiles(ctx context.Context, skillName string) ([]store.SkillFile, error) {
	skill, err := ss.client.OrgSkill.Query().
		Where(orgskill.NameEQ(skillName)).
		Only(ctx)
	if err != nil {
		if orgent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	ents, err := ss.client.OrgSkillFile.Query().
		Where(orgskillfile.SkillIDEQ(skill.ID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	files := make([]store.SkillFile, len(ents))
	for i, e := range ents {
		files[i] = entOrgSkillFileToStore(e)
	}
	return files, nil
}

func (ss *orgSkillStore) GetFile(ctx context.Context, skillName, path, filename string) (*store.SkillFile, error) {
	skill, err := ss.client.OrgSkill.Query().
		Where(orgskill.NameEQ(skillName)).
		Only(ctx)
	if err != nil {
		if orgent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	ent, err := ss.client.OrgSkillFile.Query().
		Where(
			orgskillfile.SkillIDEQ(skill.ID),
			orgskillfile.PathEQ(path),
			orgskillfile.FilenameEQ(filename),
		).
		Only(ctx)
	if err != nil {
		if orgent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	f := entOrgSkillFileToStore(ent)
	return &f, nil
}

func (ss *orgSkillStore) SaveFile(ctx context.Context, skillName string, file *store.SkillFile) error {
	skill, err := ss.client.OrgSkill.Query().
		Where(orgskill.NameEQ(skillName)).
		Only(ctx)
	if err != nil {
		return fmt.Errorf("skill %q not found: %w", skillName, err)
	}

	// Check if file exists.
	existing, err := ss.client.OrgSkillFile.Query().
		Where(
			orgskillfile.SkillIDEQ(skill.ID),
			orgskillfile.PathEQ(file.Path),
			orgskillfile.FilenameEQ(file.Filename),
		).
		Only(ctx)
	if err != nil && !orgent.IsNotFound(err) {
		return err
	}

	if existing != nil {
		return existing.Update().
			SetContent(file.Content).
			SetIsExecutable(file.IsExecutable).
			SetSizeBytes(file.SizeBytes).
			Exec(ctx)
	}

	_, err = ss.client.OrgSkillFile.Create().
		SetSkillID(skill.ID).
		SetPath(file.Path).
		SetFilename(file.Filename).
		SetContent(file.Content).
		SetIsExecutable(file.IsExecutable).
		SetSizeBytes(file.SizeBytes).
		Save(ctx)
	return err
}

func (ss *orgSkillStore) DeleteFile(ctx context.Context, skillName, path, filename string) error {
	skill, err := ss.client.OrgSkill.Query().
		Where(orgskill.NameEQ(skillName)).
		Only(ctx)
	if err != nil {
		if orgent.IsNotFound(err) {
			return nil
		}
		return err
	}

	_, err = ss.client.OrgSkillFile.Delete().
		Where(
			orgskillfile.SkillIDEQ(skill.ID),
			orgskillfile.PathEQ(path),
			orgskillfile.FilenameEQ(filename),
		).
		Exec(ctx)
	return err
}

func entOrgSkillToStore(e *orgent.OrgSkill) store.Skill {
	s := store.Skill{
		Name:             e.Name,
		Content:          e.Content,
		CreatedBy:        e.CreatedBy.String(),
		ValidationStatus: e.ValidationStatus,
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

func entOrgSkillFileToStore(e *orgent.OrgSkillFile) store.SkillFile {
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
