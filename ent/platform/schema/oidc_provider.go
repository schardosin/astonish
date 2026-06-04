package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// OIDCProvider holds the schema for the oidc_providers table.
type OIDCProvider struct {
	ent.Schema
}

func (OIDCProvider) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.UUID("org_id", uuid.UUID{}).
			Optional().
			Nillable(),
		field.String("name").
			Default(""),
		field.String("issuer_url").
			NotEmpty(),
		field.String("discovery_url").
			Default(""),
		field.String("client_id").
			NotEmpty(),
		field.String("client_secret").
			NotEmpty(),
		field.JSON("scopes", []string{}).
			Optional().
			Default([]string{"openid", "email", "profile"}),
		field.String("team_claim").
			Optional().
			Nillable(),
		field.Bool("enabled").
			Default(true),
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

func (OIDCProvider) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("organization", Organization.Type).
			Ref("oidc_providers").
			Field("org_id").
			Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (OIDCProvider) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("org_id", "issuer_url").Unique(),
	}
}

func (OIDCProvider) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("oidc_providers"),
	}
}
