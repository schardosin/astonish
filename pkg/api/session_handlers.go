package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/pdfgen"
	"github.com/schardosin/astonish/pkg/sandbox"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"google.golang.org/adk/session"
)

// validSessionID matches UUID-format session IDs (with optional prefix).
// Prevents path traversal and command injection via session ID parameters.
var validSessionID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

// StudioSessionsHandler handles GET /api/studio/sessions.
func StudioSessionsHandler(w http.ResponseWriter, r *http.Request) {
	cm := GetChatManager()
	if err := cm.ensureReady(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fs := cm.fileStore()
	if fs == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]StudioSessionResponse{})
		return
	}

	metas, err := fs.ListSessionMetas(studioChatAppName, studioChatUserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sort by updated_at descending (most recent first)
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].UpdatedAt.After(metas[j].UpdatedAt)
	})

	sessions := make([]StudioSessionResponse, 0, len(metas))
	for _, m := range metas {
		sessions = append(sessions, StudioSessionResponse{
			ID:           m.ID,
			Title:        m.Title,
			CreatedAt:    m.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    m.UpdatedAt.Format(time.RFC3339),
			MessageCount: m.MessageCount,
			FleetKey:     m.FleetKey,
			FleetName:    m.FleetName,
			IssueNumber:  m.IssueNumber,
			Repo:         m.Repo,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

// StudioSessionHandler handles GET /api/studio/sessions/{id}.
func StudioSessionHandler(w http.ResponseWriter, r *http.Request) {
	cm := GetChatManager()
	if err := cm.ensureReady(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sessionID := mux.Vars(r)["id"]
	fs := cm.fileStore()
	if fs == nil {
		http.Error(w, "File store not available", http.StatusInternalServerError)
		return
	}

	// Get session metadata
	meta, err := fs.GetSessionMeta(sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Fleet sessions: read transcript and return fleet-style messages
	if meta.FleetKey != "" {
		transcriptPath := filepath.Join(fs.BaseDir(), studioChatAppName, studioChatUserID, sessionID+".jsonl")
		transcript := persistentsession.NewTranscript(transcriptPath)
		events, readErr := transcript.ReadEvents()

		var fleetMessages []FleetMessageSummary
		if readErr == nil && len(events) > 0 {
			fleetMessages = fleetEventsToMessages(events)
		}

		resp := StudioSessionDetailResponse{
			StudioSessionResponse: StudioSessionResponse{
				ID:           meta.ID,
				Title:        meta.Title,
				CreatedAt:    meta.CreatedAt.Format(time.RFC3339),
				UpdatedAt:    meta.UpdatedAt.Format(time.RFC3339),
				MessageCount: meta.MessageCount,
				FleetKey:     meta.FleetKey,
				FleetName:    meta.FleetName,
				IssueNumber:  meta.IssueNumber,
				Repo:         meta.Repo,
			},
			Messages:      []StudioMessage{},
			FleetMessages: fleetMessages,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Get session transcript
	getResp, err := fs.Get(r.Context(), &session.GetRequest{
		AppName:   studioChatAppName,
		UserID:    studioChatUserID,
		SessionID: sessionID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load session: %v", err), http.StatusInternalServerError)
		return
	}

	// Transform ADK events into simplified messages
	var redactor *credentials.Redactor
	if cm.components != nil && cm.components.ChatAgent != nil {
		redactor = cm.components.ChatAgent.Redactor
	}
	messages := eventsToMessages(getResp.Session.Events(), redactor)

	// Collect file artifacts from write_file/edit_file tool calls
	artifacts := collectArtifacts(getResp.Session.Events())

	resp := StudioSessionDetailResponse{
		StudioSessionResponse: StudioSessionResponse{
			ID:           meta.ID,
			Title:        meta.Title,
			CreatedAt:    meta.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    meta.UpdatedAt.Format(time.RFC3339),
			MessageCount: meta.MessageCount,
		},
		Messages:  messages,
		Artifacts: artifacts,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// StudioDeleteSessionHandler handles DELETE /api/studio/sessions/{id}.
func StudioDeleteSessionHandler(w http.ResponseWriter, r *http.Request) {
	cm := GetChatManager()
	if err := cm.ensureReady(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sessionID := mux.Vars(r)["id"]
	if !validSessionID.MatchString(sessionID) {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
		return
	}

	// If this is an active fleet session, stop it and clean up sandbox
	registry := getFleetSessionRegistry()
	if fs := registry.Get(sessionID); fs != nil {
		fs.Stop()
		fs.Cleanup() // destroy sandbox container + clean session registry
		registry.Unregister(sessionID)
	}

	// Clean up per-session workspace directory if one was recorded.
	// Read metadata before deleting the session (deletion removes metadata).
	if fileStore := getFleetFileStore(); fileStore != nil {
		if meta, metaErr := fileStore.GetSessionMeta(sessionID); metaErr == nil && meta.WorkspaceDir != "" {
			if cleanErr := fleet.CleanupSessionWorkspace(meta.WorkspaceDir); cleanErr != nil {
				slog.Warn("could not clean up workspace", "component", "fleet", "workspace", meta.WorkspaceDir, "error", cleanErr)
			}
		}
	}

	sessionService := cm.components.SessionService

	err := sessionService.Delete(r.Context(), &session.DeleteRequest{
		AppName:   studioChatAppName,
		UserID:    studioChatUserID,
		SessionID: sessionID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete session: %v", err), http.StatusInternalServerError)
		return
	}

	// Best-effort: destroy sandbox container if one exists for this session
	sandbox.TryDestroySessionContainer(sessionID)

	w.WriteHeader(http.StatusNoContent)
}

// StudioStopHandler handles POST /api/studio/sessions/{id}/stop.
// Stops both the background chat runner and any active SSE stream.
func StudioStopHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]
	cm := GetChatManager()
	cm.cancelStream(sessionID)

	// Also stop the background runner if one exists
	registry := getChatRunnerRegistry()
	registry.Stop(sessionID)

	w.WriteHeader(http.StatusNoContent)
}

// StudioArtifactDownloadHandler serves file artifacts for download.
// Uses the same three-tier fallback strategy as StudioArtifactContentHandler
// so downloads work even when the original file no longer exists on the host.
//
// GET /api/studio/artifacts?path=<absolute_path>&session=<sessionID>
func StudioArtifactDownloadHandler(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "missing 'path' query parameter", http.StatusBadRequest)
		return
	}

	sessionID := r.URL.Query().Get("session")
	cleanPath := filepath.Clean(filePath)
	fileName := filepath.Base(cleanPath)

	// Tier 1: Try serving directly from host filesystem
	if _, err := os.Stat(cleanPath); err == nil {
		http.ServeFile(w, r, cleanPath)
		return
	}

	// Tier 2: Try reading from sandbox container
	if sessionID != "" {
		if content, ok := readFromSandboxContainer(sessionID, cleanPath); ok {
			serveArtifactDownload(w, fileName, content)
			return
		}
	}

	// Tier 3: Fall back to reading from persisted session JSONL
	if sessionID != "" {
		cm := GetChatManager()
		if fs := cm.fileStore(); fs != nil {
			if content, ok := readArtifactContentFromSession(fs, sessionID, cleanPath); ok {
				serveArtifactDownload(w, fileName, []byte(content))
				return
			}
		}
	}

	http.Error(w, "file not found", http.StatusNotFound)
}

// serveArtifactDownload writes content as a downloadable file response with
// appropriate Content-Type and Content-Disposition headers.
func serveArtifactDownload(w http.ResponseWriter, fileName string, content []byte) {
	// Detect content type from extension
	ct := mime.TypeByExtension(filepath.Ext(fileName))
	if ct == "" {
		ct = http.DetectContentType(content)
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	w.Write(content)
}

// StudioArtifactContentHandler returns file content as plain text for the
// in-browser file viewer. Uses a three-tier fallback strategy:
//  1. Read from host filesystem (works for non-sandbox sessions where file still exists)
//  2. Read from sandbox container via Incus PullFile (works when container is running)
//  3. Read from persisted session JSONL (works always — extracts content from write_file args)
//
// GET /api/studio/artifacts/content?path=<path>&session=<sessionID>
func StudioArtifactContentHandler(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "missing 'path' query parameter", http.StatusBadRequest)
		return
	}

	sessionID := r.URL.Query().Get("session")
	cleanPath := filepath.Clean(filePath)

	// Tier 1: Try reading from host filesystem
	if content, err := os.ReadFile(cleanPath); err == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(content)
		return
	}

	// Tier 2: Try reading from sandbox container (if sandbox is enabled and session is provided)
	if sessionID != "" {
		if content, ok := readFromSandboxContainer(sessionID, cleanPath); ok {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write(content)
			return
		}
	}

	// Tier 3: Fall back to reading content from persisted session JSONL
	if sessionID != "" {
		cm := GetChatManager()
		if fs := cm.fileStore(); fs != nil {
			if content, ok := readArtifactContentFromSession(fs, sessionID, cleanPath); ok {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				fmt.Fprint(w, content)
				return
			}
		}
	}

	http.Error(w, "file not found", http.StatusNotFound)
}

// readFromSandboxContainer attempts to read a file from a sandbox container
// using the Incus PullFile API. Returns the content and true on success.
func readFromSandboxContainer(sessionID, filePath string) ([]byte, bool) {
	// Check if sandbox is enabled
	appCfg, err := config.LoadAppConfig()
	if err != nil || appCfg == nil || !sandbox.IsSandboxEnabled(&appCfg.Sandbox) {
		return nil, false
	}

	// Look up the container name for this session
	registry, err := sandbox.NewSessionRegistry()
	if err != nil {
		slog.Debug("failed to load sandbox session registry", "error", err)
		return nil, false
	}
	entry := registry.Get(sessionID)
	if entry == nil || entry.ContainerName == "" {
		return nil, false
	}

	// Connect to Incus and pull the file
	client, err := sandboxConnect()
	if err != nil {
		slog.Debug("failed to connect to sandbox for artifact read", "error", err)
		return nil, false
	}

	reader, _, err := client.PullFile(entry.ContainerName, filePath)
	if err != nil {
		slog.Debug("failed to pull file from sandbox container",
			"container", entry.ContainerName, "path", filePath, "error", err)
		return nil, false
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		slog.Debug("failed to read file content from sandbox container", "error", err)
		return nil, false
	}

	return content, true
}

// StudioArtifactPDFHandler reads a markdown artifact and returns it as a PDF.
// Uses the same three-tier fallback as StudioArtifactDownloadHandler to locate
// the source markdown, then converts it to PDF using the pdfgen package.
//
// GET /api/studio/artifacts/pdf?path=<path>&session=<sessionID>
func StudioArtifactPDFHandler(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "missing 'path' query parameter", http.StatusBadRequest)
		return
	}

	sessionID := r.URL.Query().Get("session")
	cleanPath := filepath.Clean(filePath)

	// Read the source markdown content using the same 3-tier fallback.
	var mdContent []byte

	// Tier 1: Try reading from host filesystem
	if content, err := os.ReadFile(cleanPath); err == nil {
		mdContent = content
	}

	// Tier 2: Try reading from sandbox container
	if mdContent == nil && sessionID != "" {
		if content, ok := readFromSandboxContainer(sessionID, cleanPath); ok {
			mdContent = content
		}
	}

	// Tier 3: Fall back to reading from persisted session JSONL
	if mdContent == nil && sessionID != "" {
		cm := GetChatManager()
		if fs := cm.fileStore(); fs != nil {
			if content, ok := readArtifactContentFromSession(fs, sessionID, cleanPath); ok {
				mdContent = []byte(content)
			}
		}
	}

	if mdContent == nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Convert markdown to PDF using headless Chrome inside the session container.
	slog.Info("PDF: starting Chrome PDF generation", "path", cleanPath, "sessionID", sessionID)

	// Use a timeout so the request doesn't hang indefinitely if Chrome is stuck.
	type pdfResult struct {
		data []byte
		err  error
	}
	ch := make(chan pdfResult, 1)
	go func() {
		slog.Info("PDF: getting browser manager", "sessionID", sessionID)
		browserMgr := GetPDFBrowserManager(sessionID)
		slog.Info("PDF: browser manager obtained, calling ConvertMarkdownToPDFChrome")
		data, err := pdfgen.ConvertMarkdownToPDFChrome(mdContent, browserMgr)
		slog.Info("PDF: ConvertMarkdownToPDFChrome returned", "err", err, "size", len(data))
		ch <- pdfResult{data, err}
	}()

	var pdfData []byte
	select {
	case result := <-ch:
		if result.err != nil {
			slog.Error("PDF generation failed", "path", cleanPath, "error", result.err)
			http.Error(w, fmt.Sprintf("PDF generation failed: %v", result.err), http.StatusInternalServerError)
			return
		}
		pdfData = result.data
	case <-time.After(30 * time.Second):
		slog.Error("PDF generation timed out after 30s", "path", cleanPath, "sessionID", sessionID)
		http.Error(w, "PDF generation timed out", http.StatusGatewayTimeout)
		return
	}
	slog.Info("PDF generated via Chrome", "path", cleanPath, "size", len(pdfData))

	// Serve as downloadable PDF
	baseName := filepath.Base(cleanPath)
	ext := filepath.Ext(baseName)
	pdfName := baseName[:len(baseName)-len(ext)] + ".pdf"

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", pdfName))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(pdfData)))
	w.Write(pdfData)
}
