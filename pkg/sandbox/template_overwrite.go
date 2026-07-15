package sandbox

import (
	"fmt"
	"strings"
)

// templateOverwritePlan decides how CreateTemplateFromContainer should replace
// an existing template without deleting a source lower layer prematurely.
type templateOverwritePlan struct {
	// BasedOn is the template used for ResolveLowerLayers and registry BasedOn.
	BasedOn string
	// DeleteFirst is true when the target may be removed before create (target
	// is not needed as a live lower for the session).
	DeleteFirst bool
	// Flatten is true for self-overwrite: merge the existing template upper
	// with the session upper, then re-register BasedOn=parent (not self).
	Flatten bool
	// SelfOverwrite is true when overwriting the same template the session
	// was created from.
	SelfOverwrite bool
	// MaterializeRootfs is true when the session's source template is missing
	// from the registry (e.g. after a failed self-overwrite). The full session
	// container rootfs is copied as the new upper and BasedOn is base.
	MaterializeRootfs bool
}

// normalizeTemplateRef strips a leading @ and whitespace for comparisons.
func normalizeTemplateRef(name string) string {
	return strings.TrimPrefix(strings.TrimSpace(name), "@")
}

// planTemplateOverwrite decides delete-first vs flatten for overwrite saves.
// existing may be nil when the template is missing from the registry.
// instanceExists reports whether the Incus template container currently exists.
// sourceExists reports whether sourceTemplate is still in the registry (always
// true for base).
func planTemplateOverwrite(templateName, sourceTemplate string, existing *TemplateMeta, instanceExists, overwrite, sourceExists bool) (templateOverwritePlan, error) {
	name := normalizeTemplateRef(templateName)
	source := normalizeTemplateRef(sourceTemplate)
	if source == "" {
		source = BaseTemplate
	}

	if name == "" {
		return templateOverwritePlan{}, fmt.Errorf("template name is required")
	}
	if name == BaseTemplate {
		return templateOverwritePlan{}, fmt.Errorf("cannot create a template named %q (reserved)", BaseTemplate)
	}

	plan := templateOverwritePlan{BasedOn: source}

	if !instanceExists {
		// Source template was deleted (failed prior self-overwrite) but the
		// session still names it — materialize the live rootfs onto base.
		if source != BaseTemplate && !sourceExists {
			return templateOverwritePlan{
				BasedOn:           BaseTemplate,
				MaterializeRootfs: true,
				SelfOverwrite:     name == source,
			}, nil
		}
		return plan, nil
	}

	if !overwrite {
		return templateOverwritePlan{}, fmt.Errorf("template %q already exists", name)
	}

	if name == source {
		// Self-overwrite: deleting name first would remove the lower layer the
		// new template needs. Flatten onto the previous BasedOn (usually base).
		parent := BaseTemplate
		if existing != nil {
			if p := normalizeTemplateRef(existing.BasedOn); p != "" {
				parent = p
			}
		}
		if parent == name {
			return templateOverwritePlan{}, fmt.Errorf(
				"cannot overwrite template %q: BasedOn points to itself; fix registry metadata first", name)
		}
		return templateOverwritePlan{
			BasedOn:       parent,
			DeleteFirst:   false, // delete only after staging flatten
			Flatten:       true,
			SelfOverwrite: true,
		}, nil
	}

	// Overwriting a different template than the session source: source stays
	// available as a lower layer, so delete-then-create is safe.
	return templateOverwritePlan{
		BasedOn:     source,
		DeleteFirst: true,
		Flatten:     false,
	}, nil
}
