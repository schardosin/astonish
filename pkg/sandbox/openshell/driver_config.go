package openshell

import (
	"fmt"
	"path"
	"strings"

	"github.com/SAP/astonish/pkg/config"
)

// systemCABundlePath is where OpenShell's supervisor loads system CA roots
// for upstream TLS after MITM CONNECT. Corporate CAs must appear here
// (volume mount before PID 1); agent SSL_CERT_FILE alone is not enough.
const systemCABundlePath = "/etc/ssl/certs/ca-certificates.crt"

// defaultTrustEnvVars are set to each cert bundle MountPath when TrustEnv
// is empty. OpenShell typically overwrites these to /etc/openshell-tls/...;
// InstallSystemTrust is the contract that makes corporate upstream TLS work.
// Operators must place a *combined* PEM (system CAs + corporate roots) in
// the bundle so replacing the system store does not break public HTTPS.
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
	// DriverConfig is nil when there are no PVC-backed cert bundles
	// (configMap sources omit volumes — Kyverno injects mounts).
	DriverConfig map[string]any
	// TrustEnv maps env var name → MountPath.
	TrustEnv map[string]string
	// ExtraReadOnly are mount paths to append to the Landlock RO set.
	ExtraReadOnly []string
}

// renderDriverConfig builds the Kubernetes driver_config envelope and trust
// env vars from CertBundles. Returns a zero result when bundles is empty.
// ConfigMap-sourced bundles contribute trust env + ExtraReadOnly only;
// OpenShell's driver_config schema is PVC-only.
func renderDriverConfig(bundles []config.CertBundleConfig) (driverConfigResult, error) {
	if len(bundles) == 0 {
		return driverConfigResult{}, nil
	}

	volumes := make([]any, 0, len(bundles))
	mounts := make([]any, 0, len(bundles)*2)
	trustEnv := make(map[string]string)
	extraRO := make([]string, 0, len(bundles)*2)
	seenNames := make(map[string]struct{}, len(bundles))
	systemTrustCount := 0

	for i, b := range bundles {
		if err := validateCertBundle(b, i); err != nil {
			return driverConfigResult{}, err
		}
		if _, dup := seenNames[b.Name]; dup {
			return driverConfigResult{}, fmt.Errorf("cert_bundles[%d]: duplicate name %q", i, b.Name)
		}
		seenNames[b.Name] = struct{}{}

		source := certBundleSource(b)
		cleanedMount := path.Clean(b.MountPath)
		mountsAtSystemPath := cleanedMount == systemCABundlePath
		installSystem := certBundleInstallSystemTrust(b)
		// Mounting at the system path counts as a system-trust install even
		// when install_system_trust is false — only one such mount is allowed.
		usesSystemPath := installSystem || mountsAtSystemPath

		if usesSystemPath {
			systemTrustCount++
			if systemTrustCount > 1 {
				return driverConfigResult{}, fmt.Errorf(
					"cert_bundles[%d]: at most one cert_bundle may install into %s",
					i, systemCABundlePath)
			}
		}

		if source == config.CertBundleSourcePVC {
			volumes = append(volumes, map[string]any{
				"name": b.Name,
				"persistent_volume_claim": map[string]any{
					"claim_name": b.ClaimName,
					"read_only":  true,
				},
			})

			// Primary operator mount (skipped when it would duplicate the system mount).
			if !mountsAtSystemPath {
				mounts = append(mounts, certBundleVolumeMount(b.Name, b.MountPath, b.SubPath))
			}
			if usesSystemPath {
				mounts = append(mounts, certBundleVolumeMount(b.Name, systemCABundlePath, b.SubPath))
			}
		}

		// Landlock + trust env apply for both PVC and ConfigMap sources.
		if !mountsAtSystemPath {
			extraRO = append(extraRO, b.MountPath)
		}
		if usesSystemPath {
			extraRO = append(extraRO, systemCABundlePath)
		}

		envKeys := b.TrustEnv
		if len(envKeys) == 0 {
			envKeys = defaultTrustEnvVars
		}
		trustPath := b.MountPath
		if mountsAtSystemPath {
			trustPath = systemCABundlePath
		}
		for _, k := range envKeys {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			trustEnv[k] = trustPath
		}
	}

	result := driverConfigResult{
		TrustEnv:      trustEnv,
		ExtraReadOnly: extraRO,
	}
	if len(volumes) == 0 {
		return result, nil
	}
	result.DriverConfig = map[string]any{
		"kubernetes": map[string]any{
			"volumes": volumes,
			"containers": map[string]any{
				"agent": map[string]any{
					"volume_mounts": mounts,
				},
			},
		},
	}
	return result, nil
}

func certBundleVolumeMount(name, mountPath, subPath string) map[string]any {
	mount := map[string]any{
		"name":       name,
		"mount_path": mountPath,
		"read_only":  true,
	}
	if subPath != "" {
		mount["sub_path"] = subPath
	}
	return mount
}

// certBundleSource resolves Source with legacy defaults: claim_name without
// config_map_name → pvc; otherwise configMap when unset.
func certBundleSource(b config.CertBundleConfig) string {
	s := strings.TrimSpace(b.Source)
	if s != "" {
		return s
	}
	if strings.TrimSpace(b.ClaimName) != "" && strings.TrimSpace(b.ConfigMapName) == "" {
		return config.CertBundleSourcePVC
	}
	return config.CertBundleSourceConfigMap
}

// certBundleInstallSystemTrust returns whether the bundle should be installed
// over the system CA path. Nil means default true.
func certBundleInstallSystemTrust(b config.CertBundleConfig) bool {
	if b.InstallSystemTrust == nil {
		return true
	}
	return *b.InstallSystemTrust
}

func validateCertBundle(b config.CertBundleConfig, idx int) error {
	if strings.TrimSpace(b.Name) == "" {
		return fmt.Errorf("cert_bundles[%d]: name is required", idx)
	}
	source := certBundleSource(b)
	switch source {
	case config.CertBundleSourcePVC:
		if strings.TrimSpace(b.ClaimName) == "" {
			return fmt.Errorf("cert_bundles[%d]: claim_name is required for source pvc", idx)
		}
	case config.CertBundleSourceConfigMap:
		if strings.TrimSpace(b.ConfigMapName) == "" {
			return fmt.Errorf("cert_bundles[%d]: config_map_name is required for source configMap", idx)
		}
	default:
		return fmt.Errorf("cert_bundles[%d]: source %q must be %q or %q",
			idx, source, config.CertBundleSourcePVC, config.CertBundleSourceConfigMap)
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
// env and returns the driver_config envelope (nil when no PVC volumes).
// ConfigMap sources still set trust env; Landlock ExtraReadOnly for mount
// paths is handled by defaultSandboxPolicy.
func applyCertBundles(cfg config.SandboxOpenShellConfig, env map[string]string) (map[string]any, error) {
	result, err := renderDriverConfig(cfg.CertBundles)
	if err != nil {
		return nil, err
	}
	for k, v := range result.TrustEnv {
		env[k] = v
	}
	return result.DriverConfig, nil
}
