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

// PlatformNetworkPolicy holds the schema for platform-wide network policy rules.
type PlatformNetworkPolicy struct {
	ent.Schema
}

func (PlatformNetworkPolicy) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("host").
			NotEmpty().
			Comment("Host pattern: exact, *.example.com, or **.example.com"),
		field.Uint32("port").
			Default(443).
			Comment("Port number; 0 means any port"),
		field.Enum("action").
			Values("allow", "deny").
			Default("allow"),
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

func (PlatformNetworkPolicy) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("host", "port").Unique(),
	}
}

func (PlatformNetworkPolicy) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("platform_network_policies"),
	}
}
