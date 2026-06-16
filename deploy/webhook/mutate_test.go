package main

import (
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestMutate_AstonishPod(t *testing.T) {
	cfg := webhookConfig{
		LayersPVCName:  "astonish-layers",
		UppersPVCName:  "astonish-uppers",
		InjectSysAdmin: true,
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sandbox-abc123",
			Namespace: "astonish-sandboxes",
			Labels: map[string]string{
				"astonish.io/type": "session",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "sandbox",
					Image: "schardosin/astonish-sandbox-openshell:latest",
				},
			},
		},
	}

	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatalf("marshal pod: %v", err)
	}

	req := &admissionv1.AdmissionRequest{
		UID: "test-uid",
		Object: runtime.RawExtension{
			Raw: raw,
		},
	}

	resp := mutate(req, cfg)
	if !resp.Allowed {
		t.Fatalf("expected allowed, got denied: %v", resp.Result)
	}
	if resp.PatchType == nil {
		t.Fatal("expected patch type to be set")
	}
	if *resp.PatchType != admissionv1.PatchTypeJSONPatch {
		t.Fatalf("expected JSONPatch type, got %v", *resp.PatchType)
	}

	var patches []jsonPatch
	if err := json.Unmarshal(resp.Patch, &patches); err != nil {
		t.Fatalf("unmarshal patches: %v", err)
	}

	// Verify we have the expected patches:
	// - volumes array init (1)
	// - 4 volume additions
	// - volumeMounts array init (1)
	// - 4 mount additions
	// - securityContext with SYS_ADMIN (1)
	// Total: 11
	if len(patches) < 10 {
		t.Errorf("expected at least 10 patches, got %d: %+v", len(patches), patches)
	}

	// Check that volumes contain our PVCs.
	foundLayers := false
	foundUppers := false
	foundOverlay := false
	foundRuntime := false

	for _, p := range patches {
		if p.Path == "/spec/volumes/-" {
			raw, _ := json.Marshal(p.Value)
			var vol corev1.Volume
			if err := json.Unmarshal(raw, &vol); err != nil {
				continue
			}
			switch vol.Name {
			case "astonish-layers":
				foundLayers = true
				if vol.PersistentVolumeClaim == nil || vol.PersistentVolumeClaim.ClaimName != "astonish-layers" {
					t.Error("astonish-layers volume has wrong PVC name")
				}
			case "astonish-uppers":
				foundUppers = true
				if vol.PersistentVolumeClaim == nil || vol.PersistentVolumeClaim.ClaimName != "astonish-uppers" {
					t.Error("astonish-uppers volume has wrong PVC name")
				}
			case "astonish-overlay":
				foundOverlay = true
				if vol.EmptyDir == nil {
					t.Error("astonish-overlay should be emptyDir")
				}
			case "openshell-runtime":
				foundRuntime = true
				if vol.EmptyDir == nil {
					t.Error("openshell-runtime should be emptyDir")
				}
			}
		}
	}

	if !foundLayers {
		t.Error("missing astonish-layers volume patch")
	}
	if !foundUppers {
		t.Error("missing astonish-uppers volume patch")
	}
	if !foundOverlay {
		t.Error("missing astonish-overlay volume patch")
	}
	if !foundRuntime {
		t.Error("missing openshell-runtime volume patch")
	}
}

func TestMutate_NonAstonishPod(t *testing.T) {
	cfg := webhookConfig{
		LayersPVCName: "astonish-layers",
		UppersPVCName: "astonish-uppers",
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-other-pod",
			Namespace: "default",
			Labels:    map[string]string{},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "nginx:latest"},
			},
		},
	}

	raw, _ := json.Marshal(pod)
	req := &admissionv1.AdmissionRequest{
		UID:    "test-uid-2",
		Object: runtime.RawExtension{Raw: raw},
	}

	resp := mutate(req, cfg)
	if !resp.Allowed {
		t.Fatal("expected allowed for non-astonish pod")
	}
	if resp.Patch != nil {
		t.Error("expected no patches for non-astonish pod")
	}
}

func TestMutate_WithFuseDevice(t *testing.T) {
	cfg := webhookConfig{
		LayersPVCName:      "astonish-layers",
		UppersPVCName:      "astonish-uppers",
		FuseDeviceResource: "smarter-devices/fuse",
		InjectSysAdmin:     true,
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "sandbox-fuse",
			Labels: map[string]string{"astonish.io/type": "session"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "sandbox", Image: "test:latest"},
			},
		},
	}

	raw, _ := json.Marshal(pod)
	req := &admissionv1.AdmissionRequest{
		UID:    "test-uid-3",
		Object: runtime.RawExtension{Raw: raw},
	}

	resp := mutate(req, cfg)
	if !resp.Allowed {
		t.Fatal("expected allowed")
	}

	var patches []jsonPatch
	_ = json.Unmarshal(resp.Patch, &patches)

	// Should have resource limit and request patches for the FUSE device.
	foundFuseLimit := false
	for _, p := range patches {
		if p.Path == "/spec/containers/0/resources/limits/smarter-devices~1fuse" {
			foundFuseLimit = true
		}
	}
	if !foundFuseLimit {
		t.Error("missing FUSE device resource limit patch")
	}
}

func TestMutate_ExistingSecurityContext(t *testing.T) {
	cfg := webhookConfig{
		LayersPVCName:  "astonish-layers",
		UppersPVCName:  "astonish-uppers",
		InjectSysAdmin: true,
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "sandbox-existing-sec",
			Labels: map[string]string{"astonish.io/type": "session"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "sandbox",
					Image: "test:latest",
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{
							Add: []corev1.Capability{"NET_ADMIN"},
						},
					},
				},
			},
		},
	}

	raw, _ := json.Marshal(pod)
	req := &admissionv1.AdmissionRequest{
		UID:    "test-uid-4",
		Object: runtime.RawExtension{Raw: raw},
	}

	resp := mutate(req, cfg)
	if !resp.Allowed {
		t.Fatal("expected allowed")
	}

	var patches []jsonPatch
	_ = json.Unmarshal(resp.Patch, &patches)

	// Should append SYS_ADMIN to existing capabilities.add
	foundCapPatch := false
	for _, p := range patches {
		if p.Path == "/spec/containers/0/securityContext/capabilities/add/-" {
			foundCapPatch = true
		}
	}
	if !foundCapPatch {
		t.Error("missing SYS_ADMIN capability append patch")
	}
}

func TestEscapeJSONPointer(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"simple", "simple"},
		{"smarter-devices/fuse", "smarter-devices~1fuse"},
		{"a~b", "a~0b"},
		{"a/b~c/d", "a~1b~0c~1d"},
	}
	for _, tc := range cases {
		got := escapeJSONPointer(tc.in)
		if got != tc.want {
			t.Errorf("escapeJSONPointer(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
