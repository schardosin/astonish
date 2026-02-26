package browser

import (
	"strings"
	"testing"
)

func TestCAPTCHATypeFromString(t *testing.T) {
	tests := []struct {
		input string
		want  CAPTCHAType
	}{
		{"recaptcha_v2", CAPTCHAReCaptchaV2},
		{"RECAPTCHA_V2", CAPTCHAReCaptchaV2},
		{"recaptcha_v3", CAPTCHAReCaptchaV3},
		{"hcaptcha", CAPTCHAHCaptcha},
		{"HCAPTCHA", CAPTCHAHCaptcha},
		{"cloudflare_turnstile", CAPTCHACloudflareTurnstile},
		{"unknown", CAPTCHAUnknown},
		{"", CAPTCHANone},
		{"bogus", CAPTCHANone},
		{"recaptcha", CAPTCHANone}, // Must be exact match
	}

	for _, tt := range tests {
		got := CAPTCHATypeFromString(tt.input)
		if got != tt.want {
			t.Errorf("CAPTCHATypeFromString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCAPTCHATypeConstants(t *testing.T) {
	// Verify constant values are stable (used in JSON serialization)
	if CAPTCHANone != "" {
		t.Errorf("CAPTCHANone should be empty string, got %q", CAPTCHANone)
	}
	if CAPTCHAReCaptchaV2 != "recaptcha_v2" {
		t.Errorf("CAPTCHAReCaptchaV2 = %q", CAPTCHAReCaptchaV2)
	}
	if CAPTCHAReCaptchaV3 != "recaptcha_v3" {
		t.Errorf("CAPTCHAReCaptchaV3 = %q", CAPTCHAReCaptchaV3)
	}
	if CAPTCHAHCaptcha != "hcaptcha" {
		t.Errorf("CAPTCHAHCaptcha = %q", CAPTCHAHCaptcha)
	}
	if CAPTCHACloudflareTurnstile != "cloudflare_turnstile" {
		t.Errorf("CAPTCHACloudflareTurnstile = %q", CAPTCHACloudflareTurnstile)
	}
	if CAPTCHAUnknown != "unknown" {
		t.Errorf("CAPTCHAUnknown = %q", CAPTCHAUnknown)
	}
}

func TestCAPTCHADetectionStruct(t *testing.T) {
	// Verify the detection struct works correctly
	det := CAPTCHADetection{
		Found:    true,
		Type:     CAPTCHAReCaptchaV2,
		SiteKey:  "6LeIxAcTAAAAAJcZVRqyHh71UMIEGNQ_MXjiZKhI",
		Selector: "div.g-recaptcha",
		Details:  "Google reCAPTCHA v2 detected",
	}

	if !det.Found {
		t.Error("expected Found=true")
	}
	if det.Type != CAPTCHAReCaptchaV2 {
		t.Errorf("expected recaptcha_v2, got %s", det.Type)
	}
	if det.SiteKey == "" {
		t.Error("expected non-empty SiteKey")
	}
}

func TestNewCAPTCHASolver_EmptyProvider(t *testing.T) {
	_, err := NewCAPTCHASolver(CAPTCHASolverConfig{})
	if err == nil {
		t.Fatal("expected error for empty provider")
	}
	if !strings.Contains(err.Error(), "no CAPTCHA solver configured") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewCAPTCHASolver_TwoCaptcha(t *testing.T) {
	_, err := NewCAPTCHASolver(CAPTCHASolverConfig{Provider: "2captcha", APIKey: "test"})
	if err == nil {
		t.Fatal("expected error for unimplemented 2captcha")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewCAPTCHASolver_AntiCaptcha(t *testing.T) {
	_, err := NewCAPTCHASolver(CAPTCHASolverConfig{Provider: "anti-captcha", APIKey: "test"})
	if err == nil {
		t.Fatal("expected error for unimplemented anti-captcha")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewCAPTCHASolver_Capsolver(t *testing.T) {
	_, err := NewCAPTCHASolver(CAPTCHASolverConfig{Provider: "capsolver", APIKey: "test"})
	if err == nil {
		t.Fatal("expected error for unimplemented capsolver")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewCAPTCHASolver_UnknownProvider(t *testing.T) {
	_, err := NewCAPTCHASolver(CAPTCHASolverConfig{Provider: "bogus-solver"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unknown CAPTCHA solver provider") {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "bogus-solver") {
		t.Errorf("error should contain provider name, got: %v", err)
	}
}

func TestCAPTCHASolverConfig_ZeroValue(t *testing.T) {
	var cfg CAPTCHASolverConfig
	if cfg.Provider != "" {
		t.Error("expected empty provider on zero value")
	}
	if cfg.APIKey != "" {
		t.Error("expected empty API key on zero value")
	}
}
