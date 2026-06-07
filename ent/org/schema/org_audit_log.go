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

// OrgAuditLog holds the schema definition for the OrgAuditLog entity.
type OrgAuditLog struct {
	ent.Schema
}

func (OrgAuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.Time("timestamp").
			Default(time.Now).
			Immutable().
			Annotations(&entsql.Annotation{
				DefaultExprs: map[string]string{
					dialect.Postgres: "now()",
					dialect.SQLite:   "(datetime('now'))",
				},
			}),
		field.UUID("user_id", uuid.UUID{}),
		field.String("team_id").
			Optional().
			Nillable(),
		field.String("action").
			NotEmpty(),
		field.String("resource").
			NotEmpty(),
		field.JSON("detail", map[string]any{}).
			Optional(),
		field.String("ip_address").
			Optional().
			Nillable().
			SchemaType(map[string]string{
				dialect.Postgres: "inet",
				dialect.SQLite:   "text",
			}),
		field.String("session_id").
			Optional().
			Nillable(),
	}
}

func (OrgAuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("timestamp"),
		index.Fields("action"),
	}
}

func (OrgAuditLog) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("org_audit_log"),
	}
}
