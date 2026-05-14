// Package k8s — exec_test.go drives Exec and ExecInteractive against a
// stub remoteExecutor so we can assert:
//
//   - ctx-cancellation short-circuit (pre-RPC).
//   - session/pod lookup (error when session missing or PodName empty).
//   - Command validation (empty command rejected).
//   - StreamOptions wiring (stdin/stdout/stderr/tty/size queue).
//   - Exit-code decoding for both zero and non-zero remote exits,
//     plus transport failures.
//   - Interactive stream bridging: Read/Write/Resize/Wait/Close round
//     trips.
//
// The tests bypass SPDY entirely by replacing execExecutorFactory with a
// stub factory that returns a stubExecutor. The stub runs callbacks
// supplied per test (so each test controls what the "remote process"
// does) and synthesises exit codes via k8s.io/client-go/util/exec.CodeExitError.

package k8s

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	k8sexec "k8s.io/client-go/util/exec"

	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store"
)

// ---------------------------------------------------------------------------
// Stub executor
// ---------------------------------------------------------------------------

// stubExecutor captures the StreamOptions it received and runs a
// user-supplied behaviour function. If behave returns an error, that's
// what StreamWithContext returns; otherwise it returns nil (exit 0).
type stubExecutor struct {
	mu      sync.Mutex
	opts    remotecommand.StreamOptions
	called  bool
	method  string
	url     *url.URL
	restCfg *rest.Config

	behave func(ctx context.Context, opts remotecommand.StreamOptions) error
}

func (s *stubExecutor) StreamWithContext(ctx context.Context, opts remotecommand.StreamOptions) error {
	s.mu.Lock()
	s.opts = opts
	s.called = true
	s.mu.Unlock()
	if s.behave == nil {
		return nil
	}
	return s.behave(ctx, opts)
}

// stubFactory installs a stubExecutor on b and returns it so tests can
// assert on captured args afterward. The behave function is what the
// executor will run when Exec/ExecInteractive trigger
// StreamWithContext.
func stubFactory(t *testing.T, b *K8sBackend, behave func(ctx context.Context, opts remotecommand.StreamOptions) error) *stubExecutor {
	t.Helper()
	se := &stubExecutor{behave: behave}
	b.execExecutorFactory = func(cfg *rest.Config, method string, u *url.URL) (remoteExecutor, error) {
		se.mu.Lock()
		se.restCfg = cfg
		se.method = method
		se.url = u
		se.mu.Unlock()
		return se, nil
	}
	// Exec needs a non-nil RESTConfig for buildExecURL; we don't use
	// it against a real API server so empty is fine.
	b.restConfig = &rest.Config{Host: "https://example.test"}
	return se
}

