// Package openshell — client_grpc.go implements the GatewayClient interface
// using NVIDIA OpenShell's gRPC API (openshell.v1.OpenShell service).
//
// The client handles connection lifecycle, TLS/mTLS configuration, and maps
// our domain types to the generated proto request/response messages.
//
// Reference: docs/architecture/openshell-sandbox-backend.md §6.

package openshell

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	pb "github.com/schardosin/astonish/pkg/sandbox/openshell/gen/openshellv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// GRPCClientConfig configures the gRPC gateway client connection.
type GRPCClientConfig struct {
	// Addr is the gateway gRPC endpoint (host:port).
	Addr string

	// TLS enables TLS. When false, an insecure connection is used.
	TLS bool

	// ClientCertPath / ClientKeyPath configure mTLS client authentication.
	ClientCertPath string
	ClientKeyPath  string

	// CACertPath is the CA certificate for verifying the gateway. Empty uses system CAs.
	CACertPath string

	// AuthToken is a static bearer token sent in the Authorization header.
	AuthToken string
}

// grpcGatewayClient implements GatewayClient using the NVIDIA OpenShell gRPC API.
type grpcGatewayClient struct {
	conn   *grpc.ClientConn
	client pb.OpenShellClient
	cfg    GRPCClientConfig
}

// NewGRPCGatewayClient creates a new gRPC client connected to the OpenShell gateway.
func NewGRPCGatewayClient(cfg GRPCClientConfig) (GatewayClient, error) {
	if cfg.Addr == "" {
		return nil, errors.New("sandbox/openshell: GRPCClientConfig.Addr is required")
	}

	dialOpts, err := buildDialOptions(cfg)
	if err != nil {
		return nil, fmt.Errorf("sandbox/openshell: build dial options: %w", err)
	}

	conn, err := grpc.NewClient(cfg.Addr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("sandbox/openshell: dial gateway %s: %w", cfg.Addr, err)
	}

	return &grpcGatewayClient{
		conn:   conn,
		client: pb.NewOpenShellClient(conn),
		cfg:    cfg,
	}, nil
}

// Close closes the underlying gRPC connection.
func (c *grpcGatewayClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func buildDialOptions(cfg GRPCClientConfig) ([]grpc.DialOption, error) {
	var opts []grpc.DialOption

	if !cfg.TLS {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tlsCfg := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}

		// Load CA cert if provided.
		if cfg.CACertPath != "" {
			caCert, err := os.ReadFile(cfg.CACertPath)
			if err != nil {
				return nil, fmt.Errorf("read CA cert %s: %w", cfg.CACertPath, err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to parse CA cert %s", cfg.CACertPath)
			}
			tlsCfg.RootCAs = pool
		}

		// Load mTLS client cert if provided.
		if cfg.ClientCertPath != "" && cfg.ClientKeyPath != "" {
			cert, err := tls.LoadX509KeyPair(cfg.ClientCertPath, cfg.ClientKeyPath)
			if err != nil {
				return nil, fmt.Errorf("load client cert: %w", err)
			}
			tlsCfg.Certificates = []tls.Certificate{cert}
		}

		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	}

	// Add bearer token interceptor if configured.
	if cfg.AuthToken != "" {
		opts = append(opts, grpc.WithUnaryInterceptor(bearerTokenUnaryInterceptor(cfg.AuthToken)))
		opts = append(opts, grpc.WithStreamInterceptor(bearerTokenStreamInterceptor(cfg.AuthToken)))
	}

	return opts, nil
}

func bearerTokenUnaryInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func bearerTokenStreamInterceptor(token string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
		return streamer(ctx, desc, cc, method, opts...)
	}
}

// ---------------------------------------------------------------------------
// GatewayClient interface implementation
// ---------------------------------------------------------------------------

func (c *grpcGatewayClient) CreateSandbox(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
	pbReq := &pb.CreateSandboxRequest{
		Name:   req.Name,
		Labels: req.Labels,
		Spec: &pb.SandboxSpec{
			Template: &pb.SandboxTemplate{
				Image:       req.Image,
				Environment: req.Env,
				Labels:      req.Labels,
			},
			Policy: mapPolicyToProto(req.Policy),
		},
	}

	resp, err := c.client.CreateSandbox(ctx, pbReq)
	if err != nil {
		return nil, fmt.Errorf("gateway CreateSandbox: %w", err)
	}

	sandbox := resp.GetSandbox()
	if sandbox == nil {
		return nil, errors.New("gateway CreateSandbox: nil sandbox in response")
	}

	meta := sandbox.GetMetadata()
	status := sandbox.GetStatus()

	return &CreateSandboxResponse{
		SandboxID: meta.GetName(),
		GatewayID: meta.GetId(),
		PodName:   status.GetAgentPod(),
	}, nil
}

