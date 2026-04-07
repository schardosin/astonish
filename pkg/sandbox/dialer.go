package sandbox

import (
	"context"
	"net"
	"net/http"
)

// ContainerDialer creates TCP connections to services inside Incus containers
// by tunneling through the Incus exec API (socat STDIO). This works on all
// platforms — Linux native, Docker+Incus on macOS/Windows — because the
// connection goes through the Incus API WebSocket, not the container's bridge IP.
type ContainerDialer struct {
	Client *IncusClient
}

// Dial connects to 127.0.0.1:port inside the named container via exec tunnel.
// The connection tunnels through socat running inside the container.
func (d *ContainerDialer) Dial(containerName string, port int) (net.Conn, error) {
	return DialViaExec(d.Client, containerName, "127.0.0.1", port)
}

// HTTPTransport returns an *http.Transport that routes all connections through
// an exec tunnel to the specified container and port. The transport's DialContext
// ignores the network/address parameters — the destination is fixed to the
// container:port pair.
//
// This is designed for httputil.ReverseProxy where the target URL is a
// placeholder (e.g., http://container/...) and all traffic should go to a
// specific service inside the container.
//
// Each HTTP connection opens a new socat tunnel. For HTTP/1.1 with keep-alive,
// this means one tunnel per persistent connection, which is efficient for
// typical proxy use cases.
func (d *ContainerDialer) HTTPTransport(containerName string, port int) *http.Transport {
	return &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return d.Dial(containerName, port)
		},
		// Disable keep-alive pooling — each DialContext creates a dedicated
		// socat process, so connection reuse across different requests is fine
		// (the transport manages that), but we don't want idle connections
		// holding socat processes alive unnecessarily.
		MaxIdleConnsPerHost: 2,
	}
}
