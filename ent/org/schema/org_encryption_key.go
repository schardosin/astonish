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

// OrgEncryptionKey holds the schema definition for the OrgEncryptionKey entity.
type OrgEncryptionKey struct {
	ent.Schema
}

func (OrgEncryptionKey) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("key_name").
			NotEmpty(),
		field.Bytes("key_data").
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
	}
}

func (OrgEncryptionKey) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("key_name").Unique(),
	}
}

func (OrgEncryptionKey) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("org_encryption_keys"),
	}
}
