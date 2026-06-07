package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DeviceSession holds the schema for the device_sessions table.
type DeviceSession struct {
	ent.Schema
}

func (DeviceSession) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			StorageKey("device_code").
			NotEmpty(),
		field.String("state").
			NotEmpty(),
		field.String("nonce").
			NotEmpty(),
		field.String("provider_id").
			NotEmpty(),
		field.String("client_type").
			Default("cli"),
		field.Enum("status").
			Values("pending", "complete", "expired", "error").
			Default("pending"),
		field.String("error_message").
			Optional().
			Nillable(),
		field.JSON("result_data", map[string]any{}).
			Optional(),
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			Annotations(&entsql.Annotation{
				DefaultExprs: map[string]string{
					dialect.Postgres: "now()",
					dialect.SQLite:   "(datetime('now'))",
				},
			}),
		field.Time("expires_at").
			Annotations(&entsql.Annotation{
				DefaultExprs: map[string]string{
					dialect.Postgres: "(now() + interval '10 minutes')",
				},
			}),
	}
}

func (DeviceSession) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("state").Unique(),
		index.Fields("expires_at"),
	}
}

func (DeviceSession) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("device_sessions"),
	}
}
