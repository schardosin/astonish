package drill

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/config"
)

// DefaultReadyCheckTimeout is the default max wait time in seconds.
const DefaultReadyCheckTimeout = 30

// DefaultReadyCheckInterval is the default poll interval in seconds.
const DefaultReadyCheckInterval = 2

// RunReadyCheck polls until the application under test is ready or timeout expires.
func RunReadyCheck(ctx context.Context, rc *config.ReadyCheck) error {
	if rc == nil {
		return nil
	}

	timeout := rc.Timeout
	if timeout <= 0 {
		timeout = DefaultReadyCheckTimeout
	}
	interval := rc.Interval
	if interval <= 0 {
		interval = DefaultReadyCheckInterval
	}

	deadline := time.After(time.Duration(timeout) * time.Second)
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	// Try immediately before first tick
	if err := checkOnce(ctx, rc); err == nil {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("ready check cancelled: %w", ctx.Err())
		case <-deadline:
			return fmt.Errorf("ready check timed out after %ds (type: %s)", timeout, rc.Type)
		case <-ticker.C:
			if err := checkOnce(ctx, rc); err == nil {
				return nil
			}
		}
	}
}

func checkOnce(ctx context.Context, rc *config.ReadyCheck) error {
	switch rc.Type {
	case "http":
		return checkHTTP(ctx, rc.URL)
	case "port":
		return checkPort(rc.Host, rc.Port)
	case "output_contains":
		// output_contains is handled by the runner, not here.
		// It checks the last setup command's output for the pattern.
		// This function cannot evaluate it since it doesn't have access to setup output.
		return fmt.Errorf("output_contains ready check must be handled by the runner")
	default:
		return fmt.Errorf("unknown ready check type: %q", rc.Type)
	}
}

func checkHTTP(ctx context.Context, url string) error {
	if url == "" {
		return fmt.Errorf("ready check http: url is required")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("http ready check: status %d", resp.StatusCode)
}

func checkPort(host string, port int) error {
	if host == "" {
		host = "localhost"
	}
	if port <= 0 {
		return fmt.Errorf("ready check port: port is required")
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// CheckOutputContains checks if the provided output contains the expected pattern.
// This is used by the runner for the "output_contains" ready check type.
func CheckOutputContains(output string, pattern string) bool {
	return strings.Contains(output, pattern)
}
