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

// FleetSetupProfile holds reusable fleet plan setup definitions.
type FleetSetupProfile struct {
	ent.Schema
}

func (FleetSetupProfile) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("key").
			NotEmpty(),
		field.String("name").
			NotEmpty(),
		field.JSON("definition", map[string]any{}),
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

func (FleetSetupProfile) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("key").Unique(),
	}
}

func (FleetSetupProfile) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("fleet_setup_profiles"),
	}
}
