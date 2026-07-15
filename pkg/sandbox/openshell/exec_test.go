package openshell

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/SAP/astonish/pkg/store"
)

// seedSession inserts a test session and returns its sandbox ID.
func seedSession(t *testing.T, b *OpenShellBackend, sessionID, sandboxID string) {
	t.Helper()
	rec := &store.SandboxSession{
		SessionID:     sessionID,
		ChatSessionID: sessionID,
		Backend:       "openshell",
		ContainerName: sandboxID,
		PodName:       sandboxID, // gateway UUID used for ExecSandbox
		State:         store.SandboxSessionStateRunning,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("seedSession: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Exec tests
// ---------------------------------------------------------------------------

func TestExec_Success(t *testing.T) {
	gw := &mockGateway{
		execFn: func(ctx context.Context, sandboxID string, req ExecRequest) (*ExecResponse, error) {
			if sandboxID != "sb-exec" {
				t.Errorf("ExecCommand called with sandbox %q, want %q", sandboxID, "sb-exec")
			}
			// Echo the command back as stdout.
			return &ExecResponse{
				ExitCode: 0,
				Stdout:   []byte(strings.Join(req.Command, " ")),
			}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)
	seedSession(t, b, "sess-exec", "sb-exec")

	result, err := b.Exec(context.Background(), "sess-exec", sandbox.ExecSpec{
		Command: []string{"echo", "hello"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !bytes.Contains(result.Stdout, []byte("echo")) {
		t.Errorf("Stdout = %q, expected to contain 'echo'", result.Stdout)
	}
}

func TestExec_NonZeroExit(t *testing.T) {
	gw := &mockGateway{
		execFn: func(ctx context.Context, sandboxID string, req ExecRequest) (*ExecResponse, error) {
			return &ExecResponse{
				ExitCode: 127,
				Stderr:   []byte("command not found"),
			}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)
	seedSession(t, b, "sess-nz", "sb-nz")

	result, err := b.Exec(context.Background(), "sess-nz", sandbox.ExecSpec{
		Command: []string{"nonexist"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.ExitCode != 127 {
		t.Errorf("ExitCode = %d, want 127", result.ExitCode)
	}
}

func TestExec_EmptyCommand(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	_, err := b.Exec(context.Background(), "any", sandbox.ExecSpec{})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestExec_SessionNotFound(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	_, err := b.Exec(context.Background(), "ghost", sandbox.ExecSpec{
		Command: []string{"ls"},
	})
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestExec_WithWorkDirAndEnv(t *testing.T) {
	var capturedCmd []string
	gw := &mockGateway{
		execFn: func(ctx context.Context, sandboxID string, req ExecRequest) (*ExecResponse, error) {
			capturedCmd = req.Command
			return &ExecResponse{ExitCode: 0}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)
	seedSession(t, b, "sess-env", "sb-env")

	_, err := b.Exec(context.Background(), "sess-env", sandbox.ExecSpec{
		Command: []string{"make", "build"},
		WorkDir: "/app",
		Env:     map[string]string{"GOOS": "linux"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	// Should be wrapped in /bin/sh -c "cd ... && export ... && exec ..."
	if len(capturedCmd) != 3 || capturedCmd[0] != "/bin/sh" {
		t.Errorf("expected shell-wrapped command, got %v", capturedCmd)
	}
	shell := capturedCmd[2]
	if !strings.Contains(shell, "cd '/app'") {
		t.Errorf("shell cmd should contain cd '/app', got: %s", shell)
	}
	if !strings.Contains(shell, "GOOS='linux'") {
		t.Errorf("shell cmd should contain GOOS='linux', got: %s", shell)
	}
	if !strings.Contains(shell, "exec 'make' 'build'") {
		t.Errorf("shell cmd should contain exec 'make' 'build', got: %s", shell)
	}
}

func TestExec_GatewayError(t *testing.T) {
	gw := &mockGateway{
		execFn: func(ctx context.Context, sandboxID string, req ExecRequest) (*ExecResponse, error) {
			return nil, errors.New("connection reset")
		},
	}
	b := newTestBackendWithGateway(t, gw)
	seedSession(t, b, "sess-gwerr", "sb-gwerr")

	_, err := b.Exec(context.Background(), "sess-gwerr", sandbox.ExecSpec{
		Command: []string{"ls"},
	})
	if err == nil {
		t.Fatal("expected error from gateway")
	}
}

// ---------------------------------------------------------------------------
// ExecInteractive tests
// ---------------------------------------------------------------------------

func TestExecInteractive_NilGateway(t *testing.T) {
	b := newTestBackend(t)
	_, err := b.ExecInteractive(context.Background(), "x", sandbox.PTYSpec{Command: []string{"bash"}})
	if !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("got %v, want ErrNotImplementedYet", err)
	}
}

func TestExecInteractive_EmptyCommand(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)
	_, err := b.ExecInteractive(context.Background(), "x", sandbox.PTYSpec{})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestExecInteractive_SessionNotFound(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)
	_, err := b.ExecInteractive(context.Background(), "ghost", sandbox.PTYSpec{Command: []string{"bash"}})
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

// ---------------------------------------------------------------------------
// ExecStreaming tests
// ---------------------------------------------------------------------------

func TestExecStreaming_NilGateway(t *testing.T) {
	b := newTestBackend(t)
	_, err := b.ExecStreaming(context.Background(), "x", sandbox.ExecStreamSpec{Command: []string{"cat"}})
	if !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("got %v, want ErrNotImplementedYet", err)
	}
}

func TestExecStreaming_EmptyCommand(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)
	_, err := b.ExecStreaming(context.Background(), "x", sandbox.ExecStreamSpec{})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

// ---------------------------------------------------------------------------
// PushFile tests
// ---------------------------------------------------------------------------

func TestPushFile_Success(t *testing.T) {
	var capturedStdin []byte
	gw := &mockGateway{
		execFn: func(ctx context.Context, sandboxID string, req ExecRequest) (*ExecResponse, error) {
			if req.Stdin != nil {
				data, _ := io.ReadAll(req.Stdin)
				capturedStdin = data
			}
			return &ExecResponse{ExitCode: 0}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)
	seedSession(t, b, "sess-push", "sb-push")

	content := strings.NewReader("file content here")
	err := b.PushFile(context.Background(), "sess-push", "/home/user/test.txt", content, 0644)
	if err != nil {
		t.Fatalf("PushFile: %v", err)
	}
	// Stdin should contain tar data.
	if len(capturedStdin) == 0 {
		t.Error("expected tar data on stdin")
	}
}

func TestPushFile_EmptyPath(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)
	err := b.PushFile(context.Background(), "sess", "", strings.NewReader("x"), 0644)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestPushFile_RelativePath(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)
	err := b.PushFile(context.Background(), "sess", "relative/path.txt", strings.NewReader("x"), 0644)
	if err == nil {
		t.Fatal("expected error for relative path")
	}
}

func TestPushFile_NilContent(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)
	seedSession(t, b, "sess-nilc", "sb-nilc")
	err := b.PushFile(context.Background(), "sess-nilc", "/tmp/x", nil, 0644)
	if err == nil {
		t.Fatal("expected error for nil content")
	}
}

func TestPushFile_TarExitNonZero(t *testing.T) {
	gw := &mockGateway{
		execFn: func(ctx context.Context, sandboxID string, req ExecRequest) (*ExecResponse, error) {
			return &ExecResponse{ExitCode: 1, Stderr: []byte("permission denied")}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)
	seedSession(t, b, "sess-pushfail", "sb-pushfail")

	err := b.PushFile(context.Background(), "sess-pushfail", "/root/secret", strings.NewReader("x"), 0644)
	if err == nil {
		t.Fatal("expected error for tar failure")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error should contain stderr, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PullFile tests
// ---------------------------------------------------------------------------

func TestPullFile_Success(t *testing.T) {
	// Build a tar archive containing a single file.
	tarData, err := buildSingleFileTar("hello.txt", 0644, []byte("hello world"))
	if err != nil {
		t.Fatalf("buildSingleFileTar: %v", err)
	}

	gw := &mockGateway{
		execFn: func(ctx context.Context, sandboxID string, req ExecRequest) (*ExecResponse, error) {
			return &ExecResponse{ExitCode: 0, Stdout: tarData}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)
	seedSession(t, b, "sess-pull", "sb-pull")

	rc, err := b.PullFile(context.Background(), "sess-pull", "/tmp/hello.txt")
	if err != nil {
		t.Fatalf("PullFile: %v", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("got %q, want %q", data, "hello world")
	}
}

func TestPullFile_EmptyPath(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)
	_, err := b.PullFile(context.Background(), "sess", "")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestPullFile_RelativePath(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)
	_, err := b.PullFile(context.Background(), "sess", "relative/path")
	if err == nil {
		t.Fatal("expected error for relative path")
	}
}

func TestPullFile_TarExitNonZero(t *testing.T) {
	gw := &mockGateway{
		execFn: func(ctx context.Context, sandboxID string, req ExecRequest) (*ExecResponse, error) {
			return &ExecResponse{ExitCode: 2, Stderr: []byte("No such file")}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)
	seedSession(t, b, "sess-pullnf", "sb-pullnf")

	_, err := b.PullFile(context.Background(), "sess-pullnf", "/tmp/missing.txt")
	if err == nil {
		t.Fatal("expected error for tar failure")
	}
}

// ---------------------------------------------------------------------------
// wrapCommand helper tests
// ---------------------------------------------------------------------------

func TestWrapCommand_NoWorkDirNoEnv(t *testing.T) {
	cmd := wrapCommand([]string{"ls", "-la"}, "", nil)
	if len(cmd) != 2 || cmd[0] != "ls" || cmd[1] != "-la" {
		t.Errorf("expected passthrough, got %v", cmd)
	}
}

func TestWrapCommand_WithWorkDir(t *testing.T) {
	cmd := wrapCommand([]string{"make"}, "/app", nil)
	if len(cmd) != 3 || cmd[0] != "/bin/sh" {
		t.Fatalf("expected shell wrap, got %v", cmd)
	}
	if !strings.Contains(cmd[2], "cd '/app'") {
		t.Errorf("should contain cd, got: %s", cmd[2])
	}
	if !strings.Contains(cmd[2], "exec 'make'") {
		t.Errorf("should contain exec, got: %s", cmd[2])
	}
}

func TestWrapCommand_WithEnv(t *testing.T) {
	cmd := wrapCommand([]string{"go", "build"}, "", map[string]string{"GOOS": "linux", "CGO_ENABLED": "0"})
	if len(cmd) != 3 || cmd[0] != "/bin/sh" {
		t.Fatalf("expected shell wrap, got %v", cmd)
	}
	// Env should be sorted.
	if !strings.Contains(cmd[2], "CGO_ENABLED='0' GOOS='linux'") {
		t.Errorf("env should be sorted, got: %s", cmd[2])
	}
}

func TestWrapCommandRaw_NoWorkDirNoEnv(t *testing.T) {
	cmd := wrapCommandRaw([]string{"npx", "tavily-mcp"}, "", nil)
	if len(cmd) != 3 || cmd[0] != "/bin/sh" || cmd[1] != "-c" {
		t.Fatalf("expected shell wrap, got %v", cmd)
	}
	if !strings.HasPrefix(cmd[2], "stty raw -echo 2>/dev/null; ") {
		t.Errorf("should start with stty, got: %s", cmd[2])
	}
	if !strings.Contains(cmd[2], "exec 'npx' 'tavily-mcp'") {
		t.Errorf("should contain exec, got: %s", cmd[2])
	}
	// Should inject TERM=dumb, NODE_OPTIONS, CI, NO_COLOR by default.
	if !strings.Contains(cmd[2], "NODE_OPTIONS='--no-warnings'") {
		t.Errorf("should inject NODE_OPTIONS, got: %s", cmd[2])
	}
	if !strings.Contains(cmd[2], "TERM='dumb'") {
		t.Errorf("should inject TERM=dumb, got: %s", cmd[2])
	}
	if !strings.Contains(cmd[2], "CI='1'") {
		t.Errorf("should inject CI=1, got: %s", cmd[2])
	}
	if !strings.Contains(cmd[2], "NO_COLOR='1'") {
		t.Errorf("should inject NO_COLOR=1, got: %s", cmd[2])
	}
}

func TestWrapCommandRaw_WithWorkDirAndEnv(t *testing.T) {
	cmd := wrapCommandRaw([]string{"node", "server.js"}, "/app", map[string]string{"PORT": "3000"})
	if len(cmd) != 3 || cmd[0] != "/bin/sh" {
		t.Fatalf("expected shell wrap, got %v", cmd)
	}
	script := cmd[2]
	if !strings.HasPrefix(script, "stty raw -echo 2>/dev/null; ") {
		t.Errorf("should start with stty, got: %s", script)
	}
	if !strings.Contains(script, "cd '/app'") {
		t.Errorf("should contain cd, got: %s", script)
	}
	if !strings.Contains(script, "PORT='3000'") {
		t.Errorf("should contain env, got: %s", script)
	}
	if !strings.Contains(script, "exec 'node' 'server.js'") {
		t.Errorf("should contain exec, got: %s", script)
	}
}

func TestWrapCommandRaw_DoesNotOverrideUserEnv(t *testing.T) {
	cmd := wrapCommandRaw([]string{"cmd"}, "", map[string]string{
		"TERM":         "xterm",
		"NODE_OPTIONS": "--max-old-space-size=4096",
		"CI":           "false",
		"NO_COLOR":     "0",
	})
	script := cmd[2]
	// User-provided values should be preserved, not overridden by defaults.
	if !strings.Contains(script, "TERM='xterm'") {
		t.Errorf("should preserve user TERM, got: %s", script)
	}
	if !strings.Contains(script, "NODE_OPTIONS='--max-old-space-size=4096'") {
		t.Errorf("should preserve user NODE_OPTIONS, got: %s", script)
	}
	if !strings.Contains(script, "CI='false'") {
		t.Errorf("should preserve user CI, got: %s", script)
	}
	if !strings.Contains(script, "NO_COLOR='0'") {
		t.Errorf("should preserve user NO_COLOR, got: %s", script)
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"it's", `'it'"'"'s'`},
		{"", "''"},
		{"a b c", "'a b c'"},
	}
	for _, tt := range tests {
		got := shellQuote(tt.input)
		if got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildSingleFileTar(t *testing.T) {
	data, err := buildSingleFileTar("test.sh", 0755, []byte("#!/bin/sh\necho hi"))
	if err != nil {
		t.Fatalf("buildSingleFileTar: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty tar data")
	}
	// Should be a valid tar: minimum size is 512 (header) + content + padding.
	if len(data) < 512 {
		t.Errorf("tar data too small: %d bytes", len(data))
	}
}
