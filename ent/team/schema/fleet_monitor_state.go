package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// FleetMonitorState holds the schema definition for the FleetMonitorState entity.
type FleetMonitorState struct {
	ent.Schema
}

func (FleetMonitorState) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			StorageKey("plan_key").
			NotEmpty(),
		field.JSON("state", map[string]any{}).
			Default(map[string]any{}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			Annotations(&entsql.Annotation{
				DefaultExprs: map[string]string{
					dialect.Postgres: "now()",
					dialect.SQLite:   "(datetime('now'))",
				},
			}),
	}
}

func (FleetMonitorState) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("fleet_monitor_state"),
	}
}
