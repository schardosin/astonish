// Command astonish-sandbox-webhook is a MutatingAdmissionWebhook server
// that injects Astonish-required volumes, mounts, FUSE device resources,
// and CAP_SYS_ADMIN into sandbox pods created by OpenShell's K8s driver.
//
// OpenShell's K8s driver creates pods from the Sandbox CRD but does not
// support custom volume mounts in its driver-config-json. This webhook
// bridges that gap by mutating pods in the sandbox namespace that carry
// the astonish.io/type label.
//
// Deployment:
//   Namespace: astonish-system
//   Deployment: astonish-sandbox-webhook (2 replicas)
//   TLS: cert-manager Certificate or manually mounted secret
//
// Reference: docs/architecture/openshell-sandbox-backend.md §10.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "astonish-sandbox-webhook: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := loadConfig()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handleMutate(cfg))
	mux.HandleFunc("/healthz", handleHealthz)

	tlsCert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return fmt.Errorf("load TLS cert: %w", err)
	}

	server := &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			MinVersion:   tls.VersionTLS12,
		},
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("webhook server starting", "addr", cfg.Addr)
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down webhook server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
