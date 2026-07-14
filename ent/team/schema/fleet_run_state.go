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

// FleetRunState holds durable per-session fleet runtime state for parallel/mailbox recovery.
type FleetRunState struct{ ent.Schema }

func (FleetRunState) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("session_id").NotEmpty(),
		field.String("plan_key").NotEmpty(),
		field.String("state").NotEmpty(), // "idle" | "processing" | "waiting_for_customer" | "stopped"
		field.JSON("active_agents", []string{}).Default([]string{}),
		field.String("waiting_agent").Optional().Nillable(),
		field.String("ball").Default("agents"), // "agents" | "customer"
		field.JSON("progress", map[string]any{}).Default(map[string]any{}),
		field.Time("last_heartbeat_at").Default(time.Now),
		field.Time("created_at").Default(time.Now).Immutable().Annotations(
			&entsql.Annotation{DefaultExprs: map[string]string{
				dialect.Postgres: "now()",
				dialect.SQLite:   "(datetime('now'))",
			}},
		),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).Annotations(
			&entsql.Annotation{DefaultExprs: map[string]string{
				dialect.Postgres: "now()",
				dialect.SQLite:   "(datetime('now'))",
			}},
		),
	}
}

func (FleetRunState) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id").Unique(),
		index.Fields("plan_key", "state"),
	}
}

func (FleetRunState) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("fleet_run_states")}
}
