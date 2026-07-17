package imagebuilder

import (
	"strings"
	"testing"
)

func TestGenerateDockerfile(t *testing.T) {
	tests := []struct {
		name         string
		baseImage    string
		platformBody string
		teamBody     string
		wantLines    []string
	}{
		{
			name:         "platform only",
			baseImage:    "ghcr.io/sap/astonish-sandbox-openshell:dev",
			platformBody: "USER root\nRUN apt-get update && apt-get install -y curl\nUSER sandbox",
			teamBody:     "",
			wantLines: []string{
				"FROM ghcr.io/sap/astonish-sandbox-openshell:dev",
				"# --- Platform recipe ---",
				"USER root",
				"RUN apt-get update && apt-get install -y curl",
				"USER sandbox",
			},
		},
		{
			name:         "team only",
			baseImage:    "ghcr.io/org/sandbox:v1",
			platformBody: "",
			teamBody:     "ENV MY_VAR=hello\nRUN pip install torch",
			wantLines: []string{
				"FROM ghcr.io/org/sandbox:v1",
				"# --- Team recipe ---",
				"ENV MY_VAR=hello",
				"RUN pip install torch",
			},
		},
		{
			name:         "both platform and team",
			baseImage:    "base:latest",
			platformBody: "RUN apt-get update && apt-get install -y git",
			teamBody:     "RUN pip install numpy",
			wantLines: []string{
				"FROM base:latest",
				"# --- Platform recipe ---",
				"RUN apt-get update && apt-get install -y git",
				"# --- Team recipe ---",
				"RUN pip install numpy",
			},
		},
		{
			name:         "bodies are trimmed",
			baseImage:    "base:latest",
			platformBody: "\n\n  RUN echo platform  \n\n",
			teamBody:     "\n  RUN echo team  \n",
			wantLines: []string{
				"FROM base:latest",
				"RUN echo platform",
				"RUN echo team",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateDockerfile(tt.baseImage, tt.platformBody, tt.teamBody)
			for _, want := range tt.wantLines {
				if !strings.Contains(result, want) {
					t.Errorf("GenerateDockerfile() missing expected content %q\n\nGot:\n%s", want, result)
				}
			}
		})
	}
}

func TestGenerateDockerfile_AlwaysStartsWithFROM(t *testing.T) {
	result := GenerateDockerfile("myimage:v2", "RUN echo platform", "RUN echo team")
	if !strings.HasPrefix(result, "FROM myimage:v2\n") {
		t.Errorf("Dockerfile should start with FROM line, got:\n%s", result)
	}
}

func TestGenerateDockerfile_PlatformBeforeTeam(t *testing.T) {
	result := GenerateDockerfile("base:latest", "RUN echo platform", "RUN echo team")
	platformIdx := strings.Index(result, "RUN echo platform")
	teamIdx := strings.Index(result, "RUN echo team")
	if platformIdx >= teamIdx {
		t.Errorf("Platform body should come before team body.\nGot:\n%s", result)
	}
}

func TestMigratePackagesToDockerfile(t *testing.T) {
	body := MigratePackagesToDockerfile([]string{"curl", "git", "jq"})
	for _, want := range []string{"USER root", "apt-get install", "curl", "git", "jq", "USER sandbox"} {
		if !strings.Contains(body, want) {
			t.Errorf("MigratePackagesToDockerfile() missing %q\n\nGot:\n%s", want, body)
		}
	}
}

func TestMigratePackagesToDockerfile_Empty(t *testing.T) {
	body := MigratePackagesToDockerfile(nil)
	if body != "" {
		t.Errorf("MigratePackagesToDockerfile(nil) = %q, want empty", body)
	}
}

func TestImageRef(t *testing.T) {
	tests := []struct {
		name         string
		registryURL  string
		scope        string
		combinedBody string
		wantPrefix   string
	}{
		{
			name:         "ghcr base",
			registryURL:  "ghcr.io/sap",
			scope:        "base",
			combinedBody: CombinedBody("RUN apt-get install -y curl git", ""),
			wantPrefix:   "ghcr.io/sap/astonish-sandbox-base:",
		},
		{
			name:         "ghcr team",
			registryURL:  "ghcr.io/org",
			scope:        "frontend",
			combinedBody: CombinedBody("", "RUN npm install -g typescript"),
			wantPrefix:   "ghcr.io/org/astonish-sandbox-frontend:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ImageRef(tt.registryURL, tt.scope, tt.combinedBody)
			if !strings.HasPrefix(result, tt.wantPrefix) {
				t.Errorf("ImageRef() = %q, want prefix %q", result, tt.wantPrefix)
			}
			// Tag should be 12 hex chars.
			tag := result[len(tt.wantPrefix):]
			if len(tag) != 12 {
				t.Errorf("ImageRef() tag = %q, want 12 chars", tag)
			}
		})
	}
}

