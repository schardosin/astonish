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
)

// SandboxLayer holds the schema for the sandbox_layers table.
type SandboxLayer struct {
	ent.Schema
}

func (SandboxLayer) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			StorageKey("layer_id").
			NotEmpty(),
		field.String("parent_layer").
			Optional().
			Nillable(),
		field.String("cephfs_path").
			NotEmpty(),
		field.Int64("size_bytes").
			Default(0),
		field.Int("ref_count").
			Default(0),
		field.String("created_by").
			Optional().
			Nillable(),
		field.Time("added_at").
			Default(time.Now).
			Immutable().
			Annotations(&entsql.Annotation{
				DefaultExprs: map[string]string{
					dialect.Postgres: "now()",
					dialect.SQLite:   "(datetime('now'))",
				},
			}),
		field.Time("last_referenced").
			Default(time.Now).
			Annotations(&entsql.Annotation{
				DefaultExprs: map[string]string{
					dialect.Postgres: "now()",
					dialect.SQLite:   "(datetime('now'))",
				},
			}),
	}
}

func (SandboxLayer) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("templates", SandboxTemplate.Type),
	}
}

func (SandboxLayer) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("parent_layer"),
	}
}

func (SandboxLayer) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("sandbox_layers"),
	}
}
