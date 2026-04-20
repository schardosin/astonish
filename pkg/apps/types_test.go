package apps

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Trade Calculator", "trade_calculator"},
		{"Sales Dashboard", "sales_dashboard"},
		{"my-cool-app", "my_cool_app"},
		{"  spaced  ", "spaced"},
		{"UPPERCASE", "uppercase"},
		{"app v2.1", "app_v2_1"},
		{"", "untitled_app"},
		{"  ", "untitled_app"},
		{"special!@#chars$%", "special_chars"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Slugify(tt.input)
			if got != tt.expected {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestVisualAppYAMLRoundTrip(t *testing.T) {
	app := VisualApp{
		Name:        "Test App",
		Description: "A test application",
		Code:        "export default function App() { return <div>Hello</div>; }",
		Version:     3,
		SessionID:   "session-123",
		CreatedAt:   time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 4, 20, 10, 30, 0, 0, time.UTC),
	}

	data, err := yaml.Marshal(&app)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var loaded VisualApp
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if loaded.Name != app.Name {
		t.Errorf("Name = %q, want %q", loaded.Name, app.Name)
	}
	if loaded.Code != app.Code {
		t.Errorf("Code mismatch")
	}
	if loaded.Version != 3 {
		t.Errorf("Version = %d, want 3", loaded.Version)
	}
	if loaded.SessionID != "session-123" {
		t.Errorf("SessionID = %q, want %q", loaded.SessionID, "session-123")
	}
}

func TestSaveLoadDeleteOnDisk(t *testing.T) {
	tmpDir := t.TempDir()

	app := &VisualApp{
		Name:        "My Test App",
		Description: "Testing save/load/delete",
		Code:        "function App() { return <div>Test</div>; }\nexport default App;",
		Version:     1,
		SessionID:   "sess-abc",
	}

	// Marshal and write
	data, err := yaml.Marshal(app)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	filename := Slugify(app.Name) + ".yaml"
	path := filepath.Join(tmpDir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Read back
	readData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var loaded VisualApp
	if err := yaml.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if loaded.Name != app.Name {
		t.Errorf("Name = %q, want %q", loaded.Name, app.Name)
	}
	if loaded.Version != 1 {
		t.Errorf("Version = %d, want 1", loaded.Version)
	}

	// List YAML files in dir
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	yamlCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".yaml" {
			yamlCount++
		}
	}
	if yamlCount != 1 {
		t.Errorf("expected 1 yaml file, got %d", yamlCount)
	}

	// Delete
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should not exist after delete")
	}
}

func TestSlugifyFilename(t *testing.T) {
	// Ensure slugified names produce valid filenames
	names := []string{
		"Trade Calculator",
		"My App (v2)",
		"sales-dashboard",
		"App with Spaces and 123 Numbers",
	}
	for _, name := range names {
		slug := Slugify(name)
		filename := slug + ".yaml"
		// Should not contain path separators or spaces
		if filepath.Base(filename) != filename {
			t.Errorf("Slugify(%q) produced path-unsafe filename %q", name, filename)
		}
		for _, c := range slug {
			if c == ' ' || c == '/' || c == '\\' {
				t.Errorf("Slugify(%q) contains invalid char %q", name, string(c))
			}
		}
	}
}
