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

// PendingLinkCode holds the schema for the pending_link_codes table.
type PendingLinkCode struct {
	ent.Schema
}

func (PendingLinkCode) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			StorageKey("code").
			NotEmpty(),
		field.UUID("user_id", uuid.UUID{}),
		field.String("email").
			Optional().
			Nillable(),
		field.String("channel").
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
		field.Time("expires_at"),
	}
}

func (PendingLinkCode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "channel"),
		index.Fields("expires_at"),
	}
}

func (PendingLinkCode) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("pending_link_codes"),
	}
}
