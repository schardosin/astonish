package main

import (
	"testing"
)

func TestBuildLowerDir(t *testing.T) {
	tests := []struct {
		name      string
		chain     string
		layersDir string
		want      string
		wantErr   bool
	}{
		{
			name:      "single layer",
			chain:     "@base",
			layersDir: "/mnt/layers",
			want:      "/mnt/layers/@base/rootfs",
		},
		{
			name:      "multiple layers oldest-first",
			chain:     "@base,admin-layer,team-template",
			layersDir: "/mnt/astonish-layers",
			want:      "/mnt/astonish-layers/team-template/rootfs:/mnt/astonish-layers/admin-layer/rootfs:/mnt/astonish-layers/@base/rootfs",
		},
		{
			name:      "whitespace trimmed",
			chain:     " @base , admin , team ",
			layersDir: "/mnt/layers",
			want:      "/mnt/layers/team/rootfs:/mnt/layers/admin/rootfs:/mnt/layers/@base/rootfs",
		},
		{
			name:    "empty chain",
			chain:   "",
			wantErr: true,
		},
		{
			name:    "only commas",
			chain:   ",,",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildLowerDir(tc.chain, tc.layersDir)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("buildLowerDir(%q, %q)\n  got:  %s\n  want: %s", tc.chain, tc.layersDir, got, tc.want)
			}
		})
	}
}

func TestBuildSupervisorArgs(t *testing.T) {
	// Default: no env → nil args.
	t.Setenv("ASTONISH_SUPERVISOR_ARGS", "")
	args := buildSupervisorArgs()
	if args != nil {
		t.Errorf("expected nil, got %v", args)
	}

	// With env.
	t.Setenv("ASTONISH_SUPERVISOR_ARGS", "--log-level debug --config /etc/openshell.yaml")
	args = buildSupervisorArgs()
	want := []string{"--log-level", "debug", "--config", "/etc/openshell.yaml"}
	if len(args) != len(want) {
		t.Fatalf("len(args) = %d, want %d", len(args), len(want))
	}
	for i, a := range args {
		if a != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, a, want[i])
		}
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("TEST_ASTONISH_BOOT_VAR", "custom")
	if got := envOr("TEST_ASTONISH_BOOT_VAR", "default"); got != "custom" {
		t.Errorf("envOr with set var = %q, want %q", got, "custom")
	}

	t.Setenv("TEST_ASTONISH_BOOT_VAR", "")
	if got := envOr("TEST_ASTONISH_BOOT_VAR", "fallback"); got != "fallback" {
		t.Errorf("envOr with empty var = %q, want %q", got, "fallback")
	}
}
