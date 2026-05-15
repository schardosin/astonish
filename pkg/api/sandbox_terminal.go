package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// terminalUpgrader upgrades HTTP connections to WebSocket for the terminal.
var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // Same-origin enforced by auth middleware
	},
}

// terminalResizeMsg is a JSON message from the client requesting a terminal resize.
type terminalResizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// SandboxTerminalHandler handles GET /api/sandbox/terminal (WebSocket upgrade).
// It opens a PTY session inside a running team template editor session and
// bridges the WebSocket to the session's stdin/stdout.
//
// The implementation is backend-agnostic: it delegates to sandbox.Backend's
// ExecInteractive method, which handles both Incus containers and K8s pods.
//
// The session ID is derived server-side from the team slug — never from
// user-supplied query parameters — to prevent accessing arbitrary sessions.
//
// Protocol:
//   - Binary messages from client → session stdin (keystrokes)
//   - Binary messages from server → session stdout (terminal output)
//   - Text messages from client: JSON control messages (e.g., {"type":"resize","cols":120,"rows":40})
func SandboxTerminalHandler(w http.ResponseWriter, r *http.Request) {
	// Require team admin
	if !RequireTeamAdmin(w, r) {
		return
	}

	// Derive session ID from team slug
	tc := pgstore.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	// Build backend
	backend, cleanup, err := sandboxBackendForRequest(r)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, "Sandbox unavailable: "+err.Error())
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Verify the editor session exists and is running
	sessionID := teamTemplateSessionID(tc.TeamSlug)
	state, _ := backend.SessionState(r.Context(), sessionID)
	if state == sandbox.SessionStateGone {
		respondError(w, http.StatusNotFound, "Team template editor does not exist")
		return
	}
	if state != sandbox.SessionStateRunning {
		respondError(w, http.StatusConflict, "Team template editor is not running")
		return
	}

	// Upgrade to WebSocket
	ws, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("terminal WebSocket upgrade failed", "component", "sandbox-terminal", "error", err)
		return
	}
	defer ws.Close()

	// Start interactive shell via the Backend interface
	stream, err := teamTemplateExecInteractive(r.Context(), backend, tc.TeamSlug, sandbox.PTYSpec{
		Command: []string{"bash", "-l"},
		Rows:    24,
		Cols:    80,
	})
	if err != nil {
		slog.Error("terminal exec failed", "component", "sandbox-terminal", "session", sessionID, "error", err)
		ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "Failed to start shell"))
		return
	}
	defer stream.Close()

	var wg sync.WaitGroup

	// Goroutine: session stdout → WebSocket (binary frames)
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stream.Read(buf)
			if n > 0 {
				if writeErr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					slog.Debug("terminal ws write error", "component", "sandbox-terminal", "error", writeErr)
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					slog.Debug("terminal stdout read error", "component", "sandbox-terminal", "error", err)
				}
				// Send close frame to client
				ws.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Shell exited"))
				return
			}
		}
	}()

	// Main loop: WebSocket → session stdin + control messages
	for {
		msgType, msg, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				slog.Debug("terminal ws read error", "component", "sandbox-terminal", "error", err)
			}
			break
		}

		switch msgType {
		case websocket.BinaryMessage:
			// Raw input → session stdin
			if _, err := stream.Write(msg); err != nil {
				slog.Debug("terminal stdin write error", "component", "sandbox-terminal", "error", err)
				goto done
			}

		case websocket.TextMessage:
			// Control message (resize)
			var ctrl terminalResizeMsg
			if err := json.Unmarshal(msg, &ctrl); err != nil {
				slog.Debug("terminal invalid control msg", "component", "sandbox-terminal", "raw", string(msg))
				continue
			}
			if ctrl.Type == "resize" && ctrl.Cols > 0 && ctrl.Rows > 0 {
				if err := stream.Resize(ctrl.Rows, ctrl.Cols); err != nil {
					slog.Debug("terminal resize error", "component", "sandbox-terminal", "error", err)
				}
			}
		}
	}

done:
	// Close the stream to signal the process to exit
	stream.Close()

	// Wait for stdout goroutine to finish
	wg.Wait()
}
