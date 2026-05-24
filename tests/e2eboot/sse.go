//go:build e2e

package e2eboot

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// SSEEvent represents a parsed server-sent event.
type SSEEvent struct {
	Type string
	Data string
}

// SSE sends an authenticated POST to an SSE endpoint and returns the parsed
// events. It reads the entire stream until the connection is closed or a
// "done"/"complete" event is received.
func (h *Harness) SSE(t *testing.T, path string, body any, timeout time.Duration) []SSEEvent {
	t.Helper()
	resp := h.doWithHeaders(t, http.MethodPost, path, body, timeout, map[string]string{
		"Accept": "text/event-stream",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("[e2eboot] SSE %s: %d %s", path, resp.StatusCode, string(respBody))
	}

	return ParseSSEStream(t, resp.Body)
}

// SSERaw sends an authenticated POST to an SSE endpoint and returns the raw
// response for streaming consumption. The caller must close resp.Body.
func (h *Harness) SSERaw(t *testing.T, path string, body any, timeout time.Duration) *http.Response {
	t.Helper()
	resp := h.doWithHeaders(t, http.MethodPost, path, body, timeout, map[string]string{
		"Accept": "text/event-stream",
	})
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("[e2eboot] SSE %s: %d %s", path, resp.StatusCode, string(respBody))
	}
	return resp
}

// SSEWithExtraHeaders sends a POST with custom headers (merged with auth).
func (h *Harness) SSEWithExtraHeaders(t *testing.T, path string, body any, timeout time.Duration, headers map[string]string) *http.Response {
	t.Helper()
	resp := h.doWithHeaders(t, http.MethodPost, path, body, timeout, headers)
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("[e2eboot] SSE %s: %d %s", path, resp.StatusCode, string(respBody))
	}
	return resp
}

// ParseSSEStream reads an io.Reader and returns parsed SSE events.
// It stops when the stream ends or a "done" or "complete" event is encountered.
func ParseSSEStream(t *testing.T, r io.Reader) []SSEEvent {
	t.Helper()
	var events []SSEEvent
	scanner := bufio.NewScanner(r)
	// Increase buffer for large payloads
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	var currentType string
	var dataLines []string

	flush := func() {
		if currentType != "" || len(dataLines) > 0 {
			events = append(events, SSEEvent{
				Type: currentType,
				Data: strings.Join(dataLines, "\n"),
			})
			currentType = ""
			dataLines = nil
		}
	}

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line = event boundary
			flush()
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			currentType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		} else if line == "data:" {
			dataLines = append(dataLines, "")
		}
	}
	// Flush any trailing event
	flush()

	return events
}

// FindEvent returns the first event with the given type, or nil.
func FindEvent(events []SSEEvent, eventType string) *SSEEvent {
	for i := range events {
		if events[i].Type == eventType {
			return &events[i]
		}
	}
	return nil
}

// FindAllEvents returns all events with the given type.
func FindAllEvents(events []SSEEvent, eventType string) []SSEEvent {
	var result []SSEEvent
	for i := range events {
		if events[i].Type == eventType {
			result = append(result, events[i])
		}
	}
	return result
}

// DecodeEventData unmarshals an SSE event's data into dest.
func DecodeEventData(t *testing.T, event *SSEEvent, dest any) {
	t.Helper()
	if err := json.Unmarshal([]byte(event.Data), dest); err != nil {
		t.Fatalf("[e2eboot] decode event %q data: %v\nData: %s", event.Type, err, event.Data)
	}
}

