package api

import (
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/browser"
	"github.com/schardosin/astonish/pkg/sandbox"
)

// BrowserVNCProxyHandler proxies HTTP and WebSocket requests to the KasmVNC
// web client running inside a browser container. This provides visual access
// to the browser for human-in-the-loop tasks like CAPTCHA solving.
//
// Route: /api/browser/vnc/{container}/{path...}
//
// KasmVNC is launched with -DisableBasicAuth so no credentials are needed.
// The Studio reverse proxy is the only path into the container, providing
// its own access control layer.
//
// The KasmVNC web client serves static HTML/JS/CSS on HTTP and uses WebSocket
// for the VNC stream. This handler transparently proxies both.
func BrowserVNCProxyHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	containerName := vars["container"]
	proxyPath := "/" + vars["path"]

	if containerName == "" {
		http.Error(w, "container name is required", http.StatusBadRequest)
		return
	}

	// Connect to sandbox to discover container IP
	client, err := sandboxConnect()
	if err != nil {
		http.Error(w, "failed to connect to sandbox: "+err.Error(), http.StatusInternalServerError)
		return
	}

	ip, err := getCachedIP(client, containerName)
	if err != nil {
		http.Error(w, "failed to resolve container IP: "+err.Error(), http.StatusBadGateway)
		return
	}

	port := sandbox.DefaultKasmVNCPort

	// Strip headers that confuse KasmVNC's auth layer. KasmVNC inspects
	// several headers for authentication decisions (SSO/auth-proxy pattern)
	// and returns 401 even with -DisableBasicAuth when they contain
	// unexpected values:
	//
	// - X-Original-URL / X-Original-Method: injected by upstream reverse
	//   proxies (muxpie). KasmVNC checks X-Original-URL and rejects
	//   requests where the path doesn't match its expectations.
	//
	// - Referer: the browser sets Referer on sub-resource requests to the
	//   proxy URL (e.g. /api/browser/vnc/{container}/...). KasmVNC sees a
	//   Referer containing /api/ — which doesn't match its own root — and
	//   returns 401 on every JS, CSS, and image load.
	//
	// NOTE: Origin is intentionally NOT stripped. KasmVNC requires the
	// Origin header for WebSocket upgrade negotiation on /websockify.
	// Without it, the WebSocket path returns 404 and the VNC connection
	// fails entirely (UI.rfb never initializes → lastActiveAt TypeError).
	r.Header.Del("X-Original-URL")
	r.Header.Del("X-Original-Method")
	r.Header.Del("Referer")

	// WebSocket upgrade — use raw TCP proxy for the VNC stream
	if isWebSocketUpgrade(r) {
		proxyWebSocket(w, r, ip, port, proxyPath)
		return
	}

	// Regular HTTP — reverse proxy to KasmVNC's web interface
	target := &url.URL{
		Scheme: "http",
		Host:   ip + ":" + strconv.Itoa(port),
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = proxyPath
			req.URL.RawQuery = r.URL.RawQuery
			req.Host = target.Host
		},
		ModifyResponse: func(resp *http.Response) error {
			// Strip headers that block iframe embedding. KasmVNC sets
			// Cross-Origin-Embedder-Policy and Cross-Origin-Opener-Policy
			// which prevent the web client from loading inside the Studio
			// iframe. These are safe to remove because the proxy is
			// same-origin with the Studio UI.
			resp.Header.Del("Cross-Origin-Embedder-Policy")
			resp.Header.Del("Cross-Origin-Opener-Policy")
			resp.Header.Del("X-Frame-Options")
			resp.Header.Del("Content-Security-Policy")

			// Fix Content-Type for static assets. KasmVNC's built-in HTTP
			// server serves all files as text/plain (including JS, CSS,
			// and HTML). Browsers with X-Content-Type-Options: nosniff
			// refuse to execute scripts that don't have a JS MIME type.
			// For the root path ("/"), force text/html since KasmVNC
			// serves its web client HTML there.
			if ct := resp.Header.Get("Content-Type"); ct == "text/plain" {
				if proxyPath == "/" {
					resp.Header.Set("Content-Type", "text/html; charset=utf-8")
				} else if mimeType := mime.TypeByExtension(filepath.Ext(proxyPath)); mimeType != "" {
					resp.Header.Set("Content-Type", mimeType)
				}
			}

			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			slog.Error("browser VNC proxy error",
				"container", containerName,
				"error", err,
			)
			rw.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(rw, "Browser VNC proxy error: %s", err)
		},
	}

	proxy.ServeHTTP(w, r)
}

// BrowserVNCInfoHandler returns the KasmVNC connection details for a browser
// container. Used by the Studio UI to determine where to connect.
//
// Route: GET /api/browser/vnc-info/{container}
func BrowserVNCInfoHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	containerName := vars["container"]

	if containerName == "" {
		http.Error(w, "container name is required", http.StatusBadRequest)
		return
	}

	client, err := sandboxConnect()
	if err != nil {
		http.Error(w, "failed to connect to sandbox: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if !client.InstanceExists(containerName) {
		http.Error(w, "container not found", http.StatusNotFound)
		return
	}

	if !client.IsRunning(containerName) {
		http.Error(w, "container is not running", http.StatusConflict)
		return
	}

	ip, err := getCachedIP(client, containerName)
	if err != nil {
		http.Error(w, "failed to resolve container IP: "+err.Error(), http.StatusBadGateway)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"container": containerName,
		"ip":        ip,
		"vnc_port":  sandbox.DefaultKasmVNCPort,
		"proxy_url": fmt.Sprintf("/api/browser/vnc/%s/", containerName),
	})
}

// BrowserHandoffDoneHandler is called when the user clicks "Done" in the
// browser panel. This revokes the VNC handoff token (blocking further proxy
// access) and stops any active CDP handoff server. The browser itself
// continues running — only the visual sharing session ends.
//
// Route: POST /api/browser/handoff-done
func BrowserHandoffDoneHandler(w http.ResponseWriter, r *http.Request) {
	mgr := GetBrowserManager()
	if mgr != nil {
		// Revoke the VNC handoff token for the container (if any).
		// This causes the auth middleware to reject subsequent VNC proxy
		// requests, effectively ending the visual sharing session.
		containerName := mgr.ContainerName()
		if containerName != "" {
			browser.GetHandoffTokenRegistry().Revoke(containerName)
		}

		// Stop any active CDP handoff server (used in host mode).
		mgr.StopHandoff()
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "browser sharing session ended",
	})
}
