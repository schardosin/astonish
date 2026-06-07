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

// OrgApp holds the schema definition for the OrgApp entity.
type OrgApp struct {
	ent.Schema
}

func (OrgApp) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("slug").
			NotEmpty(),
		field.String("name").
			NotEmpty(),
		field.String("description").
			Default(""),
		field.JSON("definition", map[string]any{}),
		field.UUID("promoted_by", uuid.UUID{}),
		field.String("promoted_from_team").
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

func (OrgApp) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("slug").Unique(),
	}
}

func (OrgApp) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("org_apps"),
	}
}
