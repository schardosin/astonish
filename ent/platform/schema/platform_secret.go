package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// PlatformSecret holds the schema for the platform_secrets table.
type PlatformSecret struct {
	ent.Schema
}

func (PlatformSecret) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			StorageKey("key").
			NotEmpty(),
		field.Bytes("value").
			NotEmpty(),
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

func (PlatformSecret) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("platform_secrets"),
	}
}
