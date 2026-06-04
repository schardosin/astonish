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

// User holds the schema definition for the User entity.
type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("email").
			NotEmpty(),
		field.String("display_name").
			NotEmpty(),
		field.String("password_hash").
			Optional().
			Nillable(),
		field.String("oidc_subject").
			Optional().
			Nillable(),
		field.String("oidc_issuer").
			Optional().
			Nillable(),
		field.String("platform_role").
			Optional().
			Nillable().
			Comment("NULL for non-superadmins; 'superadmin' for superadmins"),
		field.Enum("status").
			Values("active", "suspended", "deactivated", "pending_verification").
			Default("active"),
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			Annotations(&entsql.Annotation{
				DefaultExprs: map[string]string{
					dialect.Postgres: "now()",
					dialect.SQLite:   "(datetime('now'))",
				},
			}),
		field.Time("last_login_at").
			Optional().
			Nillable(),
	}
}

func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("organizations", Organization.Type).
			Ref("users").
			Through("memberships", OrgMembership.Type),
		edge.To("login_sessions", LoginSession.Type),
		edge.To("channels", UserChannel.Type),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("email").Unique(),
		index.Fields("oidc_issuer", "oidc_subject").Unique(),
	}
}

func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("users"),
	}
}
