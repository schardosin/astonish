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

// ChatSessionEvent holds the schema definition for the ChatSessionEvent entity.
// Logical composite key: (chat_session_id, seq) enforced via unique index.
type ChatSessionEvent struct {
	ent.Schema
}

func (ChatSessionEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("chat_session_id").
			NotEmpty(),
		field.Int64("seq"),
		field.String("event_type").
			NotEmpty(),
		field.JSON("payload", map[string]any{}),
		field.String("producer_pod").
			Default(""),
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

func (ChatSessionEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", Session.Type).
			Ref("chat_events").
			Field("chat_session_id").
			Required().
			Unique(),
	}
}

func (ChatSessionEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("chat_session_id", "seq").Unique(),
		index.Fields("chat_session_id", "created_at"),
	}
}

func (ChatSessionEvent) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("chat_session_events"),
	}
}
