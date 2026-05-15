package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestImagePullPolicy(t *testing.T) {
	tests := []struct {
		name   string
		image  string
		expect corev1.PullPolicy
	}{
		// Mutable tags → Always.
		{
			name:   "dev tag",
			image:  "schardosin/astonish-sandbox-base:dev",
			expect: corev1.PullAlways,
		},
		{
			name:   "latest tag",
			image:  "docker.io/schardosin/astonish-sandbox-base:latest",
			expect: corev1.PullAlways,
		},
		{
			name:   "edge tag",
			image:  "ghcr.io/org/image:edge",
			expect: corev1.PullAlways,
		},
		{
			name:   "nightly tag",
			image:  "registry.example.com:5000/repo:nightly",
			expect: corev1.PullAlways,
		},
		{
			name:   "no tag implies latest",
			image:  "schardosin/astonish-sandbox-base",
			expect: corev1.PullAlways,
		},

		// Immutable tags → IfNotPresent.
		{
			name:   "semver tag",
			image:  "schardosin/astonish-sandbox-base:v1.2.3",
			expect: corev1.PullIfNotPresent,
		},
		{
			name:   "sha tag",
			image:  "schardosin/astonish-sandbox-base:abc1234",
			expect: corev1.PullIfNotPresent,
		},
		{
			name:   "release tag",
			image:  "docker.io/schardosin/astonish-sandbox-base:2.9.0",
			expect: corev1.PullIfNotPresent,
		},

		// Digest-pinned → IfNotPresent.
		{
			name:   "digest pinned with tag",
			image:  "schardosin/astonish-sandbox-base:dev@sha256:72740bb3e30ac977838be51d5f43f08d934f9fe89432493797eb64cb3113caf7",
			expect: corev1.PullIfNotPresent,
		},
		{
			name:   "digest pinned without tag",
			image:  "schardosin/astonish-sandbox-base@sha256:72740bb3e30ac977838be51d5f43f08d934f9fe89432493797eb64cb3113caf7",
			expect: corev1.PullIfNotPresent,
		},

		// Registry with port — should still detect tag correctly.
		{
			name:   "registry with port and dev tag",
			image:  "registry.local:5000/sandbox:dev",
			expect: corev1.PullAlways,
		},
		{
			name:   "registry with port and semver tag",
			image:  "registry.local:5000/sandbox:v2.0.0",
			expect: corev1.PullIfNotPresent,
		},
		{
			name:   "registry with port no tag",
			image:  "registry.local:5000/sandbox",
			expect: corev1.PullAlways,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := imagePullPolicy(tt.image)
			if got != tt.expect {
				t.Errorf("imagePullPolicy(%q) = %q, want %q", tt.image, got, tt.expect)
			}
		})
	}
}
