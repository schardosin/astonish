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

// FleetTemplate holds the schema definition for the FleetTemplate entity.
type FleetTemplate struct {
	ent.Schema
}

func (FleetTemplate) Fields() []ent.Field {
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

func (FleetTemplate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("key").Unique(),
	}
}

func (FleetTemplate) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("fleet_templates"),
	}
}
