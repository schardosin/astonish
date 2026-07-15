package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	// Default container-side directory for session recordings (writable in
	// Incus and OpenShell sandboxes; downloadable via PullFile).
	containerRecordingDir = "/tmp/astonish-recordings"
	// Local (host) directory for session recordings, relative to cwd.
	localRecordingDir = "recordings"
	// Fixed X display used by KasmVNC inside session containers.
	containerDisplay    = ":0"
	defaultRecordingFPS = 30
)

// safeRecordingName matches basename filenames we allow the agent to supply.
var safeRecordingName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// RecordingOptions configures StartRecording.
type RecordingOptions struct {
	// Filename is an optional basename (e.g. "demo.mp4"). Path separators and
	// unsafe characters are rejected. Empty uses a timestamped default.
	Filename string
}

// RecordingResult is returned by StopRecording.
type RecordingResult struct {
	Path            string  `json:"path"`
	DurationSeconds float64 `json:"duration_seconds"`
	SizeBytes       int64   `json:"size_bytes"`
}

// RecordingStatus is returned by RecordingStatus.
type RecordingStatus struct {
	Recording      bool    `json:"recording"`
	Path           string  `json:"path,omitempty"`
	ElapsedSeconds float64 `json:"elapsed_seconds,omitempty"`
}

// activeRecording tracks an in-progress capture.
type activeRecording struct {
	path      string
	startedAt time.Time
	stopFn    func() error
}

// ContainerStartRecordingFunc starts ffmpeg x11grab inside the session container.
// The returned stop function must gracefully end the capture (SIGINT/'q') and
// wait for the muxer to finalize the MP4.
type ContainerStartRecordingFunc func(containerName, display string, width, height int, outPath string) (stop func() error, err error)

// startLocalRecordingFunc is the local (host) ffmpeg starter. Overridable in tests.
var startLocalRecordingFunc = startLocalRecording

// StartRecording begins capturing the browser display to an MP4 file.
// The browser must already be launched (headed with a display). Only one
// recording may be active at a time.
func (m *Manager) StartRecording(opts RecordingOptions) (path string, err error) {
	if m == nil {
		return "", fmt.Errorf("browser Manager is nil")
	}

	// Ensure browser is up before taking the lock for recording state.
	if _, err := m.GetOrLaunch(); err != nil {
		return "", fmt.Errorf("browser must be launched before recording: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.recording != nil {
		return "", fmt.Errorf("recording already in progress at %s", m.recording.path)
	}

	width := m.config.ViewportWidth
	height := m.config.ViewportHeight
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}

	outPath, err := m.recordingOutputPathLocked(opts.Filename)
	if err != nil {
		return "", err
	}

	var stopFn func() error
	if m.SandboxEnabled {
		if m.ContainerStartRecordingFunc == nil {
			return "", fmt.Errorf("sandbox recording is not wired (ContainerStartRecordingFunc is nil)")
		}
		if m.containerName == "" {
			return "", fmt.Errorf("sandbox browser is not running in a container")
		}
		stopFn, err = m.ContainerStartRecordingFunc(m.containerName, containerDisplay, width, height, outPath)
		if err != nil {
			return "", fmt.Errorf("failed to start container recording: %w", err)
		}
	} else {
		display := m.displayForRecordingLocked()
		if display == "" {
			return "", fmt.Errorf("recording requires a headed browser with a display (set DISPLAY or run with Xvfb); headless capture is not supported")
		}
		stopFn, err = startLocalRecordingFunc(display, width, height, outPath)
		if err != nil {
			return "", fmt.Errorf("failed to start local recording: %w", err)
		}
	}

	m.recording = &activeRecording{
		path:      outPath,
		startedAt: time.Now(),
		stopFn:    stopFn,
	}
	m.logger.Printf("recording started: %s (%dx%d)", outPath, width, height)
	return outPath, nil
}

// StopRecording ends the active capture and returns the finalized file metadata.
func (m *Manager) StopRecording() (RecordingResult, error) {
	if m == nil {
		return RecordingResult{}, fmt.Errorf("browser Manager is nil")
	}

	m.mu.Lock()
	rec := m.recording
	m.recording = nil
	m.mu.Unlock()

	if rec == nil {
		return RecordingResult{}, fmt.Errorf("no recording in progress")
	}

	if rec.stopFn != nil {
		if err := rec.stopFn(); err != nil {
			return RecordingResult{}, fmt.Errorf("failed to stop recording: %w", err)
		}
	}

	duration := time.Since(rec.startedAt).Seconds()
	var size int64
	if fi, err := os.Stat(rec.path); err == nil {
		size = fi.Size()
	}
	// Container paths are not visible on the host filesystem; size stays 0
	// unless a later pull/stat fills it in. Tools still get the path for
	// sandbox artifact download.

	m.logger.Printf("recording stopped: %s (%.1fs, %d bytes)", rec.path, duration, size)
	return RecordingResult{
		Path:            rec.path,
		DurationSeconds: duration,
		SizeBytes:       size,
	}, nil
}