func TestImageRefDeterministic(t *testing.T) {
	body := CombinedBody("RUN apt-get install -y curl git jq", "")
	ref1 := ImageRef("docker.io/test", "base", body)
	ref2 := ImageRef("docker.io/test", "base", body)
	if ref1 != ref2 {
		t.Errorf("ImageRef not deterministic: %q != %q", ref1, ref2)
	}
}

func TestImageRefDifferentBody(t *testing.T) {
	ref1 := ImageRef("docker.io/test", "base", CombinedBody("RUN echo hello", ""))
	ref2 := ImageRef("docker.io/test", "base", CombinedBody("RUN echo world", ""))
	if ref1 == ref2 {
		t.Error("ImageRef should produce different refs for different bodies")
	}
}

func TestCombinedBody(t *testing.T) {
	// Same combined body produces same hash regardless of how it's split
	body1 := CombinedBody("platform stuff", "team stuff")
	body2 := CombinedBody("platform stuff", "team stuff")
	if body1 != body2 {
		t.Error("CombinedBody not deterministic")
	}

	// Different platform body produces different combined
	body3 := CombinedBody("different platform", "team stuff")
	if body1 == body3 {
		t.Error("CombinedBody should differ when platform body differs")
	}

	// Different team body produces different combined
	body4 := CombinedBody("platform stuff", "different team")
	if body1 == body4 {
		t.Error("CombinedBody should differ when team body differs")
	}
}

func TestContentHash(t *testing.T) {
	h := ContentHash(CombinedBody("RUN echo hello", ""))
	if len(h) != 12 {
		t.Errorf("ContentHash() = %q, want 12 chars", h)
	}
	// Deterministic
	if ContentHash(CombinedBody("RUN echo hello", "")) != h {
		t.Error("ContentHash not deterministic")
	}
}

func TestJobName(t *testing.T) {
	name := JobName("my-team", CombinedBody("", "RUN apt-get install -y curl"))
	if len(name) > 63 {
		t.Errorf("JobName too long: %d chars", len(name))
	}
	if !strings.HasPrefix(name, "astonish-build-my-team-") {
		t.Errorf("JobName = %q, want prefix 'astonish-build-my-team-'", name)
	}
}

func TestJobNameDNSSafe(t *testing.T) {
	name := JobName("team@special.chars!", CombinedBody("RUN echo test", ""))
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			t.Errorf("JobName contains invalid DNS char %q in %q", string(c), name)
		}
	}
}