// seedSession inserts a registry row pointing at a pod name so Exec's
// session-lookup path succeeds. Returns the session ID used.
func seedSession(t *testing.T, b *K8sBackend, sessionID, podName string) {
	t.Helper()
	if err := b.sessions.PutSession(&store.SandboxSession{
		SessionID: sessionID,
		Backend:   string(sandbox.BackendKindK8s),
		PodName:   podName,
		State:     store.SandboxSessionStateRunning,
	}); err != nil {
		t.Fatalf("seedSession: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Exec (non-interactive)
// ---------------------------------------------------------------------------

func TestExec_ContextCancelledShortCircuits(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	stubFactory(t, b, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.Exec(ctx, "s", sandbox.ExecSpec{Command: []string{"true"}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Exec cancelled: got %v, want context.Canceled", err)
	}
}

func TestExec_EmptyCommandRejected(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	stubFactory(t, b, nil)
	_, err := b.Exec(context.Background(), "s", sandbox.ExecSpec{})
	if err == nil || !strings.Contains(err.Error(), "Command is required") {
		t.Errorf("Exec empty command: got %v, want Command-required", err)
	}
}

func TestExec_MissingSessionRejected(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	stubFactory(t, b, nil)
	_, err := b.Exec(context.Background(), "nope", sandbox.ExecSpec{Command: []string{"true"}})
	if err == nil || !strings.Contains(err.Error(), "no pod") {
		t.Errorf("Exec missing session: got %v, want no-pod error", err)
	}
}

func TestExec_ZeroExitCaptures(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	seedSession(t, b, "s1", "astn-sess-s1")
	se := stubFactory(t, b, func(_ context.Context, opts remotecommand.StreamOptions) error {
		if opts.Stdout != nil {
			_, _ = opts.Stdout.Write([]byte("hello"))
		}
		if opts.Stderr != nil {
			_, _ = opts.Stderr.Write([]byte("warn"))
		}
		return nil // exit 0
	})

	res, err := b.Exec(context.Background(), "s1", sandbox.ExecSpec{
		Command: []string{"echo", "hi"},
		Env:     map[string]string{"FOO": "bar"},
		WorkDir: "/work",
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if string(res.Stdout) != "hello" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, "hello")
	}
	if string(res.Stderr) != "warn" {
		t.Errorf("Stderr = %q, want %q", res.Stderr, "warn")
	}

	// URL must target the right namespace/pod/exec subresource.
	if se.url == nil {
		t.Fatal("executor factory not called")
	}
	if !strings.Contains(se.url.Path, "/namespaces/astonish-sandboxes/pods/astn-sess-s1/exec") {
		t.Errorf("URL path = %q, want pod exec subresource", se.url.Path)
	}
	if se.method != "POST" {
		t.Errorf("method = %q, want POST", se.method)
	}
	// Command wrapping: we passed WorkDir + Env so the command MUST
	// be /bin/sh -c "...".
	q := se.url.Query()
	cmds := q["command"]
	if len(cmds) == 0 || cmds[0] != "/bin/sh" {
		t.Errorf("command[0] = %v, want /bin/sh", cmds)
	}
	// Expect FOO='bar', cd '/work', exec 'echo' 'hi' in the -c arg.
	script := strings.Join(cmds, " ")
	for _, want := range []string{"cd '/work'", "FOO='bar'", "exec 'echo' 'hi'"} {
		if !strings.Contains(script, want) {
			t.Errorf("wrapped command missing %q: %s", want, script)
		}
	}
	if q.Get("container") != "sandbox" {
		t.Errorf("container = %q, want sandbox", q.Get("container"))
	}
	if q.Get("tty") == "true" {
		t.Error("Exec non-interactive must not set tty=true")
	}
}

func TestExec_NonZeroExitDecoded(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	seedSession(t, b, "s1", "astn-sess-s1")
	stubFactory(t, b, func(_ context.Context, _ remotecommand.StreamOptions) error {
		return k8sexec.CodeExitError{Err: fmt.Errorf("exit 42"), Code: 42}
	})

	res, err := b.Exec(context.Background(), "s1", sandbox.ExecSpec{Command: []string{"false"}})
	if err != nil {
		t.Fatalf("Exec: unexpected transport error %v", err)
	}
	if res.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", res.ExitCode)
	}
}

func TestExec_TransportErrorSurfaced(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	seedSession(t, b, "s1", "astn-sess-s1")
	boom := errors.New("connection reset")
	stubFactory(t, b, func(_ context.Context, _ remotecommand.StreamOptions) error {
		return boom
	})

	_, err := b.Exec(context.Background(), "s1", sandbox.ExecSpec{Command: []string{"x"}})
	if err == nil || !errors.Is(err, boom) {
		t.Errorf("Exec transport err: got %v, want wraps %v", err, boom)
	}
}

func TestExec_NoRESTConfigRejected(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	seedSession(t, b, "s1", "astn-sess-s1")
	// Install stub executor but DO NOT set restConfig.
	b.execExecutorFactory = func(_ *rest.Config, _ string, _ *url.URL) (remoteExecutor, error) {
		return &stubExecutor{}, nil
	}
	b.restConfig = nil

	_, err := b.Exec(context.Background(), "s1", sandbox.ExecSpec{Command: []string{"x"}})
	if err == nil || !strings.Contains(err.Error(), "RESTConfig is required") {
		t.Errorf("Exec without RESTConfig: got %v, want RESTConfig-required", err)
	}
}

func TestExec_StdinForwarded(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	seedSession(t, b, "s1", "astn-sess-s1")
	var got bytes.Buffer
	stubFactory(t, b, func(_ context.Context, opts remotecommand.StreamOptions) error {
		if opts.Stdin == nil {
			return errors.New("expected stdin")
		}
		_, _ = io.Copy(&got, opts.Stdin)
		return nil
	})

	_, err := b.Exec(context.Background(), "s1", sandbox.ExecSpec{
		Command: []string{"cat"},
		Stdin:   strings.NewReader("payload"),
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got.String() != "payload" {
		t.Errorf("stdin received = %q, want %q", got.String(), "payload")
	}
}

// ---------------------------------------------------------------------------
// ExecInteractive (PTY)
// ---------------------------------------------------------------------------

func TestExecInteractive_ContextCancelledShortCircuits(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	stubFactory(t, b, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.ExecInteractive(ctx, "s", sandbox.PTYSpec{Command: []string{"sh"}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("ExecInteractive cancelled: got %v, want context.Canceled", err)
	}
}

func TestExecInteractive_EmptyCommandRejected(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	stubFactory(t, b, nil)
	_, err := b.ExecInteractive(context.Background(), "s", sandbox.PTYSpec{})
	if err == nil || !strings.Contains(err.Error(), "Command is required") {
		t.Errorf("ExecInteractive empty command: got %v, want Command-required", err)
	}
}

func TestExecInteractive_StreamsAndWait(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	seedSession(t, b, "s1", "astn-sess-s1")

	// Behaviour: read one line from stdin, echo it to stdout, exit 0.
	se := stubFactory(t, b, func(ctx context.Context, opts remotecommand.StreamOptions) error {
		if !opts.Tty {
			return errors.New("expected tty")
		}
		if opts.TerminalSizeQueue == nil {
			return errors.New("expected TerminalSizeQueue")
		}
		buf := make([]byte, 64)
		n, err := opts.Stdin.Read(buf)
		if err != nil {
			return fmt.Errorf("stub stdin read: %w", err)
		}
		_, _ = opts.Stdout.Write(buf[:n])
		// Consume one resize event to verify the queue is live.
		_ = opts.TerminalSizeQueue.Next()
		return nil
	})

	stream, err := b.ExecInteractive(context.Background(), "s1", sandbox.PTYSpec{
		Command: []string{"sh"},
		Rows:    40,
		Cols:    120,
	})
	if err != nil {
		t.Fatalf("ExecInteractive: %v", err)
	}

	// Write input.
	if _, err := stream.Write([]byte("ping\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Resize to nudge the queue again (initial resize already seeded).
	if err := stream.Resize(50, 132); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	// Read echoed output.
	out := make([]byte, 64)
	n, err := stream.Read(out)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read: %v", err)
	}
	if string(out[:n]) != "ping\n" {
		t.Errorf("Read = %q, want %q", out[:n], "ping\n")
	}

	code, werr := stream.Wait()
	if werr != nil {
		t.Errorf("Wait: %v", werr)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if err := stream.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	// TTY URL query must advertise tty=true and stdin=true.
	q := se.url.Query()
	if q.Get("tty") != "true" {
		t.Errorf("tty query = %q, want true", q.Get("tty"))
	}
}

func TestExecInteractive_NonZeroExitViaWait(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	seedSession(t, b, "s1", "astn-sess-s1")
	stubFactory(t, b, func(ctx context.Context, opts remotecommand.StreamOptions) error {
		return k8sexec.CodeExitError{Err: fmt.Errorf("exit 7"), Code: 7}
	})
	stream, err := b.ExecInteractive(context.Background(), "s1", sandbox.PTYSpec{
		Command: []string{"sh"},
		Rows:    24, Cols: 80,
	})
	if err != nil {
		t.Fatalf("ExecInteractive: %v", err)
	}
	defer stream.Close()
	code, werr := stream.Wait()
	if werr != nil {
		t.Errorf("Wait: unexpected transport err %v", werr)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

func TestExecInteractive_SeparateStderr(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	seedSession(t, b, "s1", "astn-sess-s1")

	var stderr bytes.Buffer
	stubFactory(t, b, func(_ context.Context, opts remotecommand.StreamOptions) error {
		if opts.Stderr == nil {
			return errors.New("expected separate stderr")
		}
		_, _ = opts.Stderr.Write([]byte("err!"))
		return nil
	})

	stream, err := b.ExecInteractive(context.Background(), "s1", sandbox.PTYSpec{
		Command:        []string{"sh"},
		SeparateStderr: &stderr,
	})
	if err != nil {
		t.Fatalf("ExecInteractive: %v", err)
	}
	defer stream.Close()
	code, werr := stream.Wait()
	if werr != nil || code != 0 {
		t.Errorf("Wait = (%d, %v), want (0, nil)", code, werr)
	}
	if stderr.String() != "err!" {
		t.Errorf("separate stderr = %q, want %q", stderr.String(), "err!")
	}
}

func TestExecInteractive_ResizeDropsOldest(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	seedSession(t, b, "s1", "astn-sess-s1")

	// Never-call behaviour so the stream stays open until we close it.
	ready := make(chan struct{})
	done := make(chan struct{})
	stubFactory(t, b, func(ctx context.Context, opts remotecommand.StreamOptions) error {
		close(ready)
		<-ctx.Done()
		close(done)
		return ctx.Err()
	})

	stream, err := b.ExecInteractive(context.Background(), "s1", sandbox.PTYSpec{
		Command: []string{"sh"},
	})
	if err != nil {
		t.Fatalf("ExecInteractive: %v", err)
	}
	<-ready

	// Push way more than the queue capacity (4). Resize must never
	// block even though no one is draining the queue.
	resizeDone := make(chan error, 1)
	go func() {
		for i := 0; i < 100; i++ {
			if err := stream.Resize(24, 80+i); err != nil {
				resizeDone <- err
				return
			}
		}
		resizeDone <- nil
	}()
	select {
	case err := <-resizeDone:
		if err != nil {
			t.Errorf("Resize: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Resize blocked — drop-oldest not honoured")
	}
	_ = stream.Close()
	<-done
}

func TestExecInteractive_ResizeRejectsInvalid(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	seedSession(t, b, "s1", "astn-sess-s1")
	done := make(chan struct{})
	stubFactory(t, b, func(ctx context.Context, _ remotecommand.StreamOptions) error {
		<-ctx.Done()
		close(done)
		return ctx.Err()
	})

	stream, err := b.ExecInteractive(context.Background(), "s1", sandbox.PTYSpec{
		Command: []string{"sh"},
	})
	if err != nil {
		t.Fatalf("ExecInteractive: %v", err)
	}
	defer func() { _ = stream.Close(); <-done }()
	if err := stream.Resize(0, 80); err == nil {
		t.Error("Resize(0, 80) should error")
	}
	if err := stream.Resize(24, -1); err == nil {
		t.Error("Resize(24, -1) should error")
	}
}

// ---------------------------------------------------------------------------
// Helper unit tests
// ---------------------------------------------------------------------------

func TestDecodeExitError(t *testing.T) {
	code, err := decodeExitError(nil)
	if code != 0 || err != nil {
		t.Errorf("decodeExitError(nil) = (%d, %v), want (0, nil)", code, err)
	}
	code, err = decodeExitError(k8sexec.CodeExitError{Err: fmt.Errorf("x"), Code: 5})
	if code != 5 || err != nil {
		t.Errorf("decodeExitError(CodeExitError{5}) = (%d, %v), want (5, nil)", code, err)
	}
	boom := errors.New("boom")
	code, err = decodeExitError(boom)
	if code != -1 || !errors.Is(err, boom) {
		t.Errorf("decodeExitError(boom) = (%d, %v), want (-1, wraps boom)", code, err)
	}
}

func TestWrapCommand(t *testing.T) {
	cases := []struct {
		name    string
		cmd     []string
		workDir string
		env     map[string]string
		wantLen int
		mustHas []string
		verbatim bool
	}{
		{
			name:    "verbatim when no workdir and no env",
			cmd:     []string{"echo", "hi"},
			verbatim: true,
			wantLen: 2,
		},
		{
			name:    "workdir only",
			cmd:     []string{"ls"},
			workDir: "/tmp",
			wantLen: 3,
			mustHas: []string{"cd '/tmp'", "exec 'ls'"},
		},
		{
			name:    "env only sorted",
			cmd:     []string{"sh"},
			env:     map[string]string{"B": "2", "A": "1"},
			wantLen: 3,
			mustHas: []string{"export A='1' B='2'", "exec 'sh'"},
		},
		{
			name:    "quotes escaped",
			cmd:     []string{"say", "it's"},
			workDir: "/x",
			wantLen: 3,
			mustHas: []string{`'it'"'"'s'`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := wrapCommand(c.cmd, c.workDir, c.env)
			if len(got) != c.wantLen {
				t.Errorf("len = %d, want %d: %v", len(got), c.wantLen, got)
			}
			if c.verbatim {
				if got[0] != "echo" {
					t.Errorf("verbatim wrap changed argv: %v", got)
				}
				return
			}
			script := got[2]
			for _, want := range c.mustHas {
				if !strings.Contains(script, want) {
					t.Errorf("script missing %q: %s", want, script)
				}
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"simple", "'simple'"},
		{"", "''"},
		{"it's", `'it'"'"'s'`},
		{"multi'quote'here", `'multi'"'"'quote'"'"'here'`},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
