// Kubernetes client configuration loaders.
//
// Phase D wiring: astonish needs to connect to a real API server when the
// operator selects backend=k8s in the app config. This file provides the
// minimal, well-tested ladder that production code uses:
//
//  1. In-cluster config (serviceaccount token mounted into the pod), when
//     Astonish itself is running inside the cluster.
//  2. Explicit kubeconfig path (opts.KubeconfigPath).
//  3. $KUBECONFIG env var.
//  4. Default ~/.kube/config.
//
// The loader is intentionally thin: we don't wrap client-go's types or
// invent new abstractions. The return value is a rest.Config ready to
// pass into kubernetes.NewForConfig.
//
// Rationale:
//
//   - We reuse client-go's clientcmd precedence rules so that kubectl and
//     astonish see the same cluster without surprises.
//   - The in-cluster branch is tried first because deployments run
//     astonish as a Pod far more often than as a laptop CLI; a stray
//     ~/.kube/config on a node would otherwise silently win.
//   - The Context field lets operators pin a specific context in a
//     multi-cluster kubeconfig without editing files.
//   - QPS/Burst are tunable: our workload (pod create/delete, exec) bursts
//     hard during fleet scale-out.

package k8s

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// LoadConfigOptions controls how the REST config is discovered. Zero value
// is valid and applies the default precedence (in-cluster → $KUBECONFIG →
// ~/.kube/config).
type LoadConfigOptions struct {
	// KubeconfigPath, if non-empty, forces the loader to use this file
	// and bypass both in-cluster detection and $KUBECONFIG. Useful for
	// CLI flags like --kubeconfig.
	KubeconfigPath string

	// Context, if non-empty, selects the named context from the loaded
	// kubeconfig. Ignored for in-cluster configs.
	Context string

	// InCluster forces the in-cluster path and returns an error if the
	// service account files are absent. Defaults to auto-detect.
	InCluster bool

	// QPS sets the client-go QPS rate. Default: 50 (client-go's default
	// of 5 is too low for bursty fleet operations).
	QPS float32

	// Burst sets the client-go burst. Default: 100.
	Burst int

	// UserAgent sets the User-Agent header on every request. Default:
	// "astonish/<version>".
	UserAgent string
}

// LoadRESTConfig discovers and returns a *rest.Config using the precedence
// documented on the package. Errors from the underlying loaders are
// wrapped with actionable context.
func LoadRESTConfig(opts LoadConfigOptions) (*rest.Config, error) {
	var (
		cfg *rest.Config
		err error
	)

	switch {
	case opts.InCluster:
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("sandbox/k8s: in-cluster config requested but unavailable: %w", err)
		}

	case opts.KubeconfigPath != "":
		cfg, err = loadKubeconfig(opts.KubeconfigPath, opts.Context)
		if err != nil {
			return nil, fmt.Errorf("sandbox/k8s: load kubeconfig %q: %w", opts.KubeconfigPath, err)
		}

	default:
		// Auto-detect: prefer in-cluster when the SA token is present,
		// fall back to kubeconfig otherwise.
		if _, statErr := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); statErr == nil {
			cfg, err = rest.InClusterConfig()
			if err != nil {
				// Token file was present but loading still failed; surface
				// the error rather than silently falling back.
				return nil, fmt.Errorf("sandbox/k8s: in-cluster token found but config load failed: %w", err)
			}
		} else {
			path := os.Getenv("KUBECONFIG")
			if path == "" {
				home, homeErr := os.UserHomeDir()
				if homeErr != nil {
					return nil, fmt.Errorf("sandbox/k8s: no kubeconfig: %w", homeErr)
				}
				path = filepath.Join(home, ".kube", "config")
			}
			cfg, err = loadKubeconfig(path, opts.Context)
			if err != nil {
				return nil, fmt.Errorf("sandbox/k8s: load kubeconfig %q: %w", path, err)
			}
		}
	}

	applyDefaultsToRESTConfig(cfg, opts)
	return cfg, nil
}

func loadKubeconfig(path, contextName string) (*rest.Config, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: path}
	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
}

func applyDefaultsToRESTConfig(cfg *rest.Config, opts LoadConfigOptions) {
	if cfg == nil {
		return
	}
	if opts.QPS > 0 {
		cfg.QPS = opts.QPS
	} else if cfg.QPS == 0 {
		cfg.QPS = 50
	}
	if opts.Burst > 0 {
		cfg.Burst = opts.Burst
	} else if cfg.Burst == 0 {
		cfg.Burst = 100
	}
	if opts.UserAgent != "" {
		cfg.UserAgent = opts.UserAgent
	} else if cfg.UserAgent == "" {
		cfg.UserAgent = rest.DefaultKubernetesUserAgent() + " astonish"
	}
}

// NewClientFromOptions is the one-stop helper used by production callers:
// discovery + clientset construction in a single call. Returns the
// underlying rest.Config too so callers that need subresource transports
// (exec, portforward) have what they need.
func NewClientFromOptions(opts LoadConfigOptions) (kubernetes.Interface, *rest.Config, error) {
	restCfg, err := LoadRESTConfig(opts)
	if err != nil {
		return nil, nil, err
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox/k8s: build clientset: %w", err)
	}
	return cs, restCfg, nil
}
