// Package k8s — image reference helpers.

package k8s

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// mutableImageTags is the set of well-known mutable tags where
// kubelet's IfNotPresent policy would mask new digests behind the
// same tag. When the configured sandbox image uses one of these tags,
// we force PullAlways so dev pushes are picked up without manual
// node-side cache eviction.
var mutableImageTags = map[string]bool{
	"dev":    true,
	"latest": true,
	"edge":   true,
	"nightly": true,
}

// imagePullPolicy returns the appropriate corev1.PullPolicy for the
// given container image reference:
//
//   - If the image contains an "@sha256:" digest pin → IfNotPresent
//     (immutable by definition).
//   - If the tag portion is empty or matches a well-known mutable tag
//     (dev, latest, edge, nightly) → Always.
//   - Otherwise (e.g. ":v1.2.3") → IfNotPresent.
//
// This prevents the classic mutable-tag-with-IfNotPresent trap where
// a freshly pushed image is invisible to nodes that already cached
// the old digest under the same tag.
func imagePullPolicy(image string) corev1.PullPolicy {
	// Digest-pinned images are immutable; no need to re-pull.
	if strings.Contains(image, "@sha256:") {
		return corev1.PullIfNotPresent
	}

	// Extract tag: everything after the last ":" that is not part of
	// a port in the registry host (ports are before the first "/").
	tag := ""
	// Split off the registry+path from the tag.
	// The tag is the substring after the last ":" that occurs after
	// the last "/" (if any).
	lastSlash := strings.LastIndex(image, "/")
	tagPart := image
	if lastSlash >= 0 {
		tagPart = image[lastSlash:]
	}
	if idx := strings.LastIndex(tagPart, ":"); idx >= 0 {
		tag = tagPart[idx+1:]
	}

	// No tag specified (implies :latest) or known mutable tag.
	if tag == "" || mutableImageTags[tag] {
		return corev1.PullAlways
	}

	return corev1.PullIfNotPresent
}
