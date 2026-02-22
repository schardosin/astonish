package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateFlowKnowledgeDoc_BasicFlow(t *testing.T) {
	yamlContent := `name: check_server
description: Check server status via SSH
nodes:
  - name: get_target
    type: input
    prompt: "Enter the server IP or hostname"
    output_model:
      target: "string"
  - name: check_status
    type: llm
    system: "You are a server admin assistant"
    prompt: "SSH into {{get_target.target}} and check disk, memory, and CPU usage"
    tools_selection:
      - shell_command
`

	entry := FlowRegistryEntry{
		FlowFile:    "check_server.yaml",
		Description: "Check server status via SSH",
		Tags:        []string{"ssh", "monitoring", "server"},
		CreatedAt:   time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC),
	}

	doc := GenerateFlowKnowledgeDoc(yamlContent, entry)

	// Verify sections exist
	checks := []struct {
		name    string
		content string
	}{
		{"title", "# check_server"},
		{"description", "Check server status via SSH"},
		{"tags", "ssh, monitoring, server"},
		{"flow file", "check_server.yaml"},
		{"created date", "2025-06-15"},
		{"hash", "**Hash:**"},
		{"parameters section", "## Parameters"},
		{"parameter name", "`get_target`"},
		{"tools section", "## Tools Used"},
		{"tool name", "shell_command"},
		{"workflow section", "## Workflow Steps"},
		{"step 1", "**get_target**"},
		{"step 2", "**check_status**"},
		{"keywords section", "## Search Keywords"},
	}

	for _, check := range checks {
		if !strings.Contains(doc, check.content) {
			t.Errorf("expected doc to contain %s (%q)", check.name, check.content)
		}
	}
}

func TestGenerateFlowKnowledgeDoc_InvalidYAML(t *testing.T) {
	entry := FlowRegistryEntry{
		FlowFile:    "broken.yaml",
		Description: "A broken flow",
		Tags:        []string{"broken"},
	}

	doc := GenerateFlowKnowledgeDoc("not: valid: yaml: {{", entry)

	// Should still produce a minimal doc from registry entry
	if !strings.Contains(doc, "# broken") {
		t.Error("expected fallback title from flow file name")
	}
	if !strings.Contains(doc, "A broken flow") {
		t.Error("expected description from entry")
	}
}

func TestGenerateFlowKnowledgeDoc_NoInputNodes(t *testing.T) {
	yamlContent := `name: simple_task
description: A simple task
nodes:
  - name: do_thing
    type: llm
    prompt: "Do something"
`

	entry := FlowRegistryEntry{
		FlowFile:    "simple_task.yaml",
		Description: "A simple task",
	}

	doc := GenerateFlowKnowledgeDoc(yamlContent, entry)

	// Should not have Parameters section
	if strings.Contains(doc, "## Parameters") {
		t.Error("expected no Parameters section for flow without input nodes")
	}
}

func TestReconcileFlowKnowledge_CreatesNewDocs(t *testing.T) {
	flowsDir := t.TempDir()
	memFlowsDir := filepath.Join(t.TempDir(), "flows")

	// Create a flow YAML
	yamlContent := `name: test_flow
description: Test flow
nodes:
  - name: step1
    type: llm
    prompt: "Do something"
`
	if err := os.WriteFile(filepath.Join(flowsDir, "test_flow.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	entries := []FlowRegistryEntry{
		{
			FlowFile:    "test_flow.yaml",
			Description: "Test flow",
			Tags:        []string{"test"},
		},
	}

	if err := ReconcileFlowKnowledge(flowsDir, memFlowsDir, entries); err != nil {
		t.Fatalf("ReconcileFlowKnowledge failed: %v", err)
	}

	// Verify knowledge doc was created
	docPath := filepath.Join(memFlowsDir, "test_flow.md")
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("expected knowledge doc to exist: %v", err)
	}

	doc := string(data)
	if !strings.Contains(doc, "# test_flow") {
		t.Error("expected knowledge doc to contain flow title")
	}
	if !strings.Contains(doc, "**Hash:**") {
		t.Error("expected knowledge doc to contain hash")
	}
}

