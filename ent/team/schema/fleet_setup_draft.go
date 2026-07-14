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

// FleetSetupDraft holds in-progress plan setup collected values.
type FleetSetupDraft struct {
	ent.Schema
}

func (FleetSetupDraft) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("template_key").
			NotEmpty(),
		field.String("setup_profile_key").
			NotEmpty(),
		field.JSON("collected", map[string]any{}),
		field.String("current_step").
			Optional(),
		field.UUID("created_by", uuid.UUID{}).
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

func (FleetSetupDraft) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at"),
	}
}

func (FleetSetupDraft) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("fleet_setup_drafts"),
	}
}
