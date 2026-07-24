package backup

import (
	"errors"
	"testing"
	"time"
)

func TestManifestValidate(t *testing.T) {
	manifest := NewManifest("sqlite", "logical", []Scope{{Kind: "team", OrgSlug: "acme", TeamSlug: "sre"}})
	manifest.Entries = []Entry{{
		Path:    "orgs/acme/teams/sre/sessions.jsonl",
		Kind:    "jsonl",
		Scope:   Scope{Kind: "team", OrgSlug: "acme", TeamSlug: "sre"},
		Entity:  "sessions",
		Records: 2,
	}}

	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestManifestValidateRejectsUnknownCompression(t *testing.T) {
	manifest := NewManifest("sqlite", "logical", []Scope{{Kind: "platform"}})
	manifest.Compression = "zip"

	if err := manifest.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want unsupported compression error")
	}
}

func TestManifestValidateRejectsUnsupportedVersion(t *testing.T) {
	manifest := NewManifest("sqlite", "logical", nil)
	manifest.FormatVersion = ArchiveFormatVersion + 1

	err := manifest.Validate()
	if !errors.Is(err, ErrUnsupportedArchiveVersion) {
		t.Fatalf("Validate() error = %v, want ErrUnsupportedArchiveVersion", err)
	}
}

func TestScopeValidate(t *testing.T) {
	tests := []struct {
		name    string
		scope   Scope
		wantErr bool
	}{
		{name: "platform", scope: Scope{Kind: "platform"}},
		{name: "org", scope: Scope{Kind: "org", OrgSlug: "acme"}},
		{name: "team", scope: Scope{Kind: "team", OrgSlug: "acme", TeamSlug: "sre"}},
		{name: "personal", scope: Scope{Kind: "personal", OrgSlug: "acme", UserID: "u1"}},
		{name: "team missing org", scope: Scope{Kind: "team", TeamSlug: "sre"}, wantErr: true},
		{name: "unknown", scope: Scope{Kind: "project"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.scope.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEntryValidateRejectsUnsafePath(t *testing.T) {
	entry := Entry{
		Path:  "../secrets.jsonl",
		Kind:  "jsonl",
		Scope: Scope{Kind: "platform"},
	}
	if err := entry.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want unsafe path error")
	}
}

func TestManifestCheckCompatible(t *testing.T) {
	manifest := NewManifest("postgres", "logical", nil)
	manifest.CreatedAt = time.Now()
	manifest.SchemaVersions = map[string]SchemaVersion{
		"platform": {Scope: "platform", Version: "202607230001"},
	}

	if err := manifest.CheckCompatible(TargetCompatibility{
		FormatVersion: ArchiveFormatVersion,
		SchemaVersions: map[string]string{
			"platform": "202607230002",
		},
	}); err != nil {
		t.Fatalf("CheckCompatible() error = %v", err)
	}

	err := manifest.CheckCompatible(TargetCompatibility{
		FormatVersion: ArchiveFormatVersion,
		SchemaVersions: map[string]string{
			"platform": "202607230000",
		},
	})
	if err == nil {
		t.Fatal("CheckCompatible() error = nil, want older target schema error")
	}
}
