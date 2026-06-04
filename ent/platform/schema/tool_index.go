package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// ToolIndex holds the schema for the tool_index table.
// The embedding column uses vector(384) on PG and BLOB on SQLite.
type ToolIndex struct {
	ent.Schema
}

func (ToolIndex) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			NotEmpty(),
		field.String("content").
			NotEmpty(),
		field.Bytes("embedding").
			Optional().
			SchemaType(map[string]string{
				dialect.Postgres: "vector(384)",
			}),
		field.JSON("metadata", map[string]any{}).
			Optional().
			Default(map[string]any{}),
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

func (ToolIndex) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("tool_index"),
	}
}
