package api

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/schardosin/astonish/pkg/sandbox"
)

// headTagRe matches <head> or <head ...> (case-insensitive).
var headTagRe = regexp.MustCompile(`(?i)(<head[^>]*>)`)

// ipCacheEntry holds a cached container IP with an expiry time.
type ipCacheEntry struct {
	ip     string
	expiry time.Time
}

// ipCache maps container names to their bridge IPs with a TTL.
var ipCache sync.Map

const ipCacheTTL = 30 * time.Second

// getCachedIP returns the cached IP for a container, or resolves and caches it.
func getCachedIP(client *sandbox.IncusClient, containerName string) (string, error) {
	if entry, ok := ipCache.Load(containerName); ok {
		cached := entry.(*ipCacheEntry)
		if time.Now().Before(cached.expiry) {
			return cached.ip, nil
		}
		ipCache.Delete(containerName)
	}

	// Use a single-attempt IP resolution for the proxy path.
	// GetContainerIPv4 polls for up to 10s, but if the container is running
	// and healthy, the IP should be available immediately.
	ip, err := client.GetContainerIPv4(containerName)
	if err != nil {
		return "", err
	}

	ipCache.Store(containerName, &ipCacheEntry{
		ip:     ip,
		expiry: time.Now().Add(ipCacheTTL),
	})

	return ip, nil
}

// InvalidateIPCache removes a container's IP from the cache.
// Called when a container is destroyed.
func InvalidateIPCache(containerName string) {
	ipCache.Delete(containerName)
}

