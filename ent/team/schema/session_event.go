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
)

// SessionEvent holds the schema definition for the SessionEvent entity.
type SessionEvent struct {
	ent.Schema
}

func (SessionEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("session_id").
			NotEmpty(),
		field.JSON("event_data", map[string]any{}),
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

func (SessionEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", Session.Type).
			Ref("events").
			Field("session_id").
			Required().
			Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (SessionEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id"),
	}
}

func (SessionEvent) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("session_events"),
	}
}
