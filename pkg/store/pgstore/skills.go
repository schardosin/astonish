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
	pool   *pgxpool.Pool
	schema string
	table  string // "org_skills" for org-level
}

func (s *pgSkillStore) tableName() string {
	return pgx.Identifier{s.schema, s.table}.Sanitize()
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
