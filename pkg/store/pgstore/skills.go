package pgstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// pgSkillStore implements store.SkillStore for PostgreSQL.
type pgSkillStore struct {
	pool   *pgxpool.Pool
	schema string
	table  string // "org_skills" for org-level
}

func (s *pgSkillStore) tableName() string {
	return pgx.Identifier{s.schema, s.table}.Sanitize()
}

func (s *pgSkillStore) LoadAll() ([]store.Skill, error) {
	return s.List()
}

func (s *pgSkillStore) Get(name string) (*store.Skill, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT name, content, frontmatter FROM %s WHERE name = $1`, s.tableName()),
		name,
	)

	var skill store.Skill
	var frontmatterJSON []byte
	err := row.Scan(&skill.Name, &skill.Content, &frontmatterJSON)
	if err != nil {
		return nil, fmt.Errorf("skill not found: %w", err)
	}
	if len(frontmatterJSON) > 0 {
		_ = json.Unmarshal(frontmatterJSON, &skill.Metadata)
	}
	return &skill, nil
}

func (s *pgSkillStore) Save(skill *store.Skill) error {
	ctx := context.Background()
	frontmatterJSON, _ := json.Marshal(skill.Metadata)

	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (name, content, frontmatter, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (name) DO UPDATE SET content = $2, frontmatter = $3, updated_at = now()`,
		s.tableName()),
		skill.Name, skill.Content, frontmatterJSON,
	)
	return err
}

func (s *pgSkillStore) Delete(name string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE name = $1`, s.tableName()),
		name,
	)
	return err
}

func (s *pgSkillStore) List() ([]store.Skill, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT name, content, frontmatter FROM %s ORDER BY name`, s.tableName()),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var skills []store.Skill
	for rows.Next() {
		var skill store.Skill
		var frontmatterJSON []byte
		if err := rows.Scan(&skill.Name, &skill.Content, &frontmatterJSON); err != nil {
			return nil, err
		}
		if len(frontmatterJSON) > 0 {
			_ = json.Unmarshal(frontmatterJSON, &skill.Metadata)
		}
		skills = append(skills, skill)
	}
	return skills, rows.Err()
}
