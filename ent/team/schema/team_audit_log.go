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

// TeamAuditLog holds the schema definition for the TeamAuditLog entity.
type TeamAuditLog struct {
	ent.Schema
}

func (TeamAuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.Time("timestamp").
			Default(time.Now).
			Annotations(&entsql.Annotation{
				DefaultExprs: map[string]string{
					dialect.Postgres: "now()",
					dialect.SQLite:   "(datetime('now'))",
				},
			}),
		field.UUID("user_id", uuid.UUID{}),
		field.String("action").
			NotEmpty(),
		field.String("resource").
			NotEmpty(),
		field.JSON("detail", map[string]any{}).
			Optional(),
		field.String("session_id").
			Optional().
			Nillable(),
	}
}

func (TeamAuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("timestamp"),
	}
}

func (TeamAuditLog) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("team_audit_log"),
	}
}
