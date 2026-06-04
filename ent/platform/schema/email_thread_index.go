package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// EmailThreadIndex holds the schema for the email_thread_index table.
type EmailThreadIndex struct {
	ent.Schema
}

func (EmailThreadIndex) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			StorageKey("message_id").
			NotEmpty(),
		field.String("session_key").
			NotEmpty(),
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

func (EmailThreadIndex) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_key"),
	}
}

func (EmailThreadIndex) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("email_thread_index"),
	}
}
