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

// Organization holds the schema definition for the Organization entity.
type Organization struct {
	ent.Schema
}

func (Organization) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("name").
			NotEmpty(),
		field.String("slug").
			NotEmpty(),
		field.String("db_name").
			Default(""),
		field.Enum("status").
			Values("active", "suspended", "decommissioned").
			Default("active"),
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			Annotations(&entsql.Annotation{
				DefaultExprs: map[string]string{
					dialect.Postgres: "now()",
					dialect.SQLite:   "(datetime('now'))",
				},
			}),
		field.JSON("settings", map[string]any{}).
			Optional(),
	}
}

func (Organization) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("users", User.Type).
			Through("memberships", OrgMembership.Type),
		edge.To("oidc_providers", OIDCProvider.Type),
		edge.To("login_sessions", LoginSession.Type),
	}
}

func (Organization) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("slug").Unique(),
		index.Fields("db_name").Unique(),
	}
}

func (Organization) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("organizations"),
	}
}
