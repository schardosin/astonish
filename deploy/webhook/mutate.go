package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// handleMutate returns an HTTP handler for the /mutate endpoint.
func handleMutate(cfg webhookConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			slog.Error("read body", "err", err)
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}

		var review admissionv1.AdmissionReview
		if err := json.Unmarshal(body, &review); err != nil {
			slog.Error("unmarshal review", "err", err)
			http.Error(w, "invalid admission review", http.StatusBadRequest)
			return
		}

		if review.Request == nil {
			http.Error(w, "missing request in review", http.StatusBadRequest)
			return
		}

		response := mutate(review.Request, cfg)
		review.Response = response
		review.Response.UID = review.Request.UID

		respBytes, err := json.Marshal(review)
		if err != nil {
			slog.Error("marshal response", "err", err)
			http.Error(w, "marshal response failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBytes)
	}
}

// mutate processes the admission request and returns the response with
// JSON patches to inject Astonish volumes into the pod.
func mutate(req *admissionv1.AdmissionRequest, cfg webhookConfig) *admissionv1.AdmissionResponse {
	// Decode the pod.
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		slog.Error("unmarshal pod", "err", err)
		return denyResponse(fmt.Sprintf("unmarshal pod: %v", err))
	}

	// Only mutate pods with astonish.io/type label.
	if _, ok := pod.Labels["astonish.io/type"]; !ok {
		slog.Debug("pod missing astonish.io/type label, allowing without mutation",
			"name", pod.Name, "namespace", pod.Namespace)
		return allowResponse()
	}

	slog.Info("mutating pod", "name", pod.Name, "namespace", pod.Namespace,
		"type", pod.Labels["astonish.io/type"])

	patches := buildPatches(cfg, &pod)

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		slog.Error("marshal patches", "err", err)
		return denyResponse(fmt.Sprintf("marshal patches: %v", err))
	}

	patchType := admissionv1.PatchTypeJSONPatch
	return &admissionv1.AdmissionResponse{
		Allowed:   true,
		Patch:     patchBytes,
		PatchType: &patchType,
	}
}

// jsonPatch represents a single RFC 6902 JSON Patch operation.
type jsonPatch struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// buildPatches constructs the JSON Patch operations to inject volumes,
// mounts, FUSE resources, and CAP_SYS_ADMIN into the pod.
func buildPatches(cfg webhookConfig, pod *corev1.Pod) []jsonPatch {
	var patches []jsonPatch

	// --- Volumes ---
	// If the pod has no volumes yet, we need to create the array first.
	if len(pod.Spec.Volumes) == 0 {
		patches = append(patches, jsonPatch{
			Op:    "add",
			Path:  "/spec/volumes",
			Value: []interface{}{},
		})
	}

	volumes := []corev1.Volume{
		{
			Name: "astonish-layers",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: cfg.LayersPVCName,
				},
			},
		},
		{
			Name: "astonish-uppers",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: cfg.UppersPVCName,
				},
			},
		},
		{
			Name: "astonish-overlay",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: "openshell-runtime",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	for _, v := range volumes {
		patches = append(patches, jsonPatch{
			Op:    "add",
			Path:  "/spec/volumes/-",
			Value: v,
		})
	}

	// --- Volume Mounts (first container) ---
	if len(pod.Spec.Containers) > 0 {
		containerIdx := 0
		basePath := fmt.Sprintf("/spec/containers/%d", containerIdx)

		// Ensure volumeMounts array exists.
		if len(pod.Spec.Containers[containerIdx].VolumeMounts) == 0 {
			patches = append(patches, jsonPatch{
				Op:    "add",
				Path:  basePath + "/volumeMounts",
				Value: []interface{}{},
			})
		}

		mounts := []corev1.VolumeMount{
			{Name: "astonish-layers", MountPath: "/mnt/astonish-layers"},
			{Name: "astonish-uppers", MountPath: "/mnt/astonish-uppers"},
			{Name: "astonish-overlay", MountPath: "/overlay"},
			{Name: "openshell-runtime", MountPath: "/var/run/openshell"},
		}

		for _, m := range mounts {
			patches = append(patches, jsonPatch{
				Op:    "add",
				Path:  basePath + "/volumeMounts/-",
				Value: m,
			})
		}

		// --- FUSE device resource ---
		if cfg.FuseDeviceResource != "" {
			patches = append(patches, ensureResourcePatches(
				basePath, pod.Spec.Containers[containerIdx],
				cfg.FuseDeviceResource, "1",
			)...)
		}

		// --- CAP_SYS_ADMIN ---
		if cfg.InjectSysAdmin {
			patches = append(patches, ensureCapabilityPatches(
				basePath, pod.Spec.Containers[containerIdx],
				"SYS_ADMIN",
			)...)
		}
	}

	return patches
}