func TestReconcileFlowKnowledge_RemovesOrphanedDocs(t *testing.T) {
	flowsDir := t.TempDir()
	memFlowsDir := t.TempDir()

	// Create an orphaned knowledge doc (no corresponding YAML)
	orphanPath := filepath.Join(memFlowsDir, "deleted_flow.md")
	if err := os.WriteFile(orphanPath, []byte("# deleted_flow\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Run with empty entries
	if err := ReconcileFlowKnowledge(flowsDir, memFlowsDir, nil); err != nil {
		t.Fatalf("ReconcileFlowKnowledge failed: %v", err)
	}

	// Verify orphaned doc was removed
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Error("expected orphaned knowledge doc to be removed")
	}
}

func TestReconcileFlowKnowledge_SkipsMissingYAML(t *testing.T) {
	flowsDir := t.TempDir()
	memFlowsDir := filepath.Join(t.TempDir(), "flows")

	// Entry references a YAML file that doesn't exist
	entries := []FlowRegistryEntry{
		{
			FlowFile:    "nonexistent.yaml",
			Description: "Missing flow",
		},
	}

	if err := ReconcileFlowKnowledge(flowsDir, memFlowsDir, entries); err != nil {
		t.Fatalf("ReconcileFlowKnowledge failed: %v", err)
	}

	// Verify no doc was created for missing YAML
	docPath := filepath.Join(memFlowsDir, "nonexistent.md")
	if _, err := os.Stat(docPath); !os.IsNotExist(err) {
		t.Error("expected no knowledge doc for missing YAML")
	}
}

func TestReconcileFlowKnowledge_RegeneratesOnChange(t *testing.T) {
	flowsDir := t.TempDir()
	memFlowsDir := t.TempDir()

	yamlV1 := `name: evolving_flow
description: Version 1
nodes:
  - name: step1
    type: llm
    prompt: "Do version 1 things"
`
	yamlV2 := `name: evolving_flow
description: Version 2 with changes
nodes:
  - name: step1
    type: llm
    prompt: "Do version 2 things"
  - name: step2
    type: llm
    prompt: "Also do more things"
`
	flowPath := filepath.Join(flowsDir, "evolving_flow.yaml")
	entry := FlowRegistryEntry{
		FlowFile:    "evolving_flow.yaml",
		Description: "Evolving flow",
	}

	// Create V1
	if err := os.WriteFile(flowPath, []byte(yamlV1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileFlowKnowledge(flowsDir, memFlowsDir, []FlowRegistryEntry{entry}); err != nil {
		t.Fatal(err)
	}

	docPath := filepath.Join(memFlowsDir, "evolving_flow.md")
	v1Doc, _ := os.ReadFile(docPath)

	// Update to V2
	if err := os.WriteFile(flowPath, []byte(yamlV2), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileFlowKnowledge(flowsDir, memFlowsDir, []FlowRegistryEntry{entry}); err != nil {
		t.Fatal(err)
	}

	v2Doc, _ := os.ReadFile(docPath)

	// V2 should have different content (different hash at minimum)
	if string(v1Doc) == string(v2Doc) {
		t.Error("expected knowledge doc to be regenerated after YAML change")
	}
	if !strings.Contains(string(v2Doc), "step2") {
		t.Error("expected V2 doc to include the new step")
	}
}

// --- Flow Registry Sync Tests ---

func TestHasFlow(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "flow_registry.json")

	registry, err := NewFlowRegistry(regPath)
	if err != nil {
		t.Fatal(err)
	}

	if registry.HasFlow("test.yaml") {
		t.Error("expected HasFlow to return false for empty registry")
	}

	_ = registry.Register(FlowRegistryEntry{
		FlowFile:    "test.yaml",
		Description: "Test flow",
		CreatedAt:   time.Now(),
	})

	if !registry.HasFlow("test.yaml") {
		t.Error("expected HasFlow to return true after registering")
	}
	if registry.HasFlow("other.yaml") {
		t.Error("expected HasFlow to return false for unregistered flow")
	}
}

func TestSyncFromDirectory_RegistersNewFlows(t *testing.T) {
	dir := t.TempDir()
	flowsDir := filepath.Join(dir, "flows")
	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	regPath := filepath.Join(dir, "flow_registry.json")

	// Create two YAML files
	yaml1 := `description: Check server status
nodes:
  - name: check
    type: llm
    prompt: "Check the server"
    tools_selection:
      - shell_command
`
	yaml2 := `description: Deploy application
nodes:
  - name: deploy
    type: llm
    prompt: "Deploy the app"
`
	os.WriteFile(filepath.Join(flowsDir, "check_server.yaml"), []byte(yaml1), 0644)
	os.WriteFile(filepath.Join(flowsDir, "deploy_app.yaml"), []byte(yaml2), 0644)

	registry, _ := NewFlowRegistry(regPath)
	added, err := registry.SyncFromDirectory(flowsDir)
	if err != nil {
		t.Fatalf("SyncFromDirectory failed: %v", err)
	}

	if added != 2 {
		t.Errorf("expected 2 flows added, got %d", added)
	}

	entries := registry.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Verify descriptions were parsed from YAML
	descMap := make(map[string]string)
	for _, e := range entries {
		descMap[e.FlowFile] = e.Description
	}
	if descMap["check_server.yaml"] != "Check server status" {
		t.Errorf("expected description 'Check server status', got %q", descMap["check_server.yaml"])
	}
	if descMap["deploy_app.yaml"] != "Deploy application" {
		t.Errorf("expected description 'Deploy application', got %q", descMap["deploy_app.yaml"])
	}
}

func TestSyncFromDirectory_SkipsAlreadyRegistered(t *testing.T) {
	dir := t.TempDir()
	flowsDir := filepath.Join(dir, "flows")
	os.MkdirAll(flowsDir, 0755)
	regPath := filepath.Join(dir, "flow_registry.json")

	os.WriteFile(filepath.Join(flowsDir, "existing.yaml"), []byte("description: Existing flow\nnodes: []\n"), 0644)

	registry, _ := NewFlowRegistry(regPath)

	// Pre-register the flow
	_ = registry.Register(FlowRegistryEntry{
		FlowFile:    "existing.yaml",
		Description: "Already registered",
		CreatedAt:   time.Now(),
	})

	added, err := registry.SyncFromDirectory(flowsDir)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 {
		t.Errorf("expected 0 flows added (already registered), got %d", added)
	}

	// Verify original description preserved (not overwritten by YAML parse)
	entries := registry.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Description != "Already registered" {
		t.Errorf("expected original description preserved, got %q", entries[0].Description)
	}
}

func TestSyncFromDirectory_PrunesDeletedFlows(t *testing.T) {
	dir := t.TempDir()
	flowsDir := filepath.Join(dir, "flows")
	os.MkdirAll(flowsDir, 0755)
	regPath := filepath.Join(dir, "flow_registry.json")

	// Create a YAML file and register it
	os.WriteFile(filepath.Join(flowsDir, "will_keep.yaml"), []byte("description: Keep\nnodes: []\n"), 0644)

	registry, _ := NewFlowRegistry(regPath)
	_ = registry.Register(FlowRegistryEntry{
		FlowFile:    "will_keep.yaml",
		Description: "Keep this",
		CreatedAt:   time.Now(),
	})
	_ = registry.Register(FlowRegistryEntry{
		FlowFile:    "deleted.yaml",
		Description: "This YAML was deleted from disk",
		CreatedAt:   time.Now(),
	})

	if len(registry.Entries()) != 2 {
		t.Fatalf("expected 2 entries before sync, got %d", len(registry.Entries()))
	}

	added, err := registry.SyncFromDirectory(flowsDir)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 {
		t.Errorf("expected 0 added, got %d", added)
	}

	entries := registry.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after pruning, got %d", len(entries))
	}
	if entries[0].FlowFile != "will_keep.yaml" {
		t.Errorf("expected will_keep.yaml to survive, got %q", entries[0].FlowFile)
	}
}

func TestSyncFromDirectory_ExtractsToolsAsTags(t *testing.T) {
	dir := t.TempDir()
	flowsDir := filepath.Join(dir, "flows")
	os.MkdirAll(flowsDir, 0755)
	regPath := filepath.Join(dir, "flow_registry.json")

	yamlContent := `description: Server check
nodes:
  - name: check
    type: llm
    prompt: "Check it"
    tools_selection:
      - shell_command
      - read_file
`
	os.WriteFile(filepath.Join(flowsDir, "server_check.yaml"), []byte(yamlContent), 0644)

	registry, _ := NewFlowRegistry(regPath)
	added, _ := registry.SyncFromDirectory(flowsDir)
	if added != 1 {
		t.Fatalf("expected 1 added, got %d", added)
	}

	entries := registry.Entries()
	tags := entries[0].Tags
	if len(tags) == 0 {
		t.Fatal("expected tags to be populated from tools_selection")
	}

	tagSet := make(map[string]bool)
	for _, tag := range tags {
		tagSet[tag] = true
	}
	if !tagSet["shell_command"] {
		t.Error("expected shell_command in tags")
	}
	if !tagSet["read_file"] {
		t.Error("expected read_file in tags")
	}
}

func TestSyncFromDirectory_NonexistentDir(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "flow_registry.json")

	registry, _ := NewFlowRegistry(regPath)
	added, err := registry.SyncFromDirectory(filepath.Join(dir, "does_not_exist"))
	if err != nil {
		t.Fatalf("expected no error for nonexistent dir, got: %v", err)
	}
	if added != 0 {
		t.Errorf("expected 0 added, got %d", added)
	}
}

func TestSyncFromDirectory_PrunesStaleEntriesWhenDirMissing(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "flow_registry.json")

	registry, _ := NewFlowRegistry(regPath)
	// Pre-register entries that point to a nonexistent directory
	_ = registry.Register(FlowRegistryEntry{
		FlowFile:    "stale_flow.yaml",
		Description: "This flow's directory doesn't exist",
		CreatedAt:   time.Now(),
	})
	_ = registry.Register(FlowRegistryEntry{
		FlowFile:    "another_stale.yaml",
		Description: "Also stale",
		CreatedAt:   time.Now(),
	})

	if len(registry.Entries()) != 2 {
		t.Fatalf("expected 2 entries before sync, got %d", len(registry.Entries()))
	}

	added, err := registry.SyncFromDirectory(filepath.Join(dir, "nonexistent_flows"))
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 {
		t.Errorf("expected 0 added, got %d", added)
	}

	entries := registry.Entries()
	if len(entries) != 0 {
		t.Errorf("expected all stale entries pruned, got %d entries", len(entries))
	}
}

func TestSyncFromDirectory_FallbackDescriptionForBadYAML(t *testing.T) {
	dir := t.TempDir()
	flowsDir := filepath.Join(dir, "flows")
	os.MkdirAll(flowsDir, 0755)
	regPath := filepath.Join(dir, "flow_registry.json")

	// Write invalid YAML
	os.WriteFile(filepath.Join(flowsDir, "broken_flow.yaml"), []byte("not: valid: yaml: {{"), 0644)

	registry, _ := NewFlowRegistry(regPath)
	added, _ := registry.SyncFromDirectory(flowsDir)
	if added != 1 {
		t.Fatalf("expected 1 added even for broken YAML, got %d", added)
	}

	entries := registry.Entries()
	if entries[0].Description != "broken_flow" {
		t.Errorf("expected fallback description 'broken_flow', got %q", entries[0].Description)
	}
}
