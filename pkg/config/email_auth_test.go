package config

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEmailAuthConfig(t *testing.T) {
	for _, value := range []string{"", "true", "false"} {
		t.Run("require_"+value, func(t *testing.T) {
			input := "trusted_authserv_id: mx.example.com\n"
			if value != "" {
				input += "require_sender_auth: " + value + "\n"
			}
			var cfg EmailConfig
			if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.TrustedAuthservID != "mx.example.com" {
				t.Fatal("missing trusted receiver")
			}
			if (cfg.RequireSenderAuth == nil) != (value == "") {
				t.Fatal("lost explicit/default distinction")
			}
			if cfg.RequireSenderAuth != nil && *cfg.RequireSenderAuth != (value == "true") {
				t.Fatal("wrong auth setting")
			}
			b, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var restored EmailConfig
			if err := json.Unmarshal(b, &restored); err != nil {
				t.Fatal(err)
			}
			if restored.TrustedAuthservID != cfg.TrustedAuthservID || (restored.RequireSenderAuth == nil) != (cfg.RequireSenderAuth == nil) {
				t.Fatal("JSON lost trust settings")
			}
			if restored.RequireSenderAuth != nil && *restored.RequireSenderAuth != *cfg.RequireSenderAuth {
				t.Fatal("JSON changed auth setting")
			}
			b, err = yaml.Marshal(restored)
			if err != nil {
				t.Fatal(err)
			}
			var roundtrip EmailConfig
			if err := yaml.Unmarshal(b, &roundtrip); err != nil {
				t.Fatal(err)
			}
			if roundtrip.TrustedAuthservID != cfg.TrustedAuthservID || (roundtrip.RequireSenderAuth == nil) != (cfg.RequireSenderAuth == nil) {
				t.Fatal("YAML lost trust settings")
			}
			if roundtrip.RequireSenderAuth != nil && *roundtrip.RequireSenderAuth != *cfg.RequireSenderAuth {
				t.Fatal("YAML changed auth setting")
			}
		})
	}
}

func TestGetSenderAuthMode(t *testing.T) {
	if (&EmailConfig{}).GetSenderAuthMode() != EmailSenderAuthModeStrict {
		t.Fatal("empty config must default to strict")
	}
	if (&EmailConfig{Provider: "msgraph"}).GetSenderAuthMode() != EmailSenderAuthModeStrict {
		t.Fatal("Microsoft Graph must not silently enable provider-trust")
	}
	if (&EmailConfig{SenderAuthMode: EmailSenderAuthModeProviderTrust}).GetSenderAuthMode() != EmailSenderAuthModeProviderTrust {
		t.Fatal("explicit provider-trust was ignored")
	}
	disabled := false
	if (&EmailConfig{RequireSenderAuth: &disabled}).GetSenderAuthMode() != EmailSenderAuthModeProviderTrust {
		t.Fatal("legacy require_sender_auth:false must map to provider-trust")
	}
}
