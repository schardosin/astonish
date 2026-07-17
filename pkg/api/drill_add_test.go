package api

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/store"
)

// drillAddMemFlowStore is a minimal FlowStore for resolveDrillAddWizard tests.
type drillAddMemFlowStore struct {
	flows map[string]string
	meta  map[string]store.FlowSummary
}

func newDrillAddMemFlowStore() *drillAddMemFlowStore {
	return &drillAddMemFlowStore{
		flows: make(map[string]string),
		meta:  make(map[string]store.FlowSummary),
	}
}

func (m *drillAddMemFlowStore) ListAllFlows(_ context.Context) []store.FlowSummary {
	out := make([]store.FlowSummary, 0, len(m.meta))
	for _, s := range m.meta {
		out = append(out, s)
	}
	return out
}

func (m *drillAddMemFlowStore) ListFlowsByType(_ context.Context, types []string) []store.FlowSummary {
	want := map[string]bool{}
	for _, t := range types {
		want[t] = true
	}
	out := make([]store.FlowSummary, 0)
	for _, s := range m.meta {
		if want[s.Type] {
			out = append(out, s)
		}
	}
	return out
}

func (m *drillAddMemFlowStore) GetFlow(_ context.Context, name string) (string, error) {
	yamlContent, ok := m.flows[name]
	if !ok {
		return "", fmt.Errorf("flow %q not found", name)
	}
	return yamlContent, nil
}

func (m *drillAddMemFlowStore) SaveFlow(_ context.Context, name string, yamlContent string) error {
	m.flows[name] = yamlContent
	typ := "flow"
	suite := ""
	if strings.Contains(yamlContent, "type: drill_suite") {
		typ = "drill_suite"
	} else if strings.Contains(yamlContent, "type: drill") {
		typ = "drill"
	}
	for _, line := range strings.Split(yamlContent, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "suite:") {
			suite = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "suite:")), `"'`)
		}
	}
	m.meta[name] = store.FlowSummary{Name: name, Type: typ, Suite: suite}
	return nil
}

func (m *drillAddMemFlowStore) DeleteFlow(_ context.Context, name string) error {
	delete(m.flows, name)
	delete(m.meta, name)
	return nil
}

func (m *drillAddMemFlowStore) GetTaps(_ context.Context) []store.FlowTap             { return nil }
func (m *drillAddMemFlowStore) AddTap(_ context.Context, _ string, _ string) (string, error) {
	return "", fmt.Errorf("not implemented")
}
func (m *drillAddMemFlowStore) RemoveTap(_ context.Context, _ string) error { return nil }
func (m *drillAddMemFlowStore) GetStoreDir(_ context.Context) string       { return "" }

func TestResolveDrillAddWizard_LoadsFromTeamStore(t *testing.T) {
	fs := newDrillAddMemFlowStore()
	ctx := context.Background()
	suiteYAML := "description: Juicytrade suite\ntype: drill_suite\nsuite_config:\n  template: juicytrade\n"
	drillYAML := "type: drill\nsuite: juicytrade\ndescription: health\nnodes: []\n"
	if err := fs.SaveFlow(ctx, "juicytrade", suiteYAML); err != nil {
		t.Fatal(err)
	}
	if err := fs.SaveFlow(ctx, "health", drillYAML); err != nil {
		t.Fatal(err)
	}

	suiteCtx, prompt, err := resolveDrillAddWizard(ctx, fs, "juicytrade")
	if err != nil {
		t.Fatalf("resolveDrillAddWizard: %v", err)
	}
	if !strings.Contains(suiteCtx, "Suite: juicytrade") {
		t.Errorf("suite context missing suite name: %s", suiteCtx)
	}
	if !strings.Contains(suiteCtx, "health") {
		t.Errorf("suite context missing existing drill: %s", suiteCtx)
	}
	if !strings.Contains(prompt, "juicytrade") {
		t.Errorf("wizard prompt missing suite name: %s", prompt[:min(200, len(prompt))])
	}
}

func TestResolveDrillAddWizard_NotFound(t *testing.T) {
	fs := newDrillAddMemFlowStore()
	_, _, err := resolveDrillAddWizard(context.Background(), fs, "juicytrade")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), `Suite "juicytrade" not found`) {
		t.Errorf("error = %v", err)
	}
}