// ensureResourcePatches returns patches that add a resource limit+request
// for the given resource name if not already present.
func ensureResourcePatches(basePath string, c corev1.Container, resName, qty string) []jsonPatch {
	var patches []jsonPatch
	q := resource.MustParse(qty)

	// Ensure resources.limits map exists.
	if c.Resources.Limits == nil {
		patches = append(patches, jsonPatch{
			Op:    "add",
			Path:  basePath + "/resources/limits",
			Value: corev1.ResourceList{},
		})
	}
	patches = append(patches, jsonPatch{
		Op:    "add",
		Path:  basePath + "/resources/limits/" + escapeJSONPointer(resName),
		Value: q.String(),
	})

	// Ensure resources.requests map exists.
	if c.Resources.Requests == nil {
		patches = append(patches, jsonPatch{
			Op:    "add",
			Path:  basePath + "/resources/requests",
			Value: corev1.ResourceList{},
		})
	}
	patches = append(patches, jsonPatch{
		Op:    "add",
		Path:  basePath + "/resources/requests/" + escapeJSONPointer(resName),
		Value: q.String(),
	})

	return patches
}

// ensureCapabilityPatches returns patches that add the specified capability
// to the container's securityContext.capabilities.add list.
func ensureCapabilityPatches(basePath string, c corev1.Container, cap string) []jsonPatch {
	var patches []jsonPatch

	// Build the path incrementally, creating each level if absent.
	if c.SecurityContext == nil {
		patches = append(patches, jsonPatch{
			Op:   "add",
			Path: basePath + "/securityContext",
			Value: corev1.SecurityContext{
				Capabilities: &corev1.Capabilities{
					Add: []corev1.Capability{corev1.Capability(cap)},
				},
			},
		})
		return patches
	}

	if c.SecurityContext.Capabilities == nil {
		patches = append(patches, jsonPatch{
			Op:   "add",
			Path: basePath + "/securityContext/capabilities",
			Value: corev1.Capabilities{
				Add: []corev1.Capability{corev1.Capability(cap)},
			},
		})
		return patches
	}

	// Check if cap already exists.
	for _, existing := range c.SecurityContext.Capabilities.Add {
		if string(existing) == cap {
			return patches // Already present.
		}
	}

	if len(c.SecurityContext.Capabilities.Add) == 0 {
		patches = append(patches, jsonPatch{
			Op:    "add",
			Path:  basePath + "/securityContext/capabilities/add",
			Value: []corev1.Capability{corev1.Capability(cap)},
		})
	} else {
		patches = append(patches, jsonPatch{
			Op:    "add",
			Path:  basePath + "/securityContext/capabilities/add/-",
			Value: corev1.Capability(cap),
		})
	}

	return patches
}

// escapeJSONPointer escapes special characters in JSON Pointer tokens
// per RFC 6901: '~' → '~0', '/' → '~1'.
func escapeJSONPointer(s string) string {
	result := ""
	for _, c := range s {
		switch c {
		case '~':
			result += "~0"
		case '/':
			result += "~1"
		default:
			result += string(c)
		}
	}
	return result
}

func allowResponse() *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{Allowed: true}
}

func denyResponse(reason string) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Allowed: false,
		Result: &metav1.Status{
			Message: reason,
		},
	}
}