func (c *grpcGatewayClient) DeleteSandbox(ctx context.Context, sandboxID string) error {
	// The gateway uses sandbox name for deletion, not ID.
	// We store the sandbox name in ContainerName in our session registry.
	_, err := c.client.DeleteSandbox(ctx, &pb.DeleteSandboxRequest{
		Name: sandboxID,
	})
	if err != nil {
		return fmt.Errorf("gateway DeleteSandbox: %w", err)
	}
	return nil
}

func (c *grpcGatewayClient) GetSandboxStatus(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
	resp, err := c.client.GetSandbox(ctx, &pb.GetSandboxRequest{
		Name: sandboxID,
	})
	if err != nil {
		return nil, fmt.Errorf("gateway GetSandbox: %w", err)
	}

	sandbox := resp.GetSandbox()
	if sandbox == nil {
		return nil, errors.New("gateway GetSandbox: nil sandbox in response")
	}

	meta := sandbox.GetMetadata()
	status := sandbox.GetStatus()
	phase := status.GetPhase()

	return &SandboxStatus{
		State:     mapPhaseToState(phase),
		Message:   phaseMessage(phase, status),
		PodName:   status.GetAgentPod(),
		GatewayID: meta.GetId(),
	}, nil
}

func (c *grpcGatewayClient) ExecCommand(ctx context.Context, sandboxID string, req ExecRequest) (*ExecResponse, error) {
	pbReq := &pb.ExecSandboxRequest{
		SandboxId:   sandboxID,
		Command:     req.Command,
		Workdir:     req.WorkDir,
		Environment: req.Env,
		Tty:         req.TTY,
		Cols:        uint32(req.Cols),
		Rows:        uint32(req.Rows),
	}

	// Read stdin if provided.
	if req.Stdin != nil {
		data, err := io.ReadAll(req.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		pbReq.Stdin = data
	}

	stream, err := c.client.ExecSandbox(ctx, pbReq)
	if err != nil {
		return nil, fmt.Errorf("gateway ExecSandbox: %w", err)
	}

	var stdout, stderr bytes.Buffer
	var exitCode int32

	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gateway ExecSandbox recv: %w", err)
		}

		switch payload := event.GetPayload().(type) {
		case *pb.ExecSandboxEvent_Stdout:
			stdout.Write(payload.Stdout.GetData())
		case *pb.ExecSandboxEvent_Stderr:
			stderr.Write(payload.Stderr.GetData())
		case *pb.ExecSandboxEvent_Exit:
			exitCode = payload.Exit.GetExitCode()
		}
	}

	return &ExecResponse{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: int(exitCode),
	}, nil
}

func (c *grpcGatewayClient) ExecStream(ctx context.Context, sandboxID string, req ExecRequest) (ExecStreamConn, error) {
	stream, err := c.client.ExecSandboxInteractive(ctx)
	if err != nil {
		return nil, fmt.Errorf("gateway ExecSandboxInteractive: %w", err)
	}

	// Send the initial start message.
	startMsg := &pb.ExecSandboxInput{
		Payload: &pb.ExecSandboxInput_Start{
			Start: &pb.ExecSandboxRequest{
				SandboxId:   sandboxID,
				Command:     req.Command,
				Workdir:     req.WorkDir,
				Environment: req.Env,
				Tty:         req.TTY,
				Cols:        uint32(req.Cols),
				Rows:        uint32(req.Rows),
			},
		},
	}
	if err := stream.Send(startMsg); err != nil {
		return nil, fmt.Errorf("gateway ExecSandboxInteractive send start: %w", err)
	}

	conn := &grpcExecStreamConn{
		stream:   stream,
		exitCode: -1,
	}
	conn.cond = sync.NewCond(&conn.mu)
	// Start background reader.
	go conn.readLoop()

	return conn, nil
}

// ---------------------------------------------------------------------------
// grpcExecStreamConn — implements ExecStreamConn
// ---------------------------------------------------------------------------

type grpcExecStreamConn struct {
	stream   pb.OpenShell_ExecSandboxInteractiveClient
	buf      bytes.Buffer
	mu       sync.Mutex
	cond     *sync.Cond
	exitCode int
	closed   bool
	readErr  error
}

func (c *grpcExecStreamConn) readLoop() {
	for {
		event, err := c.stream.Recv()
		if err != nil {
			c.mu.Lock()
			if err != io.EOF {
				c.readErr = err
			}
			c.closed = true
			c.cond.Broadcast()
			c.mu.Unlock()
			return
		}

		c.mu.Lock()
		switch payload := event.GetPayload().(type) {
		case *pb.ExecSandboxEvent_Stdout:
			c.buf.Write(payload.Stdout.GetData())
		case *pb.ExecSandboxEvent_Stderr:
			c.buf.Write(payload.Stderr.GetData())
		case *pb.ExecSandboxEvent_Exit:
			c.exitCode = int(payload.Exit.GetExitCode())
			c.closed = true
		}
		c.cond.Broadcast()
		c.mu.Unlock()
	}
}