// SandboxProxyHandler handles /api/sandbox/proxy/{container}/{port}/{path...}
// It reverse-proxies HTTP (and WebSocket) requests to a service running inside
// a sandbox container. The port must be explicitly exposed via the session
// registry before the proxy will forward traffic.
func SandboxProxyHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	containerName := vars["container"]
	portStr := vars["port"]

	if containerName == "" || portStr == "" {
		http.Error(w, `{"error":"missing container or port"}`, http.StatusBadRequest)
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		http.Error(w, `{"error":"invalid port number"}`, http.StatusBadRequest)
		return
	}

	// Check that the port is explicitly exposed
	sessRegistry, err := sandbox.NewSessionRegistry()
	if err != nil {
		http.Error(w, `{"error":"failed to load session registry"}`, http.StatusInternalServerError)
		return
	}

	if !sessRegistry.IsPortExposed(containerName, port) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, `{"error":"port %d is not exposed on container %q. Use 'astonish sandbox expose %s %d' first."}`,
			port, containerName, containerName, port)
		return
	}

	// Verify container exists and is running
	client, err := sandboxConnect()
	if err != nil {
		http.Error(w, `{"error":"sandbox unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	if !client.IsRunning(containerName) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"container %q is not running"}`, containerName)
		return
	}

	// Resolve container IP
	ip, err := getCachedIP(client, containerName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"cannot resolve container IP: %s"}`, err.Error())
		return
	}

	targetBase := fmt.Sprintf("http://%s:%d", ip, port)

	// Extract the downstream path (everything after /api/sandbox/proxy/{container}/{port})
	prefix := fmt.Sprintf("/api/sandbox/proxy/%s/%s", containerName, portStr)
	downstreamPath := strings.TrimPrefix(r.URL.Path, prefix)
	if downstreamPath == "" {
		downstreamPath = "/"
	}

	// WebSocket upgrade
	if isWebSocketUpgrade(r) {
		proxyWebSocket(w, r, ip, port, downstreamPath)
		return
	}

	// HTTP reverse proxy
	target, _ := url.Parse(targetBase)
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = downstreamPath
			req.URL.RawQuery = r.URL.RawQuery
			req.Host = target.Host

			// Forward headers
			if clientIP, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				if existing := req.Header.Get("X-Forwarded-For"); existing != "" {
					req.Header.Set("X-Forwarded-For", existing+", "+clientIP)
				} else {
					req.Header.Set("X-Forwarded-For", clientIP)
				}
			}
			req.Header.Set("X-Forwarded-Host", r.Host)
			req.Header.Set("X-Forwarded-Proto", "http")
		},
		ModifyResponse: makeBaseTagInjector(prefix + "/"),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, `{"error":"proxy error: %s"}`, err.Error())
		},
	}

	proxy.ServeHTTP(w, r)
}

// injectBaseTag inserts a <base href="..."> tag immediately after the <head>
// tag in an HTML document. This ensures the browser resolves all relative and
// absolute asset paths against the proxy prefix, so SPAs served through the
// reverse proxy load their JS/CSS/images correctly.
//
// If no <head> tag is found, the body is returned unmodified.
func injectBaseTag(body []byte, baseHref string) []byte {
	tag := fmt.Sprintf(`<base href="%s">`, baseHref)
	loc := headTagRe.FindIndex(body)
	if loc == nil {
		return body
	}
	// Insert right after the <head...> match
	insertPos := loc[1]
	result := make([]byte, 0, len(body)+len(tag))
	result = append(result, body[:insertPos]...)
	result = append(result, []byte(tag)...)
	result = append(result, body[insertPos:]...)
	return result
}

// makeBaseTagInjector returns a ModifyResponse function that injects a
// <base href> tag into HTML responses. Non-HTML responses pass through
// untouched. Gzipped HTML responses are decompressed, modified, and served
// uncompressed (HTML pages are small enough that this has no noticeable
// impact on performance).
func makeBaseTagInjector(baseHref string) func(*http.Response) error {
	return func(resp *http.Response) error {
		ct := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			return nil
		}

		// Read the response body
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}

		// Handle gzip encoding
		if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
			gz, err := gzip.NewReader(bytes.NewReader(body))
			if err != nil {
				// Not valid gzip — use body as-is
				goto inject
			}
			decompressed, err := io.ReadAll(gz)
			gz.Close()
			if err != nil {
				goto inject
			}
			body = decompressed
			resp.Header.Del("Content-Encoding")
		}

	inject:
		modified := injectBaseTag(body, baseHref)

		resp.Body = io.NopCloser(bytes.NewReader(modified))
		resp.ContentLength = int64(len(modified))
		resp.Header.Set("Content-Length", strconv.Itoa(len(modified)))
		return nil
	}
}

// isWebSocketUpgrade checks if the request is a WebSocket upgrade request.
func isWebSocketUpgrade(r *http.Request) bool {
	for _, v := range r.Header["Connection"] {
		for _, s := range strings.Split(v, ",") {
			if strings.TrimSpace(strings.ToLower(s)) == "upgrade" {
				if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
					return true
				}
			}
		}
	}
	return false
}

// proxyWebSocket forwards a WebSocket connection to the container.
func proxyWebSocket(w http.ResponseWriter, r *http.Request, ip string, port int, path string) {
	// Dial the backend container
	backendURL := fmt.Sprintf("ws://%s:%d%s", ip, port, path)
	if r.URL.RawQuery != "" {
		backendURL += "?" + r.URL.RawQuery
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	// Forward relevant headers to backend
	reqHeader := http.Header{}
	for _, key := range []string{"Origin", "Sec-WebSocket-Protocol", "Sec-WebSocket-Extensions"} {
		if v := r.Header.Get(key); v != "" {
			reqHeader.Set(key, v)
		}
	}

	backendConn, resp, err := dialer.Dial(backendURL, reqHeader)
	if err != nil {
		if resp != nil {
			http.Error(w, fmt.Sprintf("WebSocket dial failed: %s (status %d)", err, resp.StatusCode), http.StatusBadGateway)
		} else {
			http.Error(w, fmt.Sprintf("WebSocket dial failed: %s", err), http.StatusBadGateway)
		}
		return
	}
	defer backendConn.Close()

	// Upgrade the client connection
	upgrader := websocket.Upgrader{
		CheckOrigin:  func(r *http.Request) bool { return true },
		Subprotocols: websocket.Subprotocols(r),
	}

	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[sandbox-proxy] WebSocket upgrade failed: %v", err)
		return
	}
	defer clientConn.Close()

	// Bidirectional forwarding
	done := make(chan struct{})

	go func() {
		defer close(done)
		forwardWS(clientConn, backendConn)
	}()

	forwardWS(backendConn, clientConn)
	<-done
}

// forwardWS copies WebSocket messages from src to dst until an error occurs.
func forwardWS(src, dst *websocket.Conn) {
	for {
		msgType, msg, err := src.ReadMessage()
		if err != nil {
			// Send close message to the other side
			closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
			_ = dst.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))
			return
		}
		if err := dst.WriteMessage(msgType, msg); err != nil {
			return
		}
	}
}

// forwardRaw copies data bidirectionally between two io.ReadWriteClosers.
func forwardRaw(a, b io.ReadWriteCloser) {
	done := make(chan struct{})
	go func() {
		io.Copy(b, a)
		close(done)
	}()
	io.Copy(a, b)
	<-done
}
