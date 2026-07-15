package incus

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/browser"
)

const recordingPIDFile = "/tmp/astonish-recording.pid"
const recordingLogFile = "/tmp/astonish-recording.log"

// StartRecordingInContainer launches ffmpeg x11grab against the KasmVNC
// display inside the session container. The returned stop function sends
// SIGINT and waits for the MP4 to finalize.
func StartRecordingInContainer(client *IncusClient, containerName, display string, width, height int, outPath string) (func() error, error) {
	if client == nil {
		return nil, fmt.Errorf("incus client is nil")
	}
	if containerName == "" {
		return nil, fmt.Errorf("container name is required")
	}
	if outPath == "" || !strings.HasPrefix(outPath, "/") {
		return nil, fmt.Errorf("recording outPath must be absolute, got %q", outPath)
	}

	dir := filepath.Dir(outPath)
	mkdirCmd := []string{"mkdir", "-p", dir}
	if code, err := client.ExecSimple(containerName, mkdirCmd); err != nil {
		return nil, fmt.Errorf("mkdir recordings dir: %w", err)
	} else if code != 0 {
		return nil, fmt.Errorf("mkdir recordings dir exited %d", code)
	}

	args := browser.BuildFFmpegX11GrabArgs(display, width, height, outPath)
	// Quote for shell: paths may contain spaces (unlikely for our defaults).
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

	if code, out, err := ExecWithOutput(client, containerName, []string{"sh", "-c", startScript}); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w (output: %s)", err, out)
	} else if code != 0 {
		return nil, fmt.Errorf("start ffmpeg exited %d: %s", code, strings.TrimSpace(out))
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

		code, out, err := ExecWithOutput(client, containerName, []string{"sh", "-c", stopScript})
		if err != nil {
			return fmt.Errorf("stop ffmpeg: %w (output: %s)", err, out)
		}
		if code != 0 {
			return fmt.Errorf("stop ffmpeg exited %d: %s", code, strings.TrimSpace(out))
		}
		// Brief settle for filesystem flush.
		time.Sleep(100 * time.Millisecond)
		return nil
	}, nil
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