func (c *grpcExecStreamConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for c.buf.Len() == 0 && !c.closed {
		c.cond.Wait()
	}

	if c.buf.Len() > 0 {
		return c.buf.Read(p)
	}
	if c.readErr != nil {
		return 0, c.readErr
	}
	return 0, io.EOF
}

func (c *grpcExecStreamConn) Write(p []byte) (int, error) {
	msg := &pb.ExecSandboxInput{
		Payload: &pb.ExecSandboxInput_Stdin{
			Stdin: p,
		},
	}
	if err := c.stream.Send(msg); err != nil {
		return 0, fmt.Errorf("send stdin: %w", err)
	}
	return len(p), nil
}

func (c *grpcExecStreamConn) Resize(cols, rows int) error {
	msg := &pb.ExecSandboxInput{
		Payload: &pb.ExecSandboxInput_Resize{
			Resize: &pb.ExecSandboxWindowResize{
				Cols: uint32(cols),
				Rows: uint32(rows),
			},
		},
	}
	return c.stream.Send(msg)
}

func (c *grpcExecStreamConn) ExitCode() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitCode
}

func (c *grpcExecStreamConn) Close() error {
	return c.stream.CloseSend()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mapPhaseToState converts NVIDIA's SandboxPhase to our SandboxState.
func mapPhaseToState(phase pb.SandboxPhase) SandboxState {
	switch phase {
	case pb.SandboxPhase_SANDBOX_PHASE_PROVISIONING:
		return SandboxStateCreating
	case pb.SandboxPhase_SANDBOX_PHASE_READY:
		return SandboxStateRunning
	case pb.SandboxPhase_SANDBOX_PHASE_ERROR:
		return SandboxStateFailed
	case pb.SandboxPhase_SANDBOX_PHASE_DELETING:
		return SandboxStateStopped
	case pb.SandboxPhase_SANDBOX_PHASE_UNKNOWN, pb.SandboxPhase_SANDBOX_PHASE_UNSPECIFIED:
		return SandboxStateGone
	default:
		return SandboxStateGone
	}
}

func phaseMessage(phase pb.SandboxPhase, status *pb.SandboxStatus) string {
	if status == nil {
		return phase.String()
	}
	// Check conditions for more detailed messages.
	for _, cond := range status.GetConditions() {
		if cond.GetStatus() == "False" && cond.GetMessage() != "" {
			return cond.GetMessage()
		}
	}
	return phase.String()
}

// mapPolicyToProto converts the domain SandboxPolicySpec to the proto
// SandboxPolicy message. Returns nil when spec is nil (gateway applies
// its default policy).
func mapPolicyToProto(spec *SandboxPolicySpec) *pb.SandboxPolicy {
	if spec == nil {
		return nil
	}
	p := &pb.SandboxPolicy{
		Version: spec.Version,
	}
	if spec.Landlock != nil {
		p.Landlock = &pb.LandlockPolicy{
			Compatibility: spec.Landlock.Compatibility,
		}
	}
	if spec.Filesystem != nil {
		p.Filesystem = &pb.FilesystemPolicy{
			IncludeWorkdir: spec.Filesystem.IncludeWorkdir,
			ReadOnly:       spec.Filesystem.ReadOnly,
			ReadWrite:      spec.Filesystem.ReadWrite,
		}
	}
	if spec.Process != nil {
		p.Process = &pb.ProcessPolicy{
			RunAsUser:  spec.Process.RunAsUser,
			RunAsGroup: spec.Process.RunAsGroup,
		}
	}
	if len(spec.NetworkPolicies) > 0 {
		p.NetworkPolicies = make(map[string]*pb.NetworkPolicyRule, len(spec.NetworkPolicies))
		for k, v := range spec.NetworkPolicies {
			rule := &pb.NetworkPolicyRule{
				Name: v.Name,
			}
			for _, ep := range v.Endpoints {
				port := ep.Port
				if port == 0 {
					port = 443
				}
				rule.Endpoints = append(rule.Endpoints, &pb.NetworkEndpoint{
					Host: ep.Host,
					Port: port,
				})
			}
			for _, bin := range v.Binaries {
				rule.Binaries = append(rule.Binaries, &pb.NetworkBinary{
					Path: bin,
				})
			}
			p.NetworkPolicies[k] = rule
		}
	}
	return p
}
