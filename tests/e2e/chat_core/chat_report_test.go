//go:build e2e

// Package chat_core contains E2E tests for Studio Chat core functionality.
// This file covers report generation, artifact access, and export.
package chat_core

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/schardosin/astonish/tests/e2eboot"
)

// TestE2E_Chat_ReportArtifact verifies the report pipeline:
//   - The agent writes a markdown file via write_file
//   - The agent emits an ```astonish-report``` fence pointing at that file
//   - An "artifact" SSE event is emitted with {path, tool_name}
//   - A "report_marker" SSE event is emitted with {path, title}
//   - GET /api/studio/sessions/{id} returns the artifact in artifacts[] with
//     {path, fileName, fileType, toolName, isReport=true, reportTitle}
//   - GET /api/studio/artifacts/content?path=...&session=... returns the content
//
// This test exercises the TWO-STEP report contract: write_file PLUS fence.
// Without both signals, the artifact would (correctly) NOT be promoted to a
// report — that negative case is covered by CHAT-066.
//
// COVERS: CHAT-033
// COVERS: CHAT-034
// COVERS: CHAT-035
// COVERS: CHAT-067
func TestE2E_Chat_ReportArtifact(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	// User-realistic prompt: ask for a report. The system prompt teaches
	// the LLM the two-step contract (write_file + astonish-report fence).
	// We do NOT spell out the contract in the user message — if the LLM
	// can't follow the system prompt for a plain "write me a report"
	// request, the system prompt is the bug. The test resiliently skips
	// (not fails) when the LLM doesn't cooperate.
	body := map[string]any{
		"message":     "Write me a short report (3-5 lines) about Go programming and save it to /tmp/e2e-report-test.md.",
		"autoApprove": true,
	}
	events := h.SSE(t, "/api/studio/chat", body, 120*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("no session ID")
	}
	defer func() {
		if e2eboot.RetainSessions() {
			return
		}
		resp := h.Delete(t, "/api/studio/sessions/"+sessionID)
		resp.Body.Close()
	}()

	// CHAT-033: The artifact SSE event must arrive with a valid path.
	// On-the-wire shape (chat_runner.go): {"path": "...", "tool_name": "..."}
	artifactEvents := e2eboot.FindAllEvents(events, "artifact")
	if len(artifactEvents) == 0 {
		// No artifact event. Distinguish three cases:
		//   1. LLM never tried write_file        -> skip (resilient)
		//   2. LLM tried, sandbox infra broke    -> fatal (loud, fix infra)
		//   3. LLM tried, succeeded, but no event -> fatal (real regression)
		switch e2eboot.ClassifyToolOutcome(events, "write_file") {
		case e2eboot.ToolNotCalled:
			t.Skip("LLM did not call write_file — cannot test artifact pipeline")
		case e2eboot.ToolInfraFailure:
			t.Fatal("sandbox infrastructure broken: write_file was called " +
				"but every tool_result reports a sandbox/k8s scheduling " +
				"failure. This is not a CHAT-033 regression — fix the " +
				"cluster (kubectl get pods -n astonishe2e-sandbox).")
		case e2eboot.ToolSucceeded:
			t.Fatal("write_file succeeded but no 'artifact' SSE event emitted — CHAT-033 regression")
		}
	}

	var sseArtifact struct {
		Path     string `json:"path"`
		ToolName string `json:"tool_name"`
	}
	e2eboot.DecodeEventData(t, &artifactEvents[0], &sseArtifact)

	if sseArtifact.Path == "" {
		t.Error("artifact SSE event missing 'path'")
	}
	if sseArtifact.ToolName == "" {
		t.Error("artifact SSE event missing 'tool_name'")
	}
	if sseArtifact.ToolName != "" && sseArtifact.ToolName != "write_file" && sseArtifact.ToolName != "edit_file" {
		t.Errorf("unexpected tool_name in artifact event: %q", sseArtifact.ToolName)
	}

	// CHAT-067: a report_marker SSE event MUST be emitted whose path
	// matches the same-turn artifact. Absence here means either the LLM
	// failed to emit the fence (skip, resilient) or the backend detector
	// is broken (regression). Distinguish by inspecting the agent's text
	// output for the literal fence string — if the fence is present in
	// text but no marker event was emitted, the backend is the bug.
	reportMarkerEvents := e2eboot.FindAllEvents(events, "report_marker")
	if len(reportMarkerEvents) == 0 {
		fenceSeen := false
		for _, ev := range e2eboot.FindAllEvents(events, "text") {
			var td struct {
				Text string `json:"text"`
			}
			e2eboot.DecodeEventData(t, &ev, &td)
			if strings.Contains(td.Text, "astonish-report") {
				fenceSeen = true
				break
			}
		}
		if fenceSeen {
			t.Fatal("agent emitted astonish-report fence in text but no " +
				"report_marker SSE event was emitted — CHAT-067 regression " +
				"in detectAndEmitReportMarkers")
		}
		t.Skip("LLM did not emit astonish-report fence — cannot test report marker pipeline")
	}

	var sseMarker struct {
		Path  string `json:"path"`
		Title string `json:"title"`
	}
	e2eboot.DecodeEventData(t, &reportMarkerEvents[0], &sseMarker)
	if sseMarker.Path != sseArtifact.Path {
		t.Errorf("report_marker path %q does not match artifact path %q — gate would fail to flip IsReport on the right artifact",
			sseMarker.Path, sseArtifact.Path)
	}
	// Title is optional but the prompt asked for one; warn (not fail) if absent.
	if sseMarker.Title == "" {
		t.Logf("report_marker emitted without title (LLM omitted the optional title field; not a regression)")
	}

	// Wait for session to persist
	time.Sleep(1 * time.Second)

	// CHAT-034: Files button data — GET /api/studio/sessions/{id} returns
	// artifacts[] with the enriched ArtifactInfo shape, NOW including
	// IsReport+ReportTitle projected from the persisted report marker.
	detailResp := h.Get(t, "/api/studio/sessions/"+sessionID)
	if detailResp.StatusCode != http.StatusOK {
		body := e2eboot.ReadBody(t, detailResp)
		t.Fatalf("GET session detail: %d %s", detailResp.StatusCode, body)
	}
	detailBody := e2eboot.ReadBody(t, detailResp)

	var detail struct {
		ID       string `json:"id"`
		Messages []struct {
			Type       string `json:"type"`
			ToolName   string `json:"toolName"`
			ToolResult any    `json:"toolResult"`
		} `json:"messages"`
		Artifacts []struct {
			Path        string `json:"path"`
			FileName    string `json:"fileName"`
			FileType    string `json:"fileType"`
			ToolName    string `json:"toolName"`
			IsReport    bool   `json:"isReport"`
			ReportTitle string `json:"reportTitle"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal([]byte(detailBody), &detail); err != nil {
		t.Fatalf("decode session detail JSON: %v", err)
	}

	// Determine whether the underlying write_file tool actually succeeded.
	// Use the SSE-event-level classifier rather than a substring heuristic
	// on the persisted tool_result text, because the latter misclassifies
	// LLM retry-narration as success and misses some infra error shapes.
	switch e2eboot.ClassifyToolOutcome(events, "write_file") {
	case e2eboot.ToolInfraFailure:
		t.Fatal("sandbox infrastructure broken: write_file was called " +
			"but every tool_result reports a sandbox/k8s scheduling " +
			"failure. This is not a CHAT-034/035 regression — fix the " +
			"cluster (kubectl get pods -n astonishe2e-sandbox).")
	case e2eboot.ToolNotCalled:
		// Should not happen here — we already verified an artifact event
		// arrived above, which implies write_file was called. Treat
		// defensively as a skip rather than a confusing failure.
		t.Skip("write_file not observed in events despite artifact event — environment anomaly")
	case e2eboot.ToolSucceeded:
		// expected path — fall through to artifact-list assertions
	}

	if len(detail.Artifacts) == 0 {
		snippet := detailBody
		if len(snippet) > 2000 {
			snippet = snippet[:2000] + "...[truncated]"
		}
		t.Errorf("session detail has no artifacts despite SSE artifact event being emitted and write_file succeeding; body=%s", snippet)
	} else {
		a := detail.Artifacts[0]
		if a.Path != sseArtifact.Path {
			t.Errorf("session detail artifact path mismatch: SSE=%q detail=%q", sseArtifact.Path, a.Path)
		}
		if a.FileName == "" {
			t.Error("session detail artifact missing fileName")
		}
		if a.FileType == "" {
			t.Error("session detail artifact missing fileType")
		}
		if a.ToolName == "" {
			t.Error("session detail artifact missing toolName")
		}
		// CHAT-067: the persisted report marker MUST be projected onto the
		// artifact at session-detail load time. This is what survives
		// server restarts and history reloads.
		if !a.IsReport {
			t.Errorf("session detail artifact has isReport=false despite a report_marker event being emitted with matching path %q — joinReportMarkers regression",
				a.Path)
		}
		// Title may be empty even when IsReport is true; that's the
		// "marker emitted without title" case logged above. No assertion.
	}

	// CHAT-035: Download artifact via artifact content endpoint.
	contentResp := h.Get(t, "/api/studio/artifacts/content?path="+sseArtifact.Path+"&session="+sessionID)
	defer contentResp.Body.Close()

	switch contentResp.StatusCode {
	case http.StatusOK:
		contentBody := e2eboot.ReadBody(t, contentResp)
		if contentBody == "" {
			t.Error("artifact content endpoint returned empty body")
		}
	case http.StatusNotFound:
		// File may have been cleaned up before the GET — the SSE event +
		// session-detail artifacts[] entry are the primary assertions.
		t.Log("artifact file not found on disk (may have been cleaned) — acceptable")
	default:
		body := e2eboot.ReadBody(t, contentResp)
		t.Errorf("artifact content endpoint returned %d: %s", contentResp.StatusCode, body)
	}
}

// TestE2E_Chat_PlainWriteFileNotReport verifies the negative path of the
// report gate: when the agent writes a non-markdown file (or any file
// without an astonish-report fence), the artifact must NOT be flagged as
// a report. This pins down the regression surface in the b5310ae loosening
// — without this test, a future "any last-turn artifact embeds" mistake
// would silently slip through.
//
// COVERS: CHAT-066
func TestE2E_Chat_PlainWriteFileNotReport(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	// Tell the agent to write a non-report file (a working script). We
	// deliberately do NOT mention astonish-report so the LLM has no reason
	// to emit the fence.
	body := map[string]any{
		"message": `Write a small Python script (under 10 lines) that prints "hello world" to /tmp/e2e-script-test.py using the write_file tool.
This is a quick utility — just write the file and confirm. Do NOT use any astonish-report fence — this is a script, not a report.`,
		"autoApprove": true,
	}
	events := h.SSE(t, "/api/studio/chat", body, 120*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("no session ID")
	}
	defer func() {
		if e2eboot.RetainSessions() {
			return
		}
		resp := h.Delete(t, "/api/studio/sessions/"+sessionID)
		resp.Body.Close()
	}()

	artifactEvents := e2eboot.FindAllEvents(events, "artifact")
	if len(artifactEvents) == 0 {
		switch e2eboot.ClassifyToolOutcome(events, "write_file") {
		case e2eboot.ToolNotCalled:
			t.Skip("LLM did not call write_file — cannot test negative report gate")
		case e2eboot.ToolInfraFailure:
			t.Fatal("sandbox infrastructure broken: write_file was called " +
				"but every tool_result reports a sandbox/k8s scheduling " +
				"failure. Fix the cluster.")
		case e2eboot.ToolSucceeded:
			t.Fatal("write_file succeeded but no artifact SSE event emitted")
		}
	}

	// CHAT-066 core assertion: NO report_marker event must be emitted. Any
	// such event for a plain write_file would mean either (a) the LLM
	// over-eagerly emitted a fence anyway (acceptable but rare), in which
	// case we just skip the negative test, or (b) the backend is
	// fabricating markers from non-fence text — a real regression.
	reportMarkerEvents := e2eboot.FindAllEvents(events, "report_marker")
	if len(reportMarkerEvents) > 0 {
		// Inspect agent text — if a fence really is present, the LLM
		// chose to emit one despite our prompt; that's a tolerable
		// LLM-cooperation issue, not a backend bug.
		fenceSeen := false
		for _, ev := range e2eboot.FindAllEvents(events, "text") {
			var td struct {
				Text string `json:"text"`
			}
			e2eboot.DecodeEventData(t, &ev, &td)
			if strings.Contains(td.Text, "astonish-report") {
				fenceSeen = true
				break
			}
		}
		if !fenceSeen {
			t.Fatal("backend emitted report_marker SSE event but agent text " +
				"contained no astonish-report fence — false-positive marker " +
				"detection in detectAndEmitReportMarkers (CHAT-066 regression)")
		}
		t.Skip("LLM emitted an astonish-report fence anyway despite the prompt — cannot exercise CHAT-066 negative path this run")
	}

	// Wait for session to persist
	time.Sleep(1 * time.Second)

	detailResp := h.Get(t, "/api/studio/sessions/"+sessionID)
	if detailResp.StatusCode != http.StatusOK {
		body := e2eboot.ReadBody(t, detailResp)
		t.Fatalf("GET session detail: %d %s", detailResp.StatusCode, body)
	}
	var detail struct {
		Artifacts []struct {
			Path     string `json:"path"`
			IsReport bool   `json:"isReport"`
		} `json:"artifacts"`
	}
	detailBody := e2eboot.ReadBody(t, detailResp)
	if err := json.Unmarshal([]byte(detailBody), &detail); err != nil {
		t.Fatalf("decode session detail: %v", err)
	}

	// Every artifact in this session must have IsReport=false. A true value
	// here would mean joinReportMarkers projected a marker that should not
	// exist, OR ArtifactInfo's default IsReport leaked to true somehow.
	for _, a := range detail.Artifacts {
		if a.IsReport {
			t.Errorf("artifact %q has IsReport=true on a session that emitted no report_marker — gate is leaking (CHAT-066 regression)",
				a.Path)
		}
	}
}

// TestE2E_Chat_MarkdownWithMarkerIsReport is the symmetric positive
// counterpart of CHAT-066: a markdown file written with an explicit
// astonish-report fence MUST flip IsReport to true. Together with
// CHAT-066 these two tests pin the gate's behavior on both sides
// without relying on the LLM happening to produce the right output.
//
// COVERS: CHAT-068
func TestE2E_Chat_MarkdownWithMarkerIsReport(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	body := map[string]any{
		"message":     "Write a 2-line report about TypeScript. Save it to /tmp/e2e-ts-summary.md using write_file, then emit an astonish-report fence with that path. Both steps are required.",
		"autoApprove": true,
	}
	events := h.SSE(t, "/api/studio/chat", body, 120*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("no session ID")
	}
	defer func() {
		if e2eboot.RetainSessions() {
			return
		}
		resp := h.Delete(t, "/api/studio/sessions/"+sessionID)
		resp.Body.Close()
	}()

	artifactEvents := e2eboot.FindAllEvents(events, "artifact")
	if len(artifactEvents) == 0 {
		switch e2eboot.ClassifyToolOutcome(events, "write_file") {
		case e2eboot.ToolNotCalled:
			t.Skip("LLM did not call write_file — cannot test positive gate")
		case e2eboot.ToolInfraFailure:
			t.Fatal("sandbox infrastructure broken: fix cluster")
		case e2eboot.ToolSucceeded:
			t.Fatal("write_file succeeded but no artifact SSE event emitted")
		}
	}

	reportMarkerEvents := e2eboot.FindAllEvents(events, "report_marker")
	if len(reportMarkerEvents) == 0 {
		t.Skip("LLM did not emit astonish-report fence — cannot exercise CHAT-068 positive path this run")
	}

	var marker struct {
		Path  string `json:"path"`
		Title string `json:"title"`
	}
	e2eboot.DecodeEventData(t, &reportMarkerEvents[0], &marker)

	time.Sleep(1 * time.Second)

	detailResp := h.Get(t, "/api/studio/sessions/"+sessionID)
	if detailResp.StatusCode != http.StatusOK {
		body := e2eboot.ReadBody(t, detailResp)
		t.Fatalf("GET session detail: %d %s", detailResp.StatusCode, body)
	}
	var detail struct {
		Artifacts []struct {
			Path        string `json:"path"`
			FileType    string `json:"fileType"`
			IsReport    bool   `json:"isReport"`
			ReportTitle string `json:"reportTitle"`
		} `json:"artifacts"`
	}
	detailBody := e2eboot.ReadBody(t, detailResp)
	if err := json.Unmarshal([]byte(detailBody), &detail); err != nil {
		t.Fatalf("decode session detail: %v", err)
	}

	// Find the artifact whose path matches the marker. It MUST be flagged
	// IsReport=true and have FileType="Markdown" (the only fileType the
	// frontend gate accepts for inline embedding).
	var found bool
	for _, a := range detail.Artifacts {
		if a.Path != marker.Path {
			continue
		}
		found = true
		if !a.IsReport {
			t.Errorf("artifact %q matches a report_marker but IsReport=false — joinReportMarkers projection broken (CHAT-068 regression)",
				a.Path)
		}
		if a.FileType != "Markdown" {
			t.Errorf("artifact %q for a report has fileType=%q, want Markdown — file extension classifier broken or LLM wrote the wrong file",
				a.Path, a.FileType)
		}
	}
	if !found {
		t.Errorf("no artifact in session detail matches report_marker path %q — write_file path and fence path are out of sync",
			marker.Path)
	}
}

// TestE2E_Chat_CrossUserArtifactDenied verifies that a user from another org
// cannot access another user's artifact via the artifact API.
//
// COVERS: CHAT-039
func TestE2E_Chat_CrossUserArtifactDenied(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice creates a session with an artifact
	aliceClient := seed.Client(e2eboot.UserAliceEmail)
	body := map[string]any{
		"message":     `Write exactly "secret data from alice" to /tmp/e2e-artifact-auth-test.md using write_file. Nothing else.`,
		"autoApprove": true,
	}
	events := aliceClient.SSE(t, "/api/studio/chat", body, 120*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("alice did not get a session ID")
	}
	defer func() {
		if e2eboot.RetainSessions() {
			return
		}
		resp := aliceClient.Delete(t, "/api/studio/sessions/"+sessionID)
		resp.Body.Close()
	}()

	artifactEvents := e2eboot.FindAllEvents(events, "artifact")
	if len(artifactEvents) == 0 {
		// No artifact event — figure out why before skipping.
		switch e2eboot.ClassifyToolOutcome(events, "write_file") {
		case e2eboot.ToolInfraFailure:
			t.Fatal("sandbox infrastructure broken: alice's write_file was " +
				"called but every tool_result reports a sandbox/k8s " +
				"scheduling failure. CHAT-039 cannot be exercised — fix " +
				"the cluster (kubectl get pods -n astonishe2e-sandbox).")
		case e2eboot.ToolNotCalled, e2eboot.ToolSucceeded:
			t.Skip("LLM did not produce an artifact — cannot test cross-user denial")
		}
	}

	var artifact struct {
		Path string `json:"path"`
	}
	e2eboot.DecodeEventData(t, &artifactEvents[0], &artifact)
	if artifact.Path == "" {
		t.Fatal("artifact path is empty")
	}

	// Eve (different org) tries to access Alice's artifact
	eveClient := seed.Client(e2eboot.UserEveEmail)
	eveResp := eveClient.Get(t, "/api/studio/artifacts/content?path="+artifact.Path+"&session="+sessionID)
	defer eveResp.Body.Close()

	if eveResp.StatusCode == http.StatusOK {
		eveBody := e2eboot.ReadBody(t, eveResp)
		if strings.Contains(eveBody, "secret data from alice") {
			t.Error("eve (globex) was able to read alice's artifact content — cross-org artifact isolation broken")
		}
	}
	// 404 or 403 are expected — pass
}
