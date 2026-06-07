package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// OrgMemory holds the schema definition for the OrgMemory entity.
type OrgMemory struct {
	ent.Schema
}

func (OrgMemory) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("chunk_text").
			NotEmpty(),
		field.Bytes("embedding").
			Optional().
			SchemaType(map[string]string{
				dialect.Postgres: "vector(384)",
			}),
		field.String("tsv").
			Optional().
			Nillable().
			SchemaType(map[string]string{
				dialect.Postgres: "tsvector",
				dialect.SQLite:   "text",
			}),
		field.String("category").
			Optional().
			Nillable(),
		field.String("source_path").
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]any{}).
			Optional(),
		field.UUID("promoted_by", uuid.UUID{}),
		field.String("promoted_from_team").
			Optional().
			Nillable(),
		field.UUID("session_id", uuid.UUID{}).
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
	}
}

func (OrgMemory) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id"),
	}
}

func (OrgMemory) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("org_memories"),
	}
}
