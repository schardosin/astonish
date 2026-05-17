package baseconfig

import "testing"

func TestValidate_ValidDefault(t *testing.T) {
	cfg := DefaultBaseConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultBaseConfig should be valid, got: %v", err)
	}
}

func TestValidate_InvalidArch(t *testing.T) {
	cfg := BaseConfig{Core: true, Architecture: "mips"}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid architecture")
	}
}

func TestValidate_InvalidEngine(t *testing.T) {
	cfg := BaseConfig{
		Core:         true,
		Architecture: "amd64",
		Browser:      BrowserConfig{Engine: "firefox"},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid browser engine")
	}
}

func TestValidate_InvalidToolID(t *testing.T) {
	cfg := BaseConfig{
		Core:          true,
		Architecture:  "amd64",
		OptionalTools: []string{"nonexistent-tool"},
		Browser:       BrowserConfig{Engine: "none"},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for unknown optional tool ID")
	}
}

func TestValidate_ValidToolID(t *testing.T) {
	cfg := BaseConfig{
		Core:          true,
		Architecture:  "amd64",
		OptionalTools: []string{"opencode"},
		Browser:       BrowserConfig{Engine: "none"},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config, got: %v", err)
	}
}

func TestValidate_InvalidFingerprintPlatform(t *testing.T) {
	cfg := BaseConfig{
		Core:         true,
		Architecture: "amd64",
		Browser: BrowserConfig{
			Engine:              "cloakbrowser",
			FingerprintPlatform: "android",
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid fingerprint platform")
	}
}

func TestValidate_EmptyArchDefaultsOK(t *testing.T) {
	cfg := BaseConfig{
		Core:    true,
		Browser: BrowserConfig{Engine: "none"},
	}
	// Empty architecture is allowed (will default at render time)
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config with empty arch, got: %v", err)
	}
}
