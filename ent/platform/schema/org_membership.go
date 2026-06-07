package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// OrgMembership holds the schema for the org_memberships table.
// This is an edge schema with composite PK: (user_id, org_id).
type OrgMembership struct {
	ent.Schema
}

func (OrgMembership) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("user_id", uuid.UUID{}),
		field.UUID("org_id", uuid.UUID{}),
		field.Enum("role").
			Values("owner", "admin", "member").
			Default("member"),
		field.Time("joined_at").
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

func (OrgMembership) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("user", User.Type).
			Required().
			Unique().
			Field("user_id"),
		edge.To("organization", Organization.Type).
			Required().
			Unique().
			Field("org_id"),
	}
}

func (OrgMembership) Annotations() []schema.Annotation {
	return []schema.Annotation{
		field.ID("org_id", "user_id"),
		entsql.Table("org_memberships"),
	}
}
