package browser

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
)

// Xvfb manages a virtual X11 framebuffer (Xvfb) process for running
// headed Chrome on Linux servers that lack a physical display.
// This gives Chrome a real X11 display, producing a more realistic
// browser fingerprint than headless mode (different WebGL, audio, plugins).
type Xvfb struct {
	cmd     *exec.Cmd
	display string
	logger  *log.Logger
}

// NewXvfb creates a new Xvfb manager. The logger is used for warnings
// and status messages. Xvfb is not started until Start() is called.
func NewXvfb(logger *log.Logger) *Xvfb {
	return &Xvfb{logger: logger}
}

// Start launches Xvfb with the given screen resolution.
// It tries displays :99 through :109, picking the first one that is not
// already in use (checked via X lock files in /tmp). Sets the DISPLAY
// environment variable so child processes (Chrome) use it.
// Returns an error if the xvfb binary is not found or fails to start.
func (x *Xvfb) Start(width, height int) error {
	bin, err := exec.LookPath("Xvfb")
	if err != nil {
		// Also try lowercase (some distros install as "xvfb")
		bin, err = exec.LookPath("xvfb")
		if err != nil {
			return fmt.Errorf("Xvfb binary not found: %w", err)
		}
	}

	// Find an available display number by checking X lock files.
	display := ""
	for n := 99; n <= 109; n++ {
		lockFile := fmt.Sprintf("/tmp/.X%d-lock", n)
		if _, err := os.Stat(lockFile); os.IsNotExist(err) {
			display = fmt.Sprintf(":%d", n)
			break
		}
	}
	if display == "" {
		return fmt.Errorf("no available display found (all :99 through :109 are in use)")
	}

	screen := fmt.Sprintf("%dx%dx24", width, height)

	x.cmd = exec.Command(bin, display, "-screen", "0", screen, "-nolisten", "tcp", "-ac")
	x.cmd.Stdout = nil
	x.cmd.Stderr = nil

	if err := x.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Xvfb: %w", err)
	}

	x.display = display

	// Set DISPLAY so Chrome (and any other child process) uses our virtual display.
	os.Setenv("DISPLAY", display)

	// Give Xvfb a moment to initialize the display before Chrome tries to connect.
	time.Sleep(500 * time.Millisecond)

	// Verify the process is still alive (catches immediate startup failures).
	if x.cmd.ProcessState != nil && x.cmd.ProcessState.Exited() {
		return fmt.Errorf("Xvfb exited immediately (exit code %d)", x.cmd.ProcessState.ExitCode())
	}

	x.logger.Printf("Xvfb started on display %s (%dx%d)", display, width, height)
	return nil
}

// Stop terminates the Xvfb process and unsets the DISPLAY variable.
// Safe to call multiple times or on a nil receiver.
func (x *Xvfb) Stop() {
	if x == nil || x.cmd == nil || x.cmd.Process == nil {
		return
	}

	_ = x.cmd.Process.Kill()
	// Wait prevents zombie processes.
	_ = x.cmd.Wait()

	os.Unsetenv("DISPLAY")
	x.logger.Printf("Xvfb stopped (display %s)", x.display)

	x.cmd = nil
}
