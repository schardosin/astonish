package openshell

import (
	"fmt"
	"path"
	"strings"

	"github.com/SAP/astonish/pkg/config"
)

// defaultTrustEnvVars are set to each cert bundle MountPath when TrustEnv
// is empty. SSL_CERT_FILE (and peers) replace the system store — operators
// must place a *combined* PEM (system CAs + corporate roots) in the PVC.
var defaultTrustEnvVars = []string{
	"SSL_CERT_FILE",
	"CURL_CA_BUNDLE",
	"REQUESTS_CA_BUNDLE",
	"NODE_EXTRA_CA_CERTS",
	"GIT_SSL_CAINFO",
}

// protectedMountPrefixes must not be used as CertBundle mount paths —
// they collide with OpenShell-managed material or the workspace root.
var protectedMountPrefixes = []string{
	"/sandbox",
	"/etc/openshell",
	"/etc/openshell-tls",
}

// driverConfigResult is the rendered OpenShell driver_config envelope plus
// trust env vars derived from CertBundles.
type driverConfigResult struct {
	// DriverConfig is nil when there are no cert bundles.
	DriverConfig map[string]any
	// TrustEnv maps env var name → MountPath.
	TrustEnv map[string]string
	// ExtraReadOnly are mount paths to append to the Landlock RO set.
	ExtraReadOnly []string
}

// renderDriverConfig builds the Kubernetes driver_config envelope and trust
// env vars from CertBundles. Returns a zero result when bundles is empty.
func renderDriverConfig(bundles []config.CertBundleConfig) (driverConfigResult, error) {
	if len(bundles) == 0 {
		return driverConfigResult{}, nil
	}

	volumes := make([]any, 0, len(bundles))
	mounts := make([]any, 0, len(bundles))
	trustEnv := make(map[string]string)
	extraRO := make([]string, 0, len(bundles))
	seenNames := make(map[string]struct{}, len(bundles))

	for i, b := range bundles {
		if err := validateCertBundle(b, i); err != nil {
			return driverConfigResult{}, err
		}
		if _, dup := seenNames[b.Name]; dup {
			return driverConfigResult{}, fmt.Errorf("cert_bundles[%d]: duplicate name %q", i, b.Name)
		}
		seenNames[b.Name] = struct{}{}

		volumes = append(volumes, map[string]any{
			"name": b.Name,
			"persistent_volume_claim": map[string]any{
				"claim_name": b.ClaimName,
				"read_only":  true,
			},
		})

		mount := map[string]any{
			"name":       b.Name,
			"mount_path": b.MountPath,
			"read_only":  true,
		}
		if b.SubPath != "" {
			mount["sub_path"] = b.SubPath
		}
		mounts = append(mounts, mount)

		extraRO = append(extraRO, b.MountPath)

		envKeys := b.TrustEnv
		if len(envKeys) == 0 {
			envKeys = defaultTrustEnvVars
		}
		for _, k := range envKeys {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			trustEnv[k] = b.MountPath
		}
	}

	return driverConfigResult{
		DriverConfig: map[string]any{
			"kubernetes": map[string]any{
				"volumes": volumes,
				"containers": map[string]any{
					"agent": map[string]any{
						"volume_mounts": mounts,
					},
				},
			},
		},
		TrustEnv:      trustEnv,
		ExtraReadOnly: extraRO,
	}, nil
}

func validateCertBundle(b config.CertBundleConfig, idx int) error {
	if strings.TrimSpace(b.Name) == "" {
		return fmt.Errorf("cert_bundles[%d]: name is required", idx)
	}
	if strings.TrimSpace(b.ClaimName) == "" {
		return fmt.Errorf("cert_bundles[%d]: claim_name is required", idx)
	}
	if strings.TrimSpace(b.MountPath) == "" {
		return fmt.Errorf("cert_bundles[%d]: mount_path is required", idx)
	}
	if !path.IsAbs(b.MountPath) {
		return fmt.Errorf("cert_bundles[%d]: mount_path %q must be absolute", idx, b.MountPath)
	}
	cleaned := path.Clean(b.MountPath)
	for _, prefix := range protectedMountPrefixes {
		if cleaned == prefix || strings.HasPrefix(cleaned, prefix+"/") {
			return fmt.Errorf("cert_bundles[%d]: mount_path %q collides with protected prefix %q", idx, b.MountPath, prefix)
		}
	}
	if b.SubPath != "" {
		if path.IsAbs(b.SubPath) || strings.Contains(b.SubPath, "..") {
			return fmt.Errorf("cert_bundles[%d]: sub_path %q must be a relative path without '..'", idx, b.SubPath)
		}
	}
	return nil
}

// applyCertBundles merges rendered cert-bundle trust env into create-time
// env and returns the driver_config envelope (nil when no bundles). Landlock
// ExtraReadOnly for mount paths is handled by defaultSandboxPolicy.
func applyCertBundles(cfg config.SandboxOpenShellConfig, env map[string]string) (map[string]any, error) {
	result, err := renderDriverConfig(cfg.CertBundles)
	if err != nil {
		return nil, err
	}
	if result.DriverConfig == nil {
		return nil, nil
	}
	for k, v := range result.TrustEnv {
		env[k] = v
	}
	return result.DriverConfig, nil
}
