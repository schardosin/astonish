package openshell

import (
	"context"
	"io"
	"time"
)

// GatewayClient is the interface the backend uses to communicate with the
// OpenShell Gateway. It abstracts the gRPC transport so the backend can
// be tested without a live gateway.
//
// The real implementation (client_grpc.go) will use OpenShell's proto-generated
// stubs. Tests use a mock.
//
// Reference: docs/architecture/openshell-sandbox-backend.md §7, §8.
type GatewayClient interface {
	// CreateSandbox requests the gateway to create a new sandbox.
	// Returns the gateway-assigned sandbox ID.
	CreateSandbox(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error)

	// DeleteSandbox requests deletion of a sandbox by its ID.
	DeleteSandbox(ctx context.Context, sandboxID string) error

	// GetSandboxStatus retrieves the current status of a sandbox.
	GetSandboxStatus(ctx context.Context, sandboxID string) (*SandboxStatus, error)

	// ExecCommand runs a command in a sandbox and returns the result.
	// For synchronous (non-streaming) execution.
	ExecCommand(ctx context.Context, sandboxID string, req ExecRequest) (*ExecResponse, error)

	// ExecStream starts a bidirectional streaming exec session.
	// Returns a stream that the caller reads/writes. Caller MUST close the stream.
	ExecStream(ctx context.Context, sandboxID string, req ExecRequest) (ExecStreamConn, error)

	// ListSandboxes returns all sandboxes known to the gateway, optionally
	// filtered by a Kubernetes-style label selector (e.g., "purpose=chat").
	// An empty selector returns all sandboxes.
	ListSandboxes(ctx context.Context, labelSelector string) ([]SandboxSummary, error)

	// Close releases any resources held by the client (e.g., gRPC connection).
	Close() error
}

// CreateSandboxRequest contains the parameters for creating a new sandbox.
type CreateSandboxRequest struct {
	// Name is the desired sandbox name (e.g., "astn-sess-<session-id[:8]>").
	Name string

	// Image is the container image for the sandbox pod.
	Image string

	// Env is the environment variables to set in the sandbox container.
	Env map[string]string

	// Labels are the Kubernetes labels applied to the sandbox pod.
	Labels map[string]string

	// Policy is the sandbox security policy (Landlock, filesystem, process).
	// When nil, the gateway applies its default policy.
	Policy *SandboxPolicySpec

	// NodeSelector constrains where the pod is scheduled.
	NodeSelector map[string]string

	// Tolerations for the pod scheduling.
	Tolerations []Toleration
}

// SandboxPolicySpec defines the security policy applied inside the sandbox
// by the OpenShell supervisor. This is a domain-level type mapped to the
// proto SandboxPolicy in client_grpc.go.
type SandboxPolicySpec struct {
	// Version is the policy schema version (currently 1).
	Version uint32

	// Landlock controls Linux Landlock LSM enforcement.
	Landlock *LandlockSpec

	// Filesystem defines read/write access rules.
	Filesystem *FilesystemSpec

	// Process configures user/group identity for sandboxed processes.
	Process *ProcessSpec

	// NetworkPolicies defines named network access rules.
	NetworkPolicies map[string]*NetworkPolicySpec
}

// LandlockSpec controls Landlock enforcement behaviour.
type LandlockSpec struct {
	// Compatibility mode: "best_effort" degrades gracefully when the kernel
	// does not support Landlock; "hard_requirement" crashes if unavailable.
	Compatibility string
}

// FilesystemSpec restricts filesystem access within the sandbox.
type FilesystemSpec struct {
	// IncludeWorkdir automatically adds the workdir as read-write.
	IncludeWorkdir bool
	// ReadOnly paths accessible in read-only mode.
	ReadOnly []string
	// ReadWrite paths accessible in read-write mode.
	ReadWrite []string
}

// ProcessSpec configures the sandboxed process identity.
type ProcessSpec struct {
	RunAsUser  string
	RunAsGroup string
}

// NetworkPolicySpec defines allowed network endpoints for a named policy.
type NetworkPolicySpec struct {
	// Name is a human-readable identifier.
	Name string
	// Endpoints are structured host:port entries the sandbox may connect to.
	Endpoints []EndpointSpec
	// Binaries are path globs for binaries allowed to use these endpoints.
	// "/**" allows any binary.
	Binaries []string
}

// EndpointSpec is a single allowed network endpoint with host and port.
type EndpointSpec struct {
	Host string
	Port uint32 // 0 = defaults to 443 at proto mapping time
}

// Toleration mirrors corev1.Toleration for decoupling from k8s types.
type Toleration struct {
	Key      string
	Operator string
	Value    string
	Effect   string
}

// CreateSandboxResponse is returned after a sandbox is successfully created.
type CreateSandboxResponse struct {
	// SandboxID is the sandbox name (canonical lookup key for Get/Delete).
	SandboxID string

	// GatewayID is the gateway-assigned UUID (required for ExecSandbox).
	GatewayID string

	// PodName is the Kubernetes pod name (set by the K8s driver).
	PodName string
}

// SandboxState represents the lifecycle state of a sandbox.
type SandboxState string

const (
	SandboxStateCreating SandboxState = "creating"
	SandboxStateRunning  SandboxState = "running"
	SandboxStateStopped  SandboxState = "stopped"
	SandboxStateFailed   SandboxState = "failed"
	SandboxStateGone     SandboxState = "gone"
)

// SandboxStatus contains the current state of a sandbox.
type SandboxStatus struct {
	State     SandboxState
	Message   string
	PodName   string
	GatewayID string // gateway-assigned UUID (for ExecSandbox)
}

// ExecRequest contains the parameters for executing a command in a sandbox.
type ExecRequest struct {
	// Command is the command to execute (e.g., ["/usr/local/bin/astonish", "node"]).
	Command []string

	// Env is additional environment variables for the command.
	Env map[string]string

	// WorkDir is the working directory for the command.
	WorkDir string

	// Stdin, when non-nil, is fed to the command's stdin.
	Stdin io.Reader

	// TTY requests a pseudo-terminal allocation.
	TTY bool

	// Cols and Rows set the initial terminal size when TTY is true.
	Cols int
	Rows int

	// SeparateStderr, when non-nil, receives stderr output instead of
	// mixing it into the stdout stream. Critical for machine protocols
	// (MCP JSON-RPC) where stderr contamination breaks parsing.
	SeparateStderr io.Writer
}

// ExecResponse is the result of a synchronous exec.
type ExecResponse struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// ExecStreamConn is a bidirectional streaming connection for interactive
// or long-running exec sessions.
type ExecStreamConn interface {
	// Read reads from the command's stdout (and stderr if not separated).
	io.Reader

	// Write writes to the command's stdin.
	io.Writer

	// Resize sends a terminal resize event (for PTY sessions).
	Resize(cols, rows int) error

	// ExitCode returns the command's exit code after the stream is closed.
	// Returns -1 if the command hasn't exited yet.
	ExitCode() int

	// Close terminates the exec session and releases resources.
	Close() error
}

// SandboxSummary is a lightweight view of a sandbox returned by ListSandboxes.
// Used for GC reconciliation (comparing gateway state against DB session records).
type SandboxSummary struct {
	// ID is the gateway-assigned UUID.
	ID string

	// Name is the sandbox name (Kubernetes pod name, e.g., "astn-sess-abc12345").
	Name string

	// Labels are the Kubernetes labels on the sandbox pod.
	Labels map[string]string

	// CreatedAt is when the sandbox was created.
	CreatedAt time.Time

	// Phase is the sandbox lifecycle phase (e.g., "RUNNING", "CREATING", "FAILED").
	Phase string
}
