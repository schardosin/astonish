package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// SkillFile holds the schema definition for the SkillFile entity.
type SkillFile struct {
	ent.Schema
}

func (SkillFile) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.UUID("skill_id", uuid.UUID{}),
		field.String("path").
			Default(""),
		field.String("filename").
			NotEmpty(),
		field.String("content").
			NotEmpty(),
		field.Bool("is_executable").
			Default(false),
		field.Int64("size_bytes"),
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			Annotations(&entsql.Annotation{
				DefaultExprs: map[string]string{
					dialect.Postgres: "now()",
					dialect.SQLite:   "(datetime('now'))",
				},
			}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			Annotations(&entsql.Annotation{
				DefaultExprs: map[string]string{
					dialect.Postgres: "now()",
					dialect.SQLite:   "(datetime('now'))",
				},
			}),
	}
}

func (SkillFile) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("skill", Skill.Type).
			Ref("files").
			Field("skill_id").
			Required().
			Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (SkillFile) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("skill_id"),
		index.Fields("path"),
		index.Fields("skill_id", "path", "filename").Unique(),
	}
}

func (SkillFile) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("skill_files"),
	}
}
