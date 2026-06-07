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

// TeamMembership holds the schema for the team_memberships table.
// Logical composite key: (user_id, team_id) enforced via unique index.
type TeamMembership struct {
	ent.Schema
}

func (TeamMembership) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.UUID("user_id", uuid.UUID{}),
		field.UUID("team_id", uuid.UUID{}),
		field.Enum("role").
			Values("admin", "member", "viewer").
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

func (TeamMembership) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("team", Team.Type).
			Ref("memberships").
			Field("team_id").
			Required().
			Unique(),
	}
}

func (TeamMembership) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "team_id").Unique(),
		index.Fields("team_id"),
		index.Fields("user_id"),
	}
}

func (TeamMembership) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("team_memberships"),
	}
}
