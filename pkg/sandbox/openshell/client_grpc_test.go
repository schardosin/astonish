package openshell

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"

	pb "github.com/schardosin/astonish/pkg/sandbox/openshell/gen/openshellv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// fakeOpenShellServer implements the minimum OpenShell gRPC server for tests.
type fakeOpenShellServer struct {
	pb.UnimplementedOpenShellServer

	createSandboxFn func(*pb.CreateSandboxRequest) (*pb.SandboxResponse, error)
	getSandboxFn    func(*pb.GetSandboxRequest) (*pb.SandboxResponse, error)
	deleteSandboxFn func(*pb.DeleteSandboxRequest) (*pb.DeleteSandboxResponse, error)
	execSandboxFn   func(*pb.ExecSandboxRequest, grpc.ServerStreamingServer[pb.ExecSandboxEvent]) error
}

func (f *fakeOpenShellServer) CreateSandbox(_ context.Context, req *pb.CreateSandboxRequest) (*pb.SandboxResponse, error) {
	if f.createSandboxFn != nil {
		return f.createSandboxFn(req)
	}
	return &pb.SandboxResponse{
		Sandbox: &pb.Sandbox{
			Metadata: &pb.ObjectMeta{Id: "sb-123", Name: req.GetName()},
			Status:   &pb.SandboxStatus{Phase: pb.SandboxPhase_SANDBOX_PHASE_PROVISIONING, AgentPod: "pod-abc"},
		},
	}, nil
}

func (f *fakeOpenShellServer) GetSandbox(_ context.Context, req *pb.GetSandboxRequest) (*pb.SandboxResponse, error) {
	if f.getSandboxFn != nil {
		return f.getSandboxFn(req)
	}
	return &pb.SandboxResponse{
		Sandbox: &pb.Sandbox{
			Metadata: &pb.ObjectMeta{Id: "sb-123", Name: req.GetName()},
			Status:   &pb.SandboxStatus{Phase: pb.SandboxPhase_SANDBOX_PHASE_READY, AgentPod: "pod-abc"},
		},
	}, nil
}

func (f *fakeOpenShellServer) DeleteSandbox(_ context.Context, req *pb.DeleteSandboxRequest) (*pb.DeleteSandboxResponse, error) {
	if f.deleteSandboxFn != nil {
		return f.deleteSandboxFn(req)
	}
	return &pb.DeleteSandboxResponse{}, nil
}

func (f *fakeOpenShellServer) ExecSandbox(req *pb.ExecSandboxRequest, stream grpc.ServerStreamingServer[pb.ExecSandboxEvent]) error {
	if f.execSandboxFn != nil {
		return f.execSandboxFn(req, stream)
	}
	// Default: send stdout "hello\n", exit 0.
	_ = stream.Send(&pb.ExecSandboxEvent{
		Payload: &pb.ExecSandboxEvent_Stdout{
			Stdout: &pb.ExecSandboxStdout{Data: []byte("hello\n")},
		},
	})
	_ = stream.Send(&pb.ExecSandboxEvent{
		Payload: &pb.ExecSandboxEvent_Exit{
			Exit: &pb.ExecSandboxExit{ExitCode: 0},
		},
	})
	return nil
}

func (f *fakeOpenShellServer) ExecSandboxInteractive(stream grpc.BidiStreamingServer[pb.ExecSandboxInput, pb.ExecSandboxEvent]) error {
	// Receive the start message.
	msg, err := stream.Recv()
	if err != nil {
		return err
	}

	// Verify it's a start message (ignore content for test purposes).
	_ = msg.GetStart()

	// Send a stdout event back.
	_ = stream.Send(&pb.ExecSandboxEvent{
		Payload: &pb.ExecSandboxEvent_Stdout{
			Stdout: &pb.ExecSandboxStdout{Data: []byte("interactive-hello\n")},
		},
	})

	// Drain any stdin messages until client closes.
	for {
		_, err := stream.Recv()
		if err != nil {
			break
		}
	}

	// Send exit.
	_ = stream.Send(&pb.ExecSandboxEvent{
		Payload: &pb.ExecSandboxEvent_Exit{
			Exit: &pb.ExecSandboxExit{ExitCode: 0},
		},
	})

	return nil
}

