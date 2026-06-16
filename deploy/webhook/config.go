package main

import (
	"log/slog"
	"os"
)

// webhookConfig holds the server's runtime configuration.
type webhookConfig struct {
	// Addr is the listen address. Default: ":8443".
	Addr string

	// CertFile and KeyFile are paths to the TLS certificate and key.
	CertFile string
	KeyFile  string

	// LayersPVCName is the name of the PVC for the content-addressed layer store.
	LayersPVCName string

	// UppersPVCName is the name of the PVC for per-session upper layers.
	UppersPVCName string

	// FuseDeviceResource is the extended resource name for FUSE device
	// requests (e.g., "smarter-devices/fuse"). Empty means no device
	// resource is requested (privileged + mknod path).
	FuseDeviceResource string

	// InjectCAP_SYS_ADMIN controls whether CAP_SYS_ADMIN is added to
	// the container's securityContext. Required for pivot_root in
	// astonish-boot. Default: true.
	InjectSysAdmin bool

	// LogLevel sets the minimum log level.
	LogLevel slog.Level
}

func loadConfig() webhookConfig {
	cfg := webhookConfig{
		Addr:           envOr("WEBHOOK_ADDR", ":8443"),
		CertFile:       envOr("WEBHOOK_CERT_FILE", "/etc/webhook/certs/tls.crt"),
		KeyFile:        envOr("WEBHOOK_KEY_FILE", "/etc/webhook/certs/tls.key"),
		LayersPVCName:  envOr("WEBHOOK_LAYERS_PVC", "astonish-layers"),
		UppersPVCName:  envOr("WEBHOOK_UPPERS_PVC", "astonish-uppers"),
		FuseDeviceResource: os.Getenv("WEBHOOK_FUSE_DEVICE_RESOURCE"),
		InjectSysAdmin: envOr("WEBHOOK_INJECT_SYS_ADMIN", "true") == "true",
		LogLevel:       slog.LevelInfo,
	}
	if os.Getenv("WEBHOOK_LOG_LEVEL") == "debug" {
		cfg.LogLevel = slog.LevelDebug
	}
	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
