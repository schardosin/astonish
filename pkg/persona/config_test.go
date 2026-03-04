package persona

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPersona_Valid(t *testing.T) {
	dir := t.TempDir()
	content := `name: "Test Dev"
description: A test developer persona
prompt: |
  You are a test developer.
expertise:
  - Testing
  - Go
`
	path := filepath.Join(dir, "test_dev.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
		return
	}

	p, err := LoadPersona(path)
	if err != nil {
		t.Fatal(err)
		return
	}

	if p.Name != "Test Dev" {
		t.Errorf("expected name %q, got %q", "Test Dev", p.Name)
	}
	if p.Description != "A test developer persona" {
		t.Errorf("expected description %q, got %q", "A test developer persona", p.Description)
	}
	if p.Prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if len(p.Expertise) != 2 {
		t.Errorf("expected 2 expertise items, got %d", len(p.Expertise))
	}
}

func TestLoadPersona_MissingName(t *testing.T) {
	dir := t.TempDir()
	content := `description: no name
prompt: something
`
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
		return
	}

	_, err := LoadPersona(path)
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestLoadPersona_MissingPrompt(t *testing.T) {
	dir := t.TempDir()
	content := `name: "No Prompt"
description: missing prompt field
`
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
		return
	}

	_, err := LoadPersona(path)
	if err == nil {
		t.Error("expected error for missing prompt")
	}
}

func TestLoadPersonas_Directory(t *testing.T) {
	dir := t.TempDir()

	files := map[string]string{
		"dev.yaml": `name: Developer
description: Writes code
prompt: You are a developer.
`,
		"qa.yaml": `name: QA Engineer
description: Tests code
prompt: You are a QA engineer.
`,
		"readme.md": `This is not a persona file`,
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
			return
		}
	}

	personas, err := LoadPersonas(dir)
	if err != nil {
		t.Fatal(err)
		return
	}

	if len(personas) != 2 {
		t.Errorf("expected 2 personas, got %d", len(personas))
	}
	if _, ok := personas["dev"]; !ok {
		t.Error("expected persona with key 'dev'")
	}
	if _, ok := personas["qa"]; !ok {
		t.Error("expected persona with key 'qa'")
	}
}

func TestLoadPersonas_NonExistentDir(t *testing.T) {
	personas, err := LoadPersonas("/tmp/nonexistent-persona-dir-12345")
	if err != nil {
		t.Errorf("expected nil error for non-existent dir, got %v", err)
	}
	if personas != nil {
		t.Errorf("expected nil map for non-existent dir, got %v", personas)
	}
}

func TestPersonaValidate(t *testing.T) {
	tests := []struct {
		name    string
		persona PersonaConfig
		wantErr bool
	}{
		{
			name:    "valid",
			persona: PersonaConfig{Name: "Dev", Prompt: "You are a dev."},
			wantErr: false,
		},
		{
			name:    "empty name",
			persona: PersonaConfig{Name: "", Prompt: "something"},
			wantErr: true,
		},
		{
			name:    "whitespace name",
			persona: PersonaConfig{Name: "   ", Prompt: "something"},
			wantErr: true,
		},
		{
			name:    "empty prompt",
			persona: PersonaConfig{Name: "Dev", Prompt: ""},
			wantErr: true,
		},
		{
			name:    "whitespace prompt",
			persona: PersonaConfig{Name: "Dev", Prompt: "   "},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.persona.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegistry_Basic(t *testing.T) {
	dir := t.TempDir()

	// Write a persona file
	content := `name: Developer
description: Writes code
prompt: You are a developer.
expertise:
  - Go
`
	if err := os.WriteFile(filepath.Join(dir, "dev.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
		return
	}

	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
		return
	}

	if reg.Count() != 1 {
		t.Errorf("expected 1 persona, got %d", reg.Count())
	}

	p, ok := reg.GetPersona("dev")
	if !ok {
		t.Fatal("expected to find persona 'dev'")
		return
	}
	if p.Name != "Developer" {
		t.Errorf("expected name %q, got %q", "Developer", p.Name)
	}

	summaries := reg.ListPersonas()
	if len(summaries) != 1 {
		t.Errorf("expected 1 summary, got %d", len(summaries))
	}
}

func TestRegistry_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
		return
	}

	if reg.Count() != 0 {
		t.Errorf("expected 0 personas, got %d", reg.Count())
	}
}

func TestRegistry_NonExistentDir(t *testing.T) {
	reg, err := NewRegistry("/tmp/nonexistent-persona-reg-12345")
	if err != nil {
		t.Errorf("expected no error for non-existent dir, got %v", err)
	}
	if reg == nil {
		t.Fatal("expected non-nil registry")
		return
	}
	if reg.Count() != 0 {
		t.Errorf("expected 0 personas, got %d", reg.Count())
	}
}

func TestRegistry_SaveAndDelete(t *testing.T) {
	dir := t.TempDir()

	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
		return
	}

	// Save
	persona := &PersonaConfig{
		Name:        "Tester",
		Description: "Tests things",
		Prompt:      "You are a tester.",
		Expertise:   []string{"Testing"},
	}
	if err := reg.Save("tester", persona); err != nil {
		t.Fatal(err)
		return
	}

	if reg.Count() != 1 {
		t.Errorf("expected 1 persona after save, got %d", reg.Count())
	}

	// Verify file exists on disk
	if _, err := os.Stat(filepath.Join(dir, "tester.yaml")); err != nil {
		t.Errorf("expected tester.yaml on disk: %v", err)
	}

	// Delete
	if err := reg.Delete("tester"); err != nil {
		t.Fatal(err)
		return
	}

	if reg.Count() != 0 {
		t.Errorf("expected 0 personas after delete, got %d", reg.Count())
	}

	// Verify file removed from disk
	if _, err := os.Stat(filepath.Join(dir, "tester.yaml")); !os.IsNotExist(err) {
		t.Error("expected tester.yaml to be removed from disk")
	}
}

func TestRegistry_Reload(t *testing.T) {
	dir := t.TempDir()

	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
		return
	}

	if reg.Count() != 0 {
		t.Errorf("expected 0 personas initially, got %d", reg.Count())
	}

	// Write a file externally
	content := `name: External
description: Added externally
prompt: You are external.
`
	if err := os.WriteFile(filepath.Join(dir, "external.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
		return
	}

	// Reload
	if err := reg.Reload(); err != nil {
		t.Fatal(err)
		return
	}

	if reg.Count() != 1 {
		t.Errorf("expected 1 persona after reload, got %d", reg.Count())
	}
}