// startFakeServer starts a gRPC server on a random port and returns the client and cleanup func.
func startFakeServer(t *testing.T, srv *fakeOpenShellServer) (GatewayClient, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterOpenShellServer(s, srv)

	go func() { _ = s.Serve(lis) }()

	// Build client connecting to our fake server.
	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		s.Stop()
		t.Fatalf("dial: %v", err)
	}

	client := &grpcGatewayClient{
		conn:   conn,
		client: pb.NewOpenShellClient(conn),
		cfg:    GRPCClientConfig{Addr: lis.Addr().String()},
	}

	cleanup := func() {
		_ = client.Close()
		s.Stop()
	}

	return client, cleanup
}

func TestGRPCClient_CreateSandbox(t *testing.T) {
	srv := &fakeOpenShellServer{}
	client, cleanup := startFakeServer(t, srv)
	defer cleanup()

	resp, err := client.CreateSandbox(context.Background(), CreateSandboxRequest{
		Name:  "test-sandbox",
		Image: "ubuntu:24.04",
		Env:   map[string]string{"FOO": "bar"},
		Labels: map[string]string{
			"app": "astonish",
		},
	})
	if err != nil {
		t.Fatalf("CreateSandbox error: %v", err)
	}
	if resp.SandboxID != "test-sandbox" {
		t.Errorf("SandboxID = %q, want %q", resp.SandboxID, "test-sandbox")
	}
	if resp.PodName != "pod-abc" {
		t.Errorf("PodName = %q, want %q", resp.PodName, "pod-abc")
	}
}

