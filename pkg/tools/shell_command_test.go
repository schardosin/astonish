package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellCommand_Echo(t *testing.T) {
	result, err := ShellCommand(nil, ShellCommandArgs{
		Command: "echo hello world",
	})
	if err != nil {
		t.Fatalf("ShellCommand() error = %v", err)
	}
	if got := strings.TrimSpace(result.Stdout); got != "hello world" {
		t.Errorf("Stdout = %q, want %q", got, "hello world")
	}
	if result.TimedOut {
		t.Error("TimedOut = true, want false")
	}
}

func TestShellCommand_WorkingDir(t *testing.T) {
	dir := t.TempDir()

	// Create a file in the temp dir
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("found"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := ShellCommand(nil, ShellCommandArgs{
		Command:    "cat marker.txt",
		WorkingDir: dir,
	})
	if err != nil {
		t.Fatalf("ShellCommand() error = %v", err)
	}
	if got := strings.TrimSpace(result.Stdout); got != "found" {
		t.Errorf("Stdout = %q, want %q", got, "found")
	}
}

func TestShellCommand_ExitNonZero(t *testing.T) {
	_, err := ShellCommand(nil, ShellCommandArgs{
		Command: "exit 1",
	})
	if err == nil {
		t.Fatal("expected error for non-zero exit, got nil")
	}
}

func TestShellCommand_StderrInOutput(t *testing.T) {
	// CombinedOutput captures both stdout and stderr
	_, err := ShellCommand(nil, ShellCommandArgs{
		Command: "echo error_msg >&2 && exit 1",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// The error message should mention command failure
	if !strings.Contains(err.Error(), "failed to execute") {
		t.Errorf("error = %q, want it to contain 'failed to execute'", err.Error())
	}
}

func TestShellCommand_TimeoutDefault(t *testing.T) {
	// Verify that a quick command with default timeout works fine
	result, err := ShellCommand(nil, ShellCommandArgs{
		Command: "echo quick",
		Timeout: 0, // should default to 120
	})
	if err != nil {
		t.Fatalf("ShellCommand() error = %v", err)
	}
	if got := strings.TrimSpace(result.Stdout); got != "quick" {
		t.Errorf("Stdout = %q, want %q", got, "quick")
	}
}

func TestShellCommand_TimeoutExceeded(t *testing.T) {
	result, err := ShellCommand(nil, ShellCommandArgs{
		Command: "sleep 10",
		Timeout: 1,
	})
	if err != nil {
		t.Fatalf("ShellCommand() error = %v (expected nil with TimedOut=true)", err)
	}
	if !result.TimedOut {
		t.Error("TimedOut = false, want true")
	}
}

func TestShellCommand_TimeoutClamped(t *testing.T) {
	// Timeout > 3600 should be clamped to 3600, but command should still run
	result, err := ShellCommand(nil, ShellCommandArgs{
		Command: "echo clamped",
		Timeout: 9999,
	})
	if err != nil {
		t.Fatalf("ShellCommand() error = %v", err)
	}
	if got := strings.TrimSpace(result.Stdout); got != "clamped" {
		t.Errorf("Stdout = %q, want %q", got, "clamped")
	}
}

func TestShellCommand_MultilineOutput(t *testing.T) {
	result, err := ShellCommand(nil, ShellCommandArgs{
		Command: "printf 'line1\nline2\nline3\n'",
	})
	if err != nil {
		t.Fatalf("ShellCommand() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(lines) != 3 {
		t.Errorf("output lines = %d, want 3", len(lines))
	}
}
