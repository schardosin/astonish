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

// McpServer holds the schema definition for the McpServer entity.
type McpServer struct {
	ent.Schema
}

func (McpServer) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("name").
			NotEmpty(),
		field.String("command").
			Optional().
			Nillable(),
		field.JSON("args", []string{}).
			Optional().
			Default([]string{}),
		field.JSON("env", map[string]any{}).
			Optional().
			Default(map[string]any{}),
		field.String("transport").
			Default("stdio"),
		field.String("url").
			Optional().
			Nillable(),
		field.Bool("enabled").
			Default(true),
		field.JSON("cached_tools", []any{}).
			Optional(),
		field.UUID("created_by", uuid.UUID{}),
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

func (McpServer) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique(),
	}
}

func (McpServer) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("mcp_servers"),
	}
}
