package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// PlatformSetting holds the schema for the platform_settings table.
type PlatformSetting struct {
	ent.Schema
}

func (PlatformSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			StorageKey("key").
			NotEmpty(),
		field.JSON("value", map[string]any{}).
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

func (PlatformSetting) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("platform_settings"),
	}
}
