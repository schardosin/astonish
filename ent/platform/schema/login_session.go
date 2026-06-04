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

// LoginSession holds the schema for the login_sessions table.
type LoginSession struct {
	ent.Schema
}

func (LoginSession) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			StorageKey("token_hash").
			NotEmpty(),
		field.UUID("user_id", uuid.UUID{}),
		field.UUID("org_id", uuid.UUID{}),
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			Annotations(&entsql.Annotation{
				DefaultExprs: map[string]string{
					dialect.Postgres: "now()",
					dialect.SQLite:   "(datetime('now'))",
				},
			}),
		field.Time("expires_at"),
		field.String("user_agent").
			Optional().
			Nillable(),
		field.String("ip_address").
			Optional().
			Nillable(),
	}
}

func (LoginSession) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("login_sessions").
			Field("user_id").
			Required().
			Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.From("organization", Organization.Type).
			Ref("login_sessions").
			Field("org_id").
			Required().
			Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (LoginSession) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("expires_at"),
	}
}

func (LoginSession) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("login_sessions"),
	}
}
