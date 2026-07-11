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

// FleetMailboxMessage holds a durable per-recipient mailbox message for fleet handoffs.
type FleetMailboxMessage struct{ ent.Schema }

func (FleetMailboxMessage) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("session_id").NotEmpty(),
		field.String("recipient").NotEmpty(), // agent key OR "customer"
		field.String("sender").NotEmpty(),    // agent key, "customer", or "system"
		field.Text("body").Optional().Default(""),
		field.JSON("mentions", []string{}).Optional(),
		field.JSON("metadata", map[string]any{}).Optional(),
		field.String("delivery_status").Default("pending"), // "pending" | "delivered" | "read"
		field.Time("delivered_at").Optional().Nillable(),
		field.Time("read_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable().Annotations(
			&entsql.Annotation{DefaultExprs: map[string]string{
				dialect.Postgres: "now()",
				dialect.SQLite:   "(datetime('now'))",
			}},
		),
	}
}

func (FleetMailboxMessage) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "recipient", "delivery_status"),
		index.Fields("session_id", "created_at"),
	}
}

func (FleetMailboxMessage) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("fleet_mailbox_messages")}
}