// RecordingStatusLocked returns whether a recording is active. Does not lock;
// callers that already hold m.mu should use this. Prefer RecordingStatus().
func (m *Manager) recordingStatusLocked() RecordingStatus {
	if m.recording == nil {
		return RecordingStatus{Recording: false}
	}
	return RecordingStatus{
		Recording:      true,
		Path:           m.recording.path,
		ElapsedSeconds: time.Since(m.recording.startedAt).Seconds(),
	}
}

// RecordingStatus reports whether a capture is in progress.
func (m *Manager) RecordingStatus() RecordingStatus {
	if m == nil {
		return RecordingStatus{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recordingStatusLocked()
}

// stopRecordingLocked stops an active recording without returning metadata.
// Must be called with m.mu held. Errors are logged and discarded.
func (m *Manager) stopRecordingLocked() {
	if m.recording == nil {
		return
	}
	rec := m.recording
	m.recording = nil
	if rec.stopFn != nil {
		if err := rec.stopFn(); err != nil {
			m.logger.Printf("recording stop during cleanup: %v", err)
		}
	}
}

func (m *Manager) recordingOutputPathLocked(filename string) (string, error) {
	name, err := sanitizeRecordingFilename(filename)
	if err != nil {
		return "", err
	}
	if m.SandboxEnabled {
		return filepath.Join(containerRecordingDir, name), nil
	}
	if err := os.MkdirAll(localRecordingDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create recordings directory: %w", err)
	}
	abs, err := filepath.Abs(filepath.Join(localRecordingDir, name))
	if err != nil {
		return "", err
	}
	return abs, nil
}

func (m *Manager) displayForRecordingLocked() string {
	if m.xvfb != nil && m.xvfb.display != "" {
		return m.xvfb.display
	}
	return strings.TrimSpace(os.Getenv("DISPLAY"))
}

func sanitizeRecordingFilename(filename string) (string, error) {
	name := strings.TrimSpace(filename)
	if name == "" {
		name = fmt.Sprintf("browser-%s.mp4", time.Now().Format("20060102-150405"))
	}
	name = filepath.Base(name)
	if name == "." || name == ".." || name == "" {
		return "", fmt.Errorf("invalid recording filename")
	}
	if !safeRecordingName.MatchString(name) {
		return "", fmt.Errorf("recording filename may only contain letters, digits, '.', '_', and '-'")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".mp4") {
		name += ".mp4"
	}
	return name, nil
}

// startLocalRecording launches ffmpeg x11grab against the given DISPLAY.
func startLocalRecording(display string, width, height int, outPath string) (func() error, error) {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH (install ffmpeg to enable browser recording): %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, err
	}

	input := display
	if !strings.Contains(input, ".") {
		input = input + ".0"
	}

	cmd := exec.Command(bin,
		"-y",
		"-f", "x11grab",
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-framerate", fmt.Sprintf("%d", defaultRecordingFPS),
		"-i", input,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "23",
		"-pix_fmt", "yuv420p",
		outPath,
	)
	cmd.Env = append(os.Environ(), "DISPLAY="+display)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}

	// Brief settle: catch immediate start failures (missing display, etc.).
	time.Sleep(300 * time.Millisecond)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		_ = stdin.Close()
		return nil, fmt.Errorf("ffmpeg exited immediately after start")
	}

	return func() error {
		// Prefer graceful quit so the MP4 moov atom is written.
		_, _ = stdin.Write([]byte("q"))
		_ = stdin.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				// Non-zero exit is common after 'q'; only fail if file missing.
				if _, statErr := os.Stat(outPath); statErr != nil {
					return fmt.Errorf("ffmpeg exited without producing %s: %w", outPath, err)
				}
			}
			return nil
		case <-time.After(30 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			return fmt.Errorf("timed out waiting for ffmpeg to finalize recording")
		}
	}, nil
}

// BuildFFmpegX11GrabArgs returns the ffmpeg argv used for display capture.
// Exported for sandbox wiring and tests so the flags stay in one place.
func BuildFFmpegX11GrabArgs(display string, width, height int, outPath string) []string {
	input := display
	if !strings.Contains(input, ".") {
		input = input + ".0"
	}
	return []string{
		"ffmpeg", "-y",
		"-f", "x11grab",
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-framerate", fmt.Sprintf("%d", defaultRecordingFPS),
		"-i", input,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "23",
		"-pix_fmt", "yuv420p",
		outPath,
	}
}
