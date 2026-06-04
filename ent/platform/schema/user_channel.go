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

// UserChannel holds the schema for the user_channels table.
type UserChannel struct {
	ent.Schema
}

func (UserChannel) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.UUID("user_id", uuid.UUID{}),
		field.String("channel_type").
			NotEmpty(),
		field.String("external_id").
			NotEmpty(),
		field.String("display_name").
			Default(""),
		field.Bool("enabled").
			Default(true),
		field.Bool("verified").
			Default(false),
		field.Time("verified_at").
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
	}
}

func (UserChannel) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("channels").
			Field("user_id").
			Required().
			Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (UserChannel) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("channel_type", "external_id").Unique(),
		index.Fields("user_id"),
	}
}

func (UserChannel) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("user_channels"),
	}
}
