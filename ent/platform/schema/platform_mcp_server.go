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

// PlatformMCPServer holds the schema for the platform_mcp_servers table.
type PlatformMCPServer struct {
	ent.Schema
}

func (PlatformMCPServer) Fields() []ent.Field {
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
		field.JSON("env", map[string]string{}).
			Optional().
			Default(map[string]string{}),
		field.String("transport").
			Default("stdio"),
		field.String("url").
			Optional().
			Nillable(),
		field.Bool("enabled").
			Default(true),
		field.JSON("cached_tools", []any{}).
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

func (PlatformMCPServer) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique(),
	}
}

func (PlatformMCPServer) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("platform_mcp_servers"),
	}
}