func TestResolveDrillAddWizard_NilStore(t *testing.T) {
	_, _, err := resolveDrillAddWizard(context.Background(), nil, "juicytrade")
	if err == nil || !strings.Contains(err.Error(), "platform mode") {
		t.Fatalf("error = %v, want platform mode message", err)
	}
}

func TestResolveDrillAddWizard_EmptyName(t *testing.T) {
	_, _, err := resolveDrillAddWizard(context.Background(), newDrillAddMemFlowStore(), "  ")
	if err == nil || !strings.Contains(err.Error(), "Usage:") {
		t.Fatalf("error = %v, want usage", err)
	}
}

func TestResolveTutorialAddContext_RequiresTutorialSuite(t *testing.T) {
	fs := newDrillAddMemFlowStore()
	ctx := context.Background()
	suiteYAML := "description: Juicytrade suite\ntype: drill_suite\nsuite_config:\n  template: juicytrade\n"
	regularDrill := "type: drill\nsuite: juicytrade\ndescription: health\ndrill_config:\n  tags: [smoke]\nnodes: []\n"
	if err := fs.SaveFlow(ctx, "juicytrade", suiteYAML); err != nil {
		t.Fatal(err)
	}
	if err := fs.SaveFlow(ctx, "health", regularDrill); err != nil {
		t.Fatal(err)
	}

	_, _, err := resolveTutorialAddContext(ctx, fs, "juicytrade")
	if err == nil {
		t.Fatal("expected error for regular suite")
	}
	if !strings.Contains(err.Error(), "regular drill suite") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "/tutorial-drill") {
		t.Fatalf("error should guide to /tutorial-drill: %v", err)
	}
}

func TestTutorialDrillSlashParsers(t *testing.T) {
	if _, ok := tutorialDrillHint("/tutorial-drill-add foo"); ok {
		t.Fatal("hint parser must not match tutorial-drill-add")
	}
	if _, ok := tutorialDrillHint("/tutorial-add foo"); ok {
		t.Fatal("hint parser must not match tutorial-add")
	}
	hint, ok := tutorialDrillHint("/tutorial-drill teach login")
	if !ok || hint != "teach login" {
		t.Fatalf("tutorial-drill hint = %q ok=%v", hint, ok)
	}
	hint, ok = tutorialDrillHint("/tutorial teach login")
	if !ok || hint != "teach login" {
		t.Fatalf("alias /tutorial hint = %q ok=%v", hint, ok)
	}
	suite, ok := tutorialDrillAddSuite("/tutorial-drill-add my-suite")
	if !ok || suite != "my-suite" {
		t.Fatalf("tutorial-drill-add suite = %q ok=%v", suite, ok)
	}
	suite, ok = tutorialDrillAddSuite("/tutorial-add my-suite")
	if !ok || suite != "my-suite" {
		t.Fatalf("alias /tutorial-add suite = %q ok=%v", suite, ok)
	}
}

func TestResolveTutorialAddContext_AllowsTutorialSuite(t *testing.T) {
	fs := newDrillAddMemFlowStore()
	ctx := context.Background()
	suiteYAML := "description: Tutorial suite\ntype: drill_suite\nsuite_config:\n  template: juicytrade\n  setup:\n    - bash start-services.sh\n"
	tutDrill := "type: drill\nsuite: juicytrade-tutorial\ndescription: overview\ndrill_config:\n  mode: tutorial\n  tags: [tutorial]\nnodes: []\n"
	if err := fs.SaveFlow(ctx, "juicytrade-tutorial", suiteYAML); err != nil {
		t.Fatal(err)
	}
	if err := fs.SaveFlow(ctx, "overview", tutDrill); err != nil {
		t.Fatal(err)
	}

	name, suiteCtx, err := resolveTutorialAddContext(ctx, fs, "juicytrade-tutorial")
	if err != nil {
		t.Fatalf("resolveTutorialAddContext: %v", err)
	}
	if name != "juicytrade-tutorial" {
		t.Fatalf("name = %q", name)
	}
	if !strings.Contains(suiteCtx, "TutorialSuite: yes") {
		t.Fatalf("suite context: %s", suiteCtx)
	}
}
