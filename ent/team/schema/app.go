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

// App holds the schema definition for the App entity.
type App struct {
	ent.Schema
}

func (App) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("slug").
			NotEmpty(),
		field.String("name").
			NotEmpty(),
		field.String("description").
			Default(""),
		field.String("code").
			Default(""),
		field.Int("version").
			Default(1),
		field.String("session_id").
			Default(""),
		field.UUID("published_by", uuid.UUID{}).
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

func (App) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("slug").Unique(),
	}
}

func (App) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("apps"),
	}
}
