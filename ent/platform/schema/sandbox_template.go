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

// SandboxTemplate holds the schema for the sandbox_templates table.
type SandboxTemplate struct {
	ent.Schema
}

func (SandboxTemplate) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New),
		field.String("slug").
			NotEmpty(),
		field.Enum("scope").
			Values("global", "org", "team", "personal").
			Default("global"),
		field.String("owner_id").
			Default(""),
		field.String("purpose").
			Default(""),
		field.String("name").
			NotEmpty(),
		field.String("description").
			Default(""),
		field.UUID("parent_template_id", uuid.UUID{}).
			Optional().
			Nillable(),
		field.String("top_layer_id").
			Optional().
			Nillable(),
		// sandbox_image holds a fully-qualified OCI image reference for
		// backends that use per-template container images (e.g., OpenShell).
		// When non-empty, sandbox sessions created from this template use
		// this image instead of the global default. Nullable — existing
		// templates (incus/k8s) leave this unset and use LayerChain instead.
		field.String("sandbox_image").
			Optional().
			Nillable(),
		// packages holds the list of apt packages to install when building
		// a custom sandbox image for this template. JSON-encoded string array.
		// DEPRECATED: Use dockerfile_body instead for full Dockerfile control.
		field.JSON("packages", []string{}).
			Optional(),
		// dockerfile_body stores the user-authored Dockerfile instructions
		// (everything after FROM). Supports arbitrary Dockerfile syntax: RUN,
		// ENV, WORKDIR, COPY --from=, etc. The FROM line is auto-prepended at
		// build time using the platform's configured base image. ENTRYPOINT,
		// CMD, EXPOSE, and FROM are rejected by API validation.
		field.Text("dockerfile_body").
			Optional().
			Nillable(),
		// build_status tracks the state of the last image build Job.
		// Values: "", "building", "succeeded", "failed".
		field.String("build_status").
			Default(""),
		// build_job_name is the K8s Job name of the currently-running or
		// last-completed image build. Empty when no build has been triggered.
		field.String("build_job_name").
			Default(""),
		// build_error stores the error message from the last failed build.
		field.String("build_error").
			Default(""),
		// last_built_image is the full image reference produced by the last
		// successful build. Used to detect no-op rebuilds.
		field.String("last_built_image").
			Default(""),
		// build_started_at records when the last build was triggered.
		field.Time("build_started_at").
			Optional().
			Nillable(),
		field.JSON("base_config", map[string]any{}).
			Optional(),
		field.UUID("configured_by", uuid.UUID{}).
			Optional().
			Nillable(),
		field.Time("configured_at").
			Optional().
			Nillable(),
		field.Int("version").
			Default(1),
		field.UUID("created_by", uuid.UUID{}).
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

func (SandboxTemplate) Edges() []ent.Edge {
	return []ent.Edge{
		// Self-referencing parent
		edge.To("children", SandboxTemplate.Type).
			From("parent").
			Field("parent_template_id").
			Unique(),
		// Layer reference
		edge.From("layer", SandboxLayer.Type).
			Ref("templates").
			Field("top_layer_id").
			Unique(),
	}
}

func (SandboxTemplate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scope", "owner_id", "slug").Unique(),
		index.Fields("scope", "owner_id"),
		index.Fields("parent_template_id"),
	}
}

func (SandboxTemplate) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("sandbox_templates"),
	}
}
