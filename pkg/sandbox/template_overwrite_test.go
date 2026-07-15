package sandbox

import (
	"strings"
	"testing"
	"time"
)

func TestPlanTemplateOverwrite_FreshCreate(t *testing.T) {
	plan, err := planTemplateOverwrite("myapp", "base", nil, false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.BasedOn != "base" || plan.DeleteFirst || plan.Flatten || plan.MaterializeRootfs {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestPlanTemplateOverwrite_ExistsNoOverwrite(t *testing.T) {
	_, err := planTemplateOverwrite("myapp", "base", &TemplateMeta{Name: "myapp"}, true, false, true)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("got %v, want already exists", err)
	}
}

func TestPlanTemplateOverwrite_SelfOverwriteFlattens(t *testing.T) {
	meta := &TemplateMeta{Name: "juicytrade", BasedOn: "base", CreatedAt: time.Now()}
	plan, err := planTemplateOverwrite("juicytrade", "juicytrade", meta, true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.SelfOverwrite || !plan.Flatten || plan.DeleteFirst {
		t.Fatalf("want flatten self-overwrite without delete-first, got %+v", plan)
	}
	if plan.BasedOn != "base" {
		t.Fatalf("BasedOn = %q, want base", plan.BasedOn)
	}
}

func TestPlanTemplateOverwrite_SelfOverwriteAtPrefix(t *testing.T) {
	meta := &TemplateMeta{Name: "juicytrade", BasedOn: "@base"}
	plan, err := planTemplateOverwrite("@juicytrade", "juicytrade", meta, true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Flatten || plan.BasedOn != "base" {
		t.Fatalf("got %+v", plan)
	}
}

func TestPlanTemplateOverwrite_OtherOverwriteDeletesFirst(t *testing.T) {
	meta := &TemplateMeta{Name: "old", BasedOn: "base"}
	plan, err := planTemplateOverwrite("old", "juicytrade", meta, true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.DeleteFirst || plan.Flatten || plan.BasedOn != "juicytrade" {
		t.Fatalf("got %+v", plan)
	}
}

func TestPlanTemplateOverwrite_OrphanedSourceMaterializes(t *testing.T) {
	// juicytrade already deleted from registry; session still based on it.
	plan, err := planTemplateOverwrite("juicytrade", "juicytrade", nil, false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.MaterializeRootfs || plan.BasedOn != "base" {
		t.Fatalf("got %+v", plan)
	}
}

func TestPlanTemplateOverwrite_CircularBasedOn(t *testing.T) {
	meta := &TemplateMeta{Name: "juicytrade", BasedOn: "juicytrade"}
	_, err := planTemplateOverwrite("juicytrade", "juicytrade", meta, true, true, true)
	if err == nil || !strings.Contains(err.Error(), "points to itself") {
		t.Fatalf("got %v", err)
	}
}

func TestNormalizeTemplateRef(t *testing.T) {
	if got := normalizeTemplateRef(" @Foo "); got != "Foo" {
		t.Fatalf("got %q", got)
	}
}