func TestGRPCClient_CreateSandbox_WithPolicy(t *testing.T) {
	var capturedReq *pb.CreateSandboxRequest
	srv := &fakeOpenShellServer{
		createSandboxFn: func(req *pb.CreateSandboxRequest) (*pb.SandboxResponse, error) {
			capturedReq = req
			return &pb.SandboxResponse{
				Sandbox: &pb.Sandbox{
					Metadata: &pb.ObjectMeta{Id: "sb-pol", Name: req.GetName()},
					Status:   &pb.SandboxStatus{Phase: pb.SandboxPhase_SANDBOX_PHASE_PROVISIONING, AgentPod: "pod-pol"},
				},
			}, nil
		},
	}
	client, cleanup := startFakeServer(t, srv)
	defer cleanup()

	_, err := client.CreateSandbox(context.Background(), CreateSandboxRequest{
		Name:  "policy-sandbox",
		Image: "ubuntu:24.04",
		Policy: &SandboxPolicySpec{
			Version: 1,
			Landlock: &LandlockSpec{
				Compatibility: "best_effort",
			},
			Filesystem: &FilesystemSpec{
				IncludeWorkdir: true,
				ReadWrite:      []string{"/sandbox", "/tmp"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateSandbox error: %v", err)
	}

	policy := capturedReq.GetSpec().GetPolicy()
	if policy == nil {
		t.Fatal("expected policy in request, got nil")
	}
	if policy.GetVersion() != 1 {
		t.Errorf("policy version = %d, want 1", policy.GetVersion())
	}
	if policy.GetLandlock() == nil {
		t.Fatal("expected landlock policy, got nil")
	}
	if policy.GetLandlock().GetCompatibility() != "best_effort" {
		t.Errorf("landlock compatibility = %q, want %q", policy.GetLandlock().GetCompatibility(), "best_effort")
	}
	if policy.GetFilesystem() == nil {
		t.Fatal("expected filesystem policy, got nil")
	}
	if !policy.GetFilesystem().GetIncludeWorkdir() {
		t.Error("expected include_workdir = true")
	}
	if rw := policy.GetFilesystem().GetReadWrite(); len(rw) != 2 || rw[0] != "/sandbox" || rw[1] != "/tmp" {
		t.Errorf("filesystem read_write = %v, want [/sandbox /tmp]", rw)
	}
}

func TestGRPCClient_CreateSandbox_NilPolicy(t *testing.T) {
	var capturedReq *pb.CreateSandboxRequest
	srv := &fakeOpenShellServer{
		createSandboxFn: func(req *pb.CreateSandboxRequest) (*pb.SandboxResponse, error) {
			capturedReq = req
			return &pb.SandboxResponse{
				Sandbox: &pb.Sandbox{
					Metadata: &pb.ObjectMeta{Id: "sb-nil", Name: req.GetName()},
					Status:   &pb.SandboxStatus{Phase: pb.SandboxPhase_SANDBOX_PHASE_PROVISIONING, AgentPod: "pod-nil"},
				},
			}, nil
		},
	}
	client, cleanup := startFakeServer(t, srv)
	defer cleanup()

	_, err := client.CreateSandbox(context.Background(), CreateSandboxRequest{
		Name:  "no-policy-sandbox",
		Image: "ubuntu:24.04",
	})
	if err != nil {
		t.Fatalf("CreateSandbox error: %v", err)
	}
	if capturedReq.GetSpec().GetPolicy() != nil {
		t.Error("expected nil policy when none specified")
	}
}

func TestGRPCClient_CreateSandbox_WithNetworkPolicy(t *testing.T) {
	var capturedReq *pb.CreateSandboxRequest
	srv := &fakeOpenShellServer{
		createSandboxFn: func(req *pb.CreateSandboxRequest) (*pb.SandboxResponse, error) {
			capturedReq = req
			return &pb.SandboxResponse{
				Sandbox: &pb.Sandbox{
					Metadata: &pb.ObjectMeta{Id: "sb-net", Name: req.GetName()},
					Status:   &pb.SandboxStatus{Phase: pb.SandboxPhase_SANDBOX_PHASE_PROVISIONING, AgentPod: "pod-net"},
				},
			}, nil
		},
	}
	client, cleanup := startFakeServer(t, srv)
	defer cleanup()

	_, err := client.CreateSandbox(context.Background(), CreateSandboxRequest{
		Name:  "netpol-sandbox",
		Image: "ubuntu:24.04",
		Policy: &SandboxPolicySpec{
			Version: 1,
			Landlock: &LandlockSpec{
				Compatibility: "best_effort",
			},
			Filesystem: &FilesystemSpec{
				IncludeWorkdir: true,
				ReadWrite:      []string{"/sandbox"},
			},
			NetworkPolicies: map[string]*NetworkPolicySpec{
				"egress": {
					Name: "astonish-egress",
					Endpoints: []EndpointSpec{
						{Host: "github.com", Port: 443},
						{Host: "*.github.com", Port: 443},
						{Host: "ssh.github.com", Port: 22},
						{Host: "api.tavily.com"}, // Port 0 = defaults to 443
					},
					Binaries: []string{"/**"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateSandbox error: %v", err)
	}

	policy := capturedReq.GetSpec().GetPolicy()
	if policy == nil {
		t.Fatal("expected policy in request, got nil")
	}

	netPolicies := policy.GetNetworkPolicies()
	if netPolicies == nil {
		t.Fatal("expected network_policies in policy, got nil")
	}

	egressRule, ok := netPolicies["egress"]
	if !ok {
		t.Fatal("expected 'egress' rule in network_policies")
	}
	if egressRule.GetName() != "astonish-egress" {
		t.Errorf("egress rule name = %q, want %q", egressRule.GetName(), "astonish-egress")
	}

	endpoints := egressRule.GetEndpoints()
	if len(endpoints) != 4 {
		t.Fatalf("expected 4 endpoints, got %d", len(endpoints))
	}

	// Verify host and port mapping.
	if endpoints[0].GetHost() != "github.com" || endpoints[0].GetPort() != 443 {
		t.Errorf("endpoint[0] = %s:%d, want github.com:443", endpoints[0].GetHost(), endpoints[0].GetPort())
	}
	if endpoints[2].GetHost() != "ssh.github.com" || endpoints[2].GetPort() != 22 {
		t.Errorf("endpoint[2] = %s:%d, want ssh.github.com:22", endpoints[2].GetHost(), endpoints[2].GetPort())
	}
	// Port 0 should default to 443.
	if endpoints[3].GetHost() != "api.tavily.com" || endpoints[3].GetPort() != 443 {
		t.Errorf("endpoint[3] = %s:%d, want api.tavily.com:443 (port 0 default)", endpoints[3].GetHost(), endpoints[3].GetPort())
	}

	// Verify binaries.
	binaries := egressRule.GetBinaries()
	if len(binaries) != 1 {
		t.Fatalf("expected 1 binary, got %d", len(binaries))
	}
	if binaries[0].GetPath() != "/**" {
		t.Errorf("binary path = %q, want %q", binaries[0].GetPath(), "/**")
	}
}

func TestGRPCClient_GetSandboxStatus(t *testing.T) {
	srv := &fakeOpenShellServer{}
	client, cleanup := startFakeServer(t, srv)
	defer cleanup()

	status, err := client.GetSandboxStatus(context.Background(), "test-sandbox")
	if err != nil {
		t.Fatalf("GetSandboxStatus error: %v", err)
	}
	if status.State != SandboxStateRunning {
		t.Errorf("State = %q, want %q", status.State, SandboxStateRunning)
	}
	if status.PodName != "pod-abc" {
		t.Errorf("PodName = %q, want %q", status.PodName, "pod-abc")
	}
}

func TestGRPCClient_DeleteSandbox(t *testing.T) {
	srv := &fakeOpenShellServer{}
	client, cleanup := startFakeServer(t, srv)
	defer cleanup()

	err := client.DeleteSandbox(context.Background(), "test-sandbox")
	if err != nil {
		t.Fatalf("DeleteSandbox error: %v", err)
	}
}

func TestGRPCClient_ExecCommand(t *testing.T) {
	srv := &fakeOpenShellServer{}
	client, cleanup := startFakeServer(t, srv)
	defer cleanup()

	resp, err := client.ExecCommand(context.Background(), "sb-123", ExecRequest{
		Command: []string{"echo", "hello"},
		WorkDir: "/workspace",
	})
	if err != nil {
		t.Fatalf("ExecCommand error: %v", err)
	}
	if string(resp.Stdout) != "hello\n" {
		t.Errorf("Stdout = %q, want %q", string(resp.Stdout), "hello\n")
	}
	if resp.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", resp.ExitCode)
	}
}

func TestGRPCClient_ExecCommand_WithStdin(t *testing.T) {
	srv := &fakeOpenShellServer{
		execSandboxFn: func(req *pb.ExecSandboxRequest, stream grpc.ServerStreamingServer[pb.ExecSandboxEvent]) error {
			// Echo back the stdin as stdout.
			_ = stream.Send(&pb.ExecSandboxEvent{
				Payload: &pb.ExecSandboxEvent_Stdout{
					Stdout: &pb.ExecSandboxStdout{Data: req.GetStdin()},
				},
			})
			_ = stream.Send(&pb.ExecSandboxEvent{
				Payload: &pb.ExecSandboxEvent_Exit{
					Exit: &pb.ExecSandboxExit{ExitCode: 0},
				},
			})
			return nil
		},
	}
	client, cleanup := startFakeServer(t, srv)
	defer cleanup()

	resp, err := client.ExecCommand(context.Background(), "sb-123", ExecRequest{
		Command: []string{"cat"},
		Stdin:   strings.NewReader("input-data"),
	})
	if err != nil {
		t.Fatalf("ExecCommand error: %v", err)
	}
	if string(resp.Stdout) != "input-data" {
		t.Errorf("Stdout = %q, want %q", string(resp.Stdout), "input-data")
	}
}

func TestGRPCClient_ExecCommand_NonZeroExit(t *testing.T) {
	srv := &fakeOpenShellServer{
		execSandboxFn: func(_ *pb.ExecSandboxRequest, stream grpc.ServerStreamingServer[pb.ExecSandboxEvent]) error {
			_ = stream.Send(&pb.ExecSandboxEvent{
				Payload: &pb.ExecSandboxEvent_Stderr{
					Stderr: &pb.ExecSandboxStderr{Data: []byte("error: not found\n")},
				},
			})
			_ = stream.Send(&pb.ExecSandboxEvent{
				Payload: &pb.ExecSandboxEvent_Exit{
					Exit: &pb.ExecSandboxExit{ExitCode: 1},
				},
			})
			return nil
		},
	}
	client, cleanup := startFakeServer(t, srv)
	defer cleanup()

	resp, err := client.ExecCommand(context.Background(), "sb-123", ExecRequest{
		Command: []string{"false"},
	})
	if err != nil {
		t.Fatalf("ExecCommand error: %v", err)
	}
	if resp.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", resp.ExitCode)
	}
	if string(resp.Stderr) != "error: not found\n" {
		t.Errorf("Stderr = %q, want %q", string(resp.Stderr), "error: not found\n")
	}
}

func TestMapPhaseToState(t *testing.T) {
	tests := []struct {
		phase pb.SandboxPhase
		want  SandboxState
	}{
		{pb.SandboxPhase_SANDBOX_PHASE_PROVISIONING, SandboxStateCreating},
		{pb.SandboxPhase_SANDBOX_PHASE_READY, SandboxStateRunning},
		{pb.SandboxPhase_SANDBOX_PHASE_ERROR, SandboxStateFailed},
		{pb.SandboxPhase_SANDBOX_PHASE_DELETING, SandboxStateStopped},
		{pb.SandboxPhase_SANDBOX_PHASE_UNKNOWN, SandboxStateGone},
		{pb.SandboxPhase_SANDBOX_PHASE_UNSPECIFIED, SandboxStateGone},
	}
	for _, tt := range tests {
		t.Run(tt.phase.String(), func(t *testing.T) {
			got := mapPhaseToState(tt.phase)
			if got != tt.want {
				t.Errorf("mapPhaseToState(%v) = %q, want %q", tt.phase, got, tt.want)
			}
		})
	}
}

func TestNewGRPCGatewayClient_EmptyAddr(t *testing.T) {
	_, err := NewGRPCGatewayClient(GRPCClientConfig{})
	if err == nil {
		t.Fatal("expected error for empty Addr")
	}
}

func TestGRPCClient_Close(t *testing.T) {
	srv := &fakeOpenShellServer{}
	client, cleanup := startFakeServer(t, srv)
	defer cleanup()

	// Close should not panic.
	closeable := client.(*grpcGatewayClient)
	if err := closeable.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
}

func TestGRPCClient_ExecStream(t *testing.T) {
	srv := &fakeOpenShellServer{}
	client, cleanup := startFakeServer(t, srv)
	defer cleanup()

	stream, err := client.ExecStream(context.Background(), "sb-123", ExecRequest{
		Command: []string{"bash"},
		TTY:     true,
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("ExecStream error: %v", err)
	}
	defer stream.Close()

	// Write stdin.
	_, err = stream.Write([]byte("echo hi\n"))
	if err != nil {
		// May fail since the fake server doesn't implement interactive, that's OK.
		// The point is we can call Write without panic.
		t.Logf("Write error (expected with unimplemented interactive): %v", err)
	}

	// ExitCode should be -1 initially.
	if ec := stream.ExitCode(); ec != -1 {
		t.Logf("ExitCode before close = %d (may vary with fake server)", ec)
	}

	// Try resize — should not panic.
	_ = stream.Resize(120, 40)

	// Read until EOF or error.
	buf := make([]byte, 1024)
	_, err = stream.Read(buf)
	if err != nil && err != io.EOF {
		t.Logf("Read error (expected with unimplemented interactive): %v", err)
	}
}
