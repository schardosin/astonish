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

// FleetTask holds a durable task-board entry for a fleet session.
type FleetTask struct{ ent.Schema }

func (FleetTask) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("session_id").NotEmpty(),
		field.String("title").NotEmpty(),
		field.Text("description").Optional().Default(""),
		field.JSON("required_capabilities", []string{}).Default([]string{}),
		field.String("claimed_by").Optional().Nillable(),
		field.String("status").Default("open"), // "open" | "claimed" | "in_progress" | "done" | "failed" | "cancelled"
		field.JSON("result", map[string]any{}).Optional(),
		field.String("parent_task_id").Optional().Nillable(),
		field.Time("claimed_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
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

func (FleetTask) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "status"),
		index.Fields("claimed_by"),
	}
}

func (FleetTask) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("fleet_tasks")}
}