func TestSanitizeDNS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-team", "my-team"},
		{"team@org", "team-org"},
		{"UPPER", "upper"},
		{"a.b.c", "a-b-c"},
		{"---", "x"},
		{"", "x"},
	}
	for _, tt := range tests {
		got := sanitizeDNS(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeDNS(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidate(t *testing.T) {
	b := New(Config{})
	if err := b.Validate(); err == nil {
		t.Error("Validate() should fail with empty config")
	}
}

func TestIsConfigured(t *testing.T) {
	if IsConfigured("", "") {
		t.Error("IsConfigured(\"\", \"\") should be false")
	}
	if IsConfigured("url", "") {
		t.Error("IsConfigured(\"url\", \"\") should be false")
	}
	if !IsConfigured("url", "secret") {
		t.Error("IsConfigured(\"url\", \"secret\") should be true")
	}
}

// ---------------------------------------------------------------------------
// 3-Layer Image Model Invariants
//
// These tests ensure the image build model's structural properties hold:
// 1. Output always starts with exactly one FROM line
// 2. Platform recipe (Layer 2) always comes before team recipe (Layer 3)
// 3. Empty bodies produce no spurious content
// 4. Both bodies empty → only FROM line
// 5. Hash changes when either layer changes
// ---------------------------------------------------------------------------

func TestGenerateDockerfile_OnlyOneFROM(t *testing.T) {
	result := GenerateDockerfile("base:latest", "RUN echo platform", "RUN echo team")
	count := strings.Count(result, "FROM ")
	if count != 1 {
		t.Errorf("expected exactly 1 FROM, got %d.\nDockerfile:\n%s", count, result)
	}
}

func TestGenerateDockerfile_BothEmpty_OnlyFROM(t *testing.T) {
	result := GenerateDockerfile("base:latest", "", "")
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line (FROM only), got %d lines:\n%s", len(lines), result)
	}
	if !strings.HasPrefix(lines[0], "FROM base:latest") {
		t.Errorf("expected FROM base:latest, got %q", lines[0])
	}
}

func TestGenerateDockerfile_EmptyPlatform_NoHeader(t *testing.T) {
	result := GenerateDockerfile("base:latest", "", "RUN echo team")
	if strings.Contains(result, "Platform recipe") {
		t.Errorf("should not contain Platform recipe header when platform body is empty.\nGot:\n%s", result)
	}
	if !strings.Contains(result, "Team recipe") {
		t.Errorf("should contain Team recipe header.\nGot:\n%s", result)
	}
}

func TestGenerateDockerfile_EmptyTeam_NoHeader(t *testing.T) {
	result := GenerateDockerfile("base:latest", "RUN echo platform", "")
	if strings.Contains(result, "Team recipe") {
		t.Errorf("should not contain Team recipe header when team body is empty.\nGot:\n%s", result)
	}
	if !strings.Contains(result, "Platform recipe") {
		t.Errorf("should contain Platform recipe header.\nGot:\n%s", result)
	}
}

func TestCombinedBody_HashChangesWithPlatform(t *testing.T) {
	team := "RUN echo team"
	hash1 := ContentHash(CombinedBody("RUN echo platform-v1", team))
	hash2 := ContentHash(CombinedBody("RUN echo platform-v2", team))
	if hash1 == hash2 {
		t.Error("hash should change when platform body changes")
	}
}

func TestCombinedBody_HashChangesWithTeam(t *testing.T) {
	platform := "RUN echo platform"
	hash1 := ContentHash(CombinedBody(platform, "RUN echo team-v1"))
	hash2 := ContentHash(CombinedBody(platform, "RUN echo team-v2"))
	if hash1 == hash2 {
		t.Error("hash should change when team body changes")
	}
}

func TestCombinedBody_SameContentSameHash(t *testing.T) {
	// Verify determinism across calls.
	h1 := ContentHash(CombinedBody("RUN apt-get install git", "RUN pip install torch"))
	h2 := ContentHash(CombinedBody("RUN apt-get install git", "RUN pip install torch"))
	if h1 != h2 {
		t.Errorf("same content should produce same hash: %q != %q", h1, h2)
	}
}

func TestImageTag_ReflectsLayerChanges(t *testing.T) {
	b := New(Config{
		RegistryURL: "docker.io/test",
		SecretName:  "secret",
	})

	tag1 := b.ImageTag(BuildSpec{
		Scope:        "general",
		BaseImage:    "base:latest",
		PlatformBody: "RUN echo platform",
		TeamBody:     "RUN echo team-v1",
	})

	tag2 := b.ImageTag(BuildSpec{
		Scope:        "general",
		BaseImage:    "base:latest",
		PlatformBody: "RUN echo platform",
		TeamBody:     "RUN echo team-v2",
	})

	if tag1 == tag2 {
		t.Errorf("different team body should produce different image tag:\n  tag1=%s\n  tag2=%s", tag1, tag2)
	}
}

func TestImageTag_PlatformChangeAffectsTeamBuild(t *testing.T) {
	// When the platform recipe changes, team image tags must also change
	// (because the combined body changes).
	b := New(Config{
		RegistryURL: "docker.io/test",
		SecretName:  "secret",
	})

	tag1 := b.ImageTag(BuildSpec{
		Scope:        "general",
		BaseImage:    "base:latest",
		PlatformBody: "RUN echo platform-v1",
		TeamBody:     "RUN echo team",
	})

	tag2 := b.ImageTag(BuildSpec{
		Scope:        "general",
		BaseImage:    "base:latest",
		PlatformBody: "RUN echo platform-v2",
		TeamBody:     "RUN echo team",
	})

	if tag1 == tag2 {
		t.Errorf("platform change should produce different team image tag:\n  tag1=%s\n  tag2=%s", tag1, tag2)
	}
}
