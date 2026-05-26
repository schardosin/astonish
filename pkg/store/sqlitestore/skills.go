package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/schardosin/astonish/pkg/skills"
	"github.com/schardosin/astonish/pkg/store"
)

// sqliteSkillStore implements store.SkillStore.
type sqliteSkillStore struct {
	db    *sql.DB
	table string // "skills" or "org_skills"
}

func (s *sqliteSkillStore) LoadAll(ctx context.Context) ([]store.Skill, error) {
	return s.List(ctx)
}

func (s *sqliteSkillStore) Get(ctx context.Context, name string) (*store.Skill, error) {
	row := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT name, content FROM %s WHERE name = ?`, s.table), name)

	var dbName, rawContent string
	err := row.Scan(&dbName, &rawContent)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseStoredSkillSQLite(dbName, rawContent), nil
}

func (s *sqliteSkillStore) Save(ctx context.Context, skill *store.Skill) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (id, name, content, frontmatter, created_by, created_at, updated_at)
		 VALUES (hex(randomblob(16)), ?, ?, '{}', ?, datetime('now'), datetime('now'))
		 ON CONFLICT(name) DO UPDATE SET content = excluded.content, updated_at = datetime('now')`, s.table),
		skill.Name, skill.Content, nilStr(skill.CreatedBy))
	return err
}

func (s *sqliteSkillStore) Delete(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE name = ?`, s.table), name)
	return err
}

func (s *sqliteSkillStore) List(ctx context.Context) ([]store.Skill, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT name, content FROM %s ORDER BY name`, s.table))
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
		result = append(result, *parseStoredSkillSQLite(dbName, rawContent))
	}
	return result, rows.Err()
}

// parseStoredSkillSQLite re-parses the raw SKILL.md content stored in SQLite
// to populate all structured fields. Falls back to a minimal Skill
// with just the name and raw content if parsing fails.
func parseStoredSkillSQLite(name, rawContent string) *store.Skill {
	parsed, err := skills.ParseSkillFile("sqlite:"+name, []byte(rawContent))
	if err != nil {
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
		Content:     rawContent,
		Source:      "custom",
	}
}
