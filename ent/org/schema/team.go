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

// Team holds the schema definition for the Team entity.
type Team struct {
	ent.Schema
}

func (Team) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("name").
			NotEmpty(),
		field.String("slug").
			NotEmpty(),
		field.String("schema_name").
			NotEmpty(),
		field.String("test_field").
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
		field.JSON("settings", map[string]any{}).
			Optional().
			Default(map[string]any{}),
	}
}

func (Team) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("memberships", TeamMembership.Type),
	}
}

func (Team) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("slug").Unique(),
		index.Fields("schema_name").Unique(),
	}
}

func (Team) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("teams"),
	}
}
