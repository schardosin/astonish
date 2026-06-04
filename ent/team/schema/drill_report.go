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

// DrillReport holds the schema definition for the DrillReport entity.
type DrillReport struct {
	ent.Schema
}

func (DrillReport) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("suite").
			NotEmpty(),
		field.String("status").
			NotEmpty(),
		field.String("summary").
			Default(""),
		field.Int64("duration_ms").
			Default(0),
		field.JSON("report_data", map[string]any{}),
		field.Time("started_at"),
		field.Time("finished_at"),
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
	}
}

func (DrillReport) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("suite"),
		index.Fields("created_at"),
	}
}

func (DrillReport) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("drill_reports"),
	}
}