// ExtractSessionIDFromSSE scans events for a "session" event containing a sessionId field.
func ExtractSessionIDFromSSE(t *testing.T, events []SSEEvent) string {
	t.Helper()
	for _, ev := range events {
		if ev.Type == "session" {
			var session struct {
				SessionID string `json:"sessionId"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &session); err == nil && session.SessionID != "" {
				return session.SessionID
			}
		}
	}
	return ""
}

// ChatAndWaitForPod sends a chat message, extracts the session ID from SSE,
// derives the pod name, and waits for the pod to be Running.
func (h *Harness) ChatAndWaitForPod(t *testing.T, message string) (sessionID, podName string) {
	t.Helper()

	body := map[string]string{"message": message}
	events := h.SSE(t, "/api/studio/chat", body, 90*time.Second)

	sessionID = ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("[e2eboot] failed to extract sessionId from SSE stream")
	}

	podName = DerivePodName(sessionID)
	t.Logf("[e2eboot] Waiting for pod %s...", podName)
	WaitForPodRunning(t, podName, 120*time.Second)

	// Give overlay composition a moment
	time.Sleep(3 * time.Second)
	return sessionID, podName
}

// CleanupSession deletes a session via the API and removes the pod.
func (h *Harness) CleanupSession(t *testing.T, sessionID, podName string) {
	t.Helper()
	resp := h.Delete(t, "/api/studio/sessions/"+sessionID)
	resp.Body.Close()
	DeletePod(t, podName)
}

func (h *Harness) doWithHeaders(t *testing.T, method, path string, body any, timeout time.Duration, headers map[string]string) *http.Response {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		jsonBody, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(jsonBody))
	}

	url := h.BaseURL + path
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("[e2eboot] create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.Token)
	req.Header.Set("X-Astonish-Team", "general")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("[e2eboot] %s %s: %v", method, path, err)
	}
	return resp
}

// ToolOutcome classifies the result of attempting a particular tool during
// an SSE chat stream. It is used by tests that need to distinguish "the
// LLM never tried this tool" (a legitimate skip condition) from "the LLM
// tried but the sandbox infrastructure failed" (a hard failure that must
// be visible in test output, not silently skipped).
type ToolOutcome int

const (
	// ToolNotCalled — the named tool never appears in any tool_call event.
	// The LLM judged it unnecessary; tests should usually t.Skip in this case.
	ToolNotCalled ToolOutcome = iota
	// ToolSucceeded — at least one tool_result for this tool came back
	// without a recognized infrastructure error string.
	ToolSucceeded
	// ToolInfraFailure — the tool was called one or more times and EVERY
	// tool_result contains a recognizable sandbox/k8s infrastructure
	// failure ("pod does not have a host assigned", "unable to upgrade
	// connection", "create pod", etc.). Tests should t.Fatal in this
	// case so cluster-level failures are visible rather than silently
	// degrading test coverage to a green skip.
	ToolInfraFailure
)

// infraErrorPatterns are substrings in a tool_result that indicate a
// sandbox-infrastructure-level failure (rather than a tool-logic failure
// like "file not found" that the test environment can legitimately
// expect). Matched case-insensitively against tool_result content.
var infraErrorPatterns = []string{
	"does not have a host assigned",
	"unable to upgrade connection",
	"failed to create pod",
	"create pod",
	"sandbox/k8s: exec",
	"insufficient memory",
	"unschedulable",
}

// ClassifyToolOutcome inspects the SSE events for tool_call and tool_result
// events involving the named tool and returns whether the tool was never
// invoked, succeeded at least once, or failed every time with what looks
// like sandbox infrastructure trouble. See ToolOutcome for guidance on how
// callers should react to each outcome.
//
// The classification is conservative: a result is "infra failure" only
// when it matches one of the known infrastructure error patterns. Any
// other failure (e.g. permission denied, malformed args) counts as a
// real tool failure that the test should handle as such, not as infra.
func ClassifyToolOutcome(events []SSEEvent, toolName string) ToolOutcome {
	called := false
	for _, ev := range events {
		if ev.Type != "tool_call" {
			continue
		}
		// tool_call payload contains "name":"<toolName>"; cheap substring
		// check is sufficient — we don't need to fully decode every event.
		if strings.Contains(ev.Data, `"name":"`+toolName+`"`) {
			called = true
			break
		}
	}
	if !called {
		return ToolNotCalled
	}

	// At least one matching tool_call exists. Examine all tool_result
	// events for this tool and decide.
	anySuccess := false
	anyResult := false
	for _, ev := range events {
		if ev.Type != "tool_result" {
			continue
		}
		// tool_result payload usually carries "tool_name" or "name"; match
		// either form.
		if !strings.Contains(ev.Data, toolName) {
			continue
		}
		anyResult = true
		lower := strings.ToLower(ev.Data)
		isInfra := false
		for _, pat := range infraErrorPatterns {
			if strings.Contains(lower, strings.ToLower(pat)) {
				isInfra = true
				break
			}
		}
		if !isInfra {
			anySuccess = true
		}
	}
	if anySuccess {
		return ToolSucceeded
	}
	if anyResult {
		return ToolInfraFailure
	}
	// Tool was called but no tool_result came back at all — treat as
	// infra failure (the SSE stream ended before the result arrived,
	// which itself indicates a backend problem).
	return ToolInfraFailure
}

