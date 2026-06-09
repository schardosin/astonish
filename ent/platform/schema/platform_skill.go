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

// PlatformSkill holds the schema definition for the PlatformSkill entity.
type PlatformSkill struct {
	ent.Schema
}

func (PlatformSkill) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("name").
			NotEmpty(),
		field.String("content").
			NotEmpty(),
		field.JSON("frontmatter", map[string]any{}).
			Optional(),
		field.String("validation_status").
			Default("unknown"),
		field.JSON("validation_meta", map[string]any{}).
			Optional(),
		field.String("created_by").
			Optional().
			Nillable(),
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

func (PlatformSkill) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("files", PlatformSkillFile.Type),
	}
}

func (PlatformSkill) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique(),
	}
}

func (PlatformSkill) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("platform_skills"),
	}
}
