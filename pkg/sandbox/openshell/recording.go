package openshell

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/browser"
)

const recordingPIDFile = "/tmp/astonish-recording.pid"
const recordingLogFile = "/tmp/astonish-recording.log"

// StartRecordingInSandbox launches ffmpeg x11grab against the KasmVNC display
// inside the OpenShell pod. The returned stop function sends SIGINT and waits
// for the MP4 to finalize.
func StartRecordingInSandbox(ctx context.Context, gw GatewayClient, podName, display string, width, height int, outPath string) (func() error, error) {
	if gw == nil {
		return nil, fmt.Errorf("gateway client is nil")
	}
	if podName == "" {
		return nil, fmt.Errorf("pod name is required")
	}
	if outPath == "" || !strings.HasPrefix(outPath, "/") {
		return nil, fmt.Errorf("recording outPath must be absolute, got %q", outPath)
	}

	dir := filepath.Dir(outPath)
	if _, err := execShell(ctx, gw, podName, fmt.Sprintf("mkdir -p %s", shellQuote(dir))); err != nil {
		return nil, fmt.Errorf("mkdir recordings dir: %w", err)
	}

	args := browser.BuildFFmpegX11GrabArgs(display, width, height, outPath)
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}

	startScript := fmt.Sprintf(`set -e
rm -f %s
DISPLAY=%s %s >%s 2>&1 &
echo $! > %s
sleep 0.4
if ! kill -0 "$(cat %s)" 2>/dev/null; then
  echo "ffmpeg failed to start:" >&2
  cat %s >&2 || true
  exit 1
fi
`, shellQuote(recordingPIDFile), shellQuote(display), strings.Join(quoted, " "),
		shellQuote(recordingLogFile), shellQuote(recordingPIDFile),
		shellQuote(recordingPIDFile), shellQuote(recordingLogFile))

	if out, err := execShell(ctx, gw, podName, startScript); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w (output: %s)", err, out)
	}

	return func() error {
		stopScript := fmt.Sprintf(`set -e
PIDFILE=%s
OUT=%s
if [ ! -f "$PIDFILE" ]; then
  echo "recording pid file missing" >&2
  exit 1
fi
PID=$(cat "$PIDFILE")
kill -INT "$PID" 2>/dev/null || true
for i in $(seq 1 60); do
  if ! kill -0 "$PID" 2>/dev/null; then
    break
  fi
  sleep 0.5
done
kill -9 "$PID" 2>/dev/null || true
rm -f "$PIDFILE"
if [ ! -s "$OUT" ]; then
  echo "recording file missing or empty: $OUT" >&2
  cat %s >&2 || true
  exit 1
fi
`, shellQuote(recordingPIDFile), shellQuote(outPath), shellQuote(recordingLogFile))

		if out, err := execShell(context.Background(), gw, podName, stopScript); err != nil {
			return fmt.Errorf("stop ffmpeg: %w (output: %s)", err, out)
		}
		time.Sleep(100 * time.Millisecond)
		return nil
	}, nil
}

func execShell(ctx context.Context, gw GatewayClient, podName, script string) (string, error) {
	resp, err := gw.ExecCommand(ctx, podName, ExecRequest{
		Command: []string{"sh", "-c", script},
	})
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(string(resp.Stdout) + string(resp.Stderr))
	if resp.ExitCode != 0 {
		return out, fmt.Errorf("exit %d: %s", resp.ExitCode, out)
	}
	return out, nil
}
