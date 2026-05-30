package pgstore

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/skills"
	"github.com/schardosin/astonish/pkg/store"
)

// pgSkillStore implements store.SkillStore for PostgreSQL.
//
// The `content` column stores the full raw SKILL.md file (YAML frontmatter + body).
// On load, it is re-parsed via skills.ParseSkillFile to populate all structured
// fields (Description, OS, RequireBins, RequireEnv, etc.).
// The `frontmatter` JSONB column is maintained as a search/filter index but
// is not used as the source of truth.
type pgSkillStore struct {
	pool       *pgxpool.Pool
	schema     string
	table      string // "skills" or "org_skills"
	filesTable string // "skill_files" or "org_skill_files"
}

func (s *pgSkillStore) tableName() string {
	return pgx.Identifier{s.schema, s.table}.Sanitize()
}

func (s *pgSkillStore) filesTableName() string {
	if s.schema == "public" {
		// org level
		return pgx.Identifier{"public", s.filesTable}.Sanitize()
	}
	return pgx.Identifier{s.schema, s.filesTable}.Sanitize()
}

func (s *pgSkillStore) LoadAll(ctx context.Context) ([]store.Skill, error) {
	return s.List(ctx)
}

func (s *pgSkillStore) Get(ctx context.Context, name string) (*store.Skill, error) {
	row := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT name, content FROM %s WHERE name = $1`, s.tableName()),
		name,
	)

	var dbName, rawContent string
	if err := row.Scan(&dbName, &rawContent); err != nil {
		return nil, fmt.Errorf("skill not found: %w", err)
	}

	return parseStoredSkill(dbName, rawContent), nil
}

func (s *pgSkillStore) Save(ctx context.Context, skill *store.Skill) error {
	// content holds the full raw SKILL.md (frontmatter + body).
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (name, content, frontmatter, created_by, updated_at)
		 VALUES ($1, $2, '{}'::jsonb, $3, now())
		 ON CONFLICT (name) DO UPDATE SET content = $2, updated_at = now()`,
		s.tableName()),
		skill.Name, skill.Content, skill.CreatedBy,
	)
	return err
}

func (s *pgSkillStore) Delete(ctx context.Context, name string) error {
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE name = $1`, s.tableName()),
		name,
	)
	return err
}

func (s *pgSkillStore) List(ctx context.Context) ([]store.Skill, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT name, content FROM %s ORDER BY name`, s.tableName()),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.Skill
	for rows.Next() {
		var dbName, rawContent string
		if err := rows.Scan(&dbName, &rawContent); err != nil {
			return nil, err
		}
		result = append(result, *parseStoredSkill(dbName, rawContent))
	}
	return result, rows.Err()
}

// parseStoredSkill re-parses the raw SKILL.md content stored in PG
// to populate all structured fields. Falls back to a minimal Skill
// with just the name and raw content if parsing fails.
func parseStoredSkill(name, rawContent string) *store.Skill {
	parsed, err := skills.ParseSkillFile("pg:"+name, []byte(rawContent))
	if err != nil {
		// Content may be legacy (body-only from before the fix).
		// Return what we have.
		return &store.Skill{
			Name:    name,
			Content: rawContent,
			Source:  "custom",
		}
	}
	return &store.Skill{
		Name:        parsed.Name,
		Description: parsed.Description,
		OS:          parsed.OS,
		RequireBins: parsed.RequireBins,
		RequireEnv:  parsed.RequireEnv,
		Metadata:    parsed.Metadata,
		Content:     rawContent, // Keep full raw file for reconstructRawFile
		Source:      "custom",
	}
}

// --- Multi-file support methods ---

func (s *pgSkillStore) ListFiles(ctx context.Context, skillName string) ([]store.SkillFile, error) {
	// Resolve skill ID
	var skillID string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT id FROM %s WHERE name = $1`, s.tableName()), skillName).Scan(&skillID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

 	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
 		SELECT id, skill_id, path, filename, content, is_executable, size_bytes, created_at::text, updated_at::text
 		FROM %s
 		WHERE skill_id = $1
 		ORDER BY path, filename`, s.filesTableName()), skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []store.SkillFile
	for rows.Next() {
		var f store.SkillFile
		if err := rows.Scan(&f.ID, &f.SkillID, &f.Path, &f.Filename, &f.Content, &f.IsExecutable, &f.SizeBytes, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (s *pgSkillStore) GetFile(ctx context.Context, skillName, path, filename string) (*store.SkillFile, error) {
	var skillID string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT id FROM %s WHERE name = $1`, s.tableName()), skillName).Scan(&skillID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

 	row := s.pool.QueryRow(ctx, fmt.Sprintf(`
 		SELECT id, skill_id, path, filename, content, is_executable, size_bytes, created_at::text, updated_at::text
 		FROM %s
 		WHERE skill_id = $1 AND path = $2 AND filename = $3`, s.filesTableName()),
 		skillID, path, filename)

	var f store.SkillFile
	if err := row.Scan(&f.ID, &f.SkillID, &f.Path, &f.Filename, &f.Content, &f.IsExecutable, &f.SizeBytes, &f.CreatedAt, &f.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &f, nil
}

func (s *pgSkillStore) SaveFile(ctx context.Context, skillName string, file *store.SkillFile) error {
	var skillID string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT id FROM %s WHERE name = $1`, s.tableName()), skillName).Scan(&skillID)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (skill_id, path, filename, content, is_executable, size_bytes, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now(), now())
		ON CONFLICT (skill_id, path, filename) DO UPDATE SET
			content = EXCLUDED.content,
			is_executable = EXCLUDED.is_executable,
			size_bytes = EXCLUDED.size_bytes,
			updated_at = now()`,
		s.filesTableName()),
		skillID, file.Path, file.Filename, file.Content, file.IsExecutable, file.SizeBytes)
	return err
}

func (s *pgSkillStore) DeleteFile(ctx context.Context, skillName, path, filename string) error {
	var skillID string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT id FROM %s WHERE name = $1`, s.tableName()), skillName).Scan(&skillID)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, fmt.Sprintf(`
		DELETE FROM %s
		WHERE skill_id = $1 AND path = $2 AND filename = $3`,
		s.filesTableName()),
		skillID, path, filename)
	return err
}
