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

// AppState holds the schema definition for the AppState entity.
// Logical composite key: (app_id, user_id, key) enforced via unique index.
type AppState struct {
	ent.Schema
}

func (AppState) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.UUID("app_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}),
		field.String("key").
			NotEmpty(),
		field.JSON("value", map[string]any{}).
			Optional(),
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

func (AppState) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("app_id", "user_id", "key").Unique(),
	}
}

func (AppState) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("app_state"),
	}
}
