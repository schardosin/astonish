package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/provider/bedrock"
	"github.com/SAP/astonish/pkg/provider/streaming"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/genai"
)

// Separate read gates prevent a fast local stream from replacing a status before
// Chromium paints it. Every gate is below the production Bedrock stream parser;
// no browser fetch, SSE events, React state, or production hooks are mocked.
type streamBrowserLLM struct {
	held    [3]chan struct{}
	release [3]chan struct{}
	calls   atomic.Int32
}

func (m *streamBrowserLLM) GenerateContent(ctx context.Context, _ *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if !stream {
			yield(nil, fmt.Errorf("browser acceptance requires streaming"))
			return
		}
		if m.calls.Add(1) > 1 {
			yield(&model.LLMResponse{Content: genai.NewContentFromText("Browser probe finished.", "model"), TurnComplete: true}, nil)
			return
		}
		frames := []string{
			`data: {"type":"message_start","message":{"usage":{"input_tokens":5}}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"PRIVATE_REASONING"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"PRIVATE_REASONING"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"PRIVATE_SIGNATURE"}}

data: {"type":"content_block_stop","index":0}

`,
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"browser-probe-1","name":"progress_probe","input":{}}}

data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"input\":\"PRIVATE_"}}

`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"ARGUMENT\"}"}}

data: {"type":"content_block_stop","index":1}

data: {"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Browser partial answer"}}

`,
		}
		var readers []io.Reader
		for i, frame := range frames {
			readers = append(readers, &generationHeldReader{ctx: ctx, prefix: strings.NewReader(frame), suffix: strings.NewReader(""), held: m.held[i], release: m.release[i]})
		}
		readers = append(readers, strings.NewReader(`data: {"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":" is complete."}}

data: {"type":"content_block_stop","index":2}

data: {"type":"message_stop"}

`))
		for response, err := range bedrock.ParseStreamWithProgress(io.MultiReader(readers...), func(p streaming.Progress) { streaming.Report(ctx, p) }) {
			if !yield(response, err) {
				return
			}
		}
	}
}

// Explicit runs fail rather than silently skipping unavailable SPA/browser
// prerequisites. Run after `cd web && npm run build` with:
// ASTONISH_STREAM_BROWSER_ACCEPTANCE=1 go test ./pkg/api -run '^TestChatGenerationProgressBrowser$' -count=1 -timeout=3m -v
func TestChatGenerationProgressBrowser(t *testing.T) {
	if os.Getenv("ASTONISH_STREAM_BROWSER_ACCEPTANCE") != "1" {
		t.Skip("explicit browser acceptance: set ASTONISH_STREAM_BROWSER_ACCEPTANCE=1")
	}
	var pkg struct {
		Version string `json:"version"`
	}
	data, err := os.ReadFile("../../web/package.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatal(err)
	}
	dist, err := filepath.Abs(filepath.Join("../../web/dist", pkg.Version))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err != nil {
		t.Fatalf("build Studio SPA before browser acceptance: %v", err)
	}
	bin := os.Getenv("ASTONISH_STREAM_BROWSER_PATH")
	if bin == "" {
		var found bool
		bin, found = launcher.LookPath()
		if !found {
			t.Fatal("browser prerequisite missing; set ASTONISH_STREAM_BROWSER_PATH")
		}
	}
	provider := &streamBrowserLLM{}
	var releases [3]sync.Once
	for i := range provider.held {
		provider.held[i], provider.release[i] = make(chan struct{}), make(chan struct{})
	}
	release := func(i int) { releases[i].Do(func() { close(provider.release[i]) }) }
	var executed atomic.Int32
	probe, err := functiontool.New(functiontool.Config{Name: "progress_probe", Description: "Record a completed browser tool call"}, func(_ tool.Context, args struct {
		Input string `json:"input"`
	}) (mockToolResult, error) {
		executed.Add(1)
		if args.Input != "PRIVATE_ARGUMENT" {
			return mockToolResult{}, fmt.Errorf("incomplete arguments: %q", args.Input)
		}
		return mockToolResult{Output: map[string]any{"ok": true}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	env := setupMemoryHTTPAcceptance(t, &MockLLM{funcLLM: provider}, []tool.Tool{probe})
	// Registered after env cleanup so failed assertions release blocked readers
	// before the fixture waits for its HTTP requests and runners to terminate.
	t.Cleanup(func() {
		for i := range releases {
			release(i)
		}
	})
	env.Router.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var fixture any
		switch r.URL.Path {
		case "/api/auth/setup-status":
			fixture = map[string]any{"initialized": true, "auth_mode": "builtin"}
		case "/api/auth/me":
			fixture = map[string]any{"user": map[string]any{"id": env.UserID, "email": "stream@example.invalid", "display_name": "Streaming acceptance", "role": "owner", "platform_role": "superadmin"}, "org": map[string]any{"slug": env.OrgSlug, "name": "Streaming acceptance"}, "team": env.TeamSlug}
		case "/api/orgs":
			fixture = map[string]any{"orgs": []any{}, "active_org": env.OrgSlug}
		case "/api/teams":
			fixture = []any{map[string]any{"slug": env.TeamSlug, "name": "General"}}
		case "/api/settings/status":
			fixture = map[string]any{"setupRequired": false}
		case "/api/version":
			fixture = map[string]any{"version": "dev"}
		case "/api/agents", "/api/tools":
			fixture = []any{}
		case "/api/settings/config":
			fixture = map[string]any{}
		case "/api/settings/providers/effective":
			fixture = map[string]any{"providers": map[string]any{}}
		}
		if fixture != nil {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(fixture)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.Error(w, "fixture endpoint unavailable", http.StatusNotFound)
			return
		}
		path := filepath.Join(dist, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	})
	launch := launcher.New().Bin(bin).UserDataDir(t.TempDir()).Headless(true).Leakless(false)
	control, err := launch.Launch()
	if err != nil {
		t.Fatalf("launch isolated browser: %v", err)
	}
	t.Cleanup(launch.Cleanup)
	browser := rod.New().ControlURL(control).MustConnect()
	t.Cleanup(func() { _ = browser.Close() })
	page := browser.MustPage(env.URL + "/#/chat").Timeout(60 * time.Second)
	page = page.WithPanic(func(v any) { html, _ := page.CancelTimeout().HTML(); t.Fatalf("browser failure: %v\n%s", v, html) })
	page.MustElement(`[data-testid="chat-input"]`).MustInput("Run the browser progress probe").MustType(input.Enter)
	assertPrivate := func() {
		t.Helper()
		if page.MustEval(`() => /PRIVATE_REASONING|PRIVATE_SIGNATURE/.test(document.body.textContent)`).Bool() {
			t.Fatal("private reasoning or signature leaked into Studio")
		}
	}
	for i, text := range []string{"Model is thinking…", "Preparing tool arguments…", "Browser partial answer"} {
		select {
		case <-provider.held[i]:
		case <-time.After(15 * time.Second):
			t.Fatalf("provider did not reach browser gate %d", i)
		}
		page.MustElementR("span,p,div.thinking-note", "^"+text+"$").MustVisible()
		page.MustWait(`() => { const stop=document.querySelector('button[title="Stop"]'); const input=document.querySelector('[data-testid="chat-input"]'); return stop && !stop.disabled && input?.getAttribute('placeholder')==='Agent is responding...' }`)
		assertPrivate()
		if executed.Load() != 0 || provider.calls.Load() != 1 {
			t.Fatalf("generation/tool completed before terminal release: calls=%d tools=%d", provider.calls.Load(), executed.Load())
		}
		if page.MustEval(`() => /PRIVATE_ARGUMENT|progress_probe|Browser probe finished\.|Browser partial answer is complete\./.test(document.body.textContent)`).Bool() {
			t.Fatal("terminal tool payload or final text appeared before release")
		}
		t.Logf("browser painted %q while provider read is held and Stop is enabled", text)
		if i == 2 && os.Getenv("ASTONISH_STREAM_BROWSER_SCREENSHOT") != "" {
			page.MustScreenshot(os.Getenv("ASTONISH_STREAM_BROWSER_SCREENSHOT"))
		}
		release(i)
	}
	page.MustElementR("span,p", "^Browser probe finished\\.$").MustVisible()
	page.MustWait(`() => !document.querySelector('button[title="Stop"]')`)
	assertComplete := func() {
		t.Helper()
		assertPrivate()
		// Studio intentionally keeps only the latest answer bubble for a tool
		// run; earlier streamed text becomes activity/history, not a second answer.
		for _, text := range []string{"Browser probe finished."} {
			if got := page.MustEval(`text => document.body.innerText.split(text).length-1`, text).Int(); got != 1 {
				t.Fatalf("visible %q count=%d, want 1; transcript=%s", text, got, page.MustElement(`[data-testid="message-area"]`).MustText())
			}
		}
		if page.MustEval(`() => !!document.querySelector('[data-testid="thinking-indicator"]')`).Bool() {
			t.Fatal("active generation indicator survived completion")
		}
	}
	assertComplete()
	if executed.Load() != 1 || provider.calls.Load() != 2 {
		t.Fatalf("final tools=%d calls=%d, want 1 and 2", executed.Load(), provider.calls.Load())
	}
	sid := page.MustEval(`() => location.hash.split('/')[2]`).Str()
	if sid == "" {
		t.Fatal("browser did not navigate to persisted session")
	}
	detail := env.Detail(t, sid)
	counts := map[string]int{}
	answers := map[string]int{}
	for _, msg := range detail.Messages {
		counts[msg.Type]++
		if msg.Type == "agent" {
			answers[msg.Content]++
		}
	}
	if counts["agent"] != 2 || answers["Browser partial answer is complete."] != 1 || answers["Browser probe finished."] != 1 {
		t.Fatalf("persisted answers lost or duplicated streamed text: %v", answers)
	}
	if counts["tool_call"] != 1 || counts["tool_result"] != 1 {
		t.Fatalf("persisted tool lifecycle not exactly once: %v", counts)
	}
	data, err = json.Marshal(detail.Messages)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "PRIVATE_REASONING") || strings.Contains(string(data), "PRIVATE_SIGNATURE") {
		t.Fatal("private reasoning or signature leaked into restored history")
	}
	page.MustReload().MustElementR("span,p", "^Browser probe finished\\.$").MustVisible()
	page.MustWait(`() => !document.querySelector('button[title="Stop"]')`)
	assertComplete()
	t.Logf("PASS: real browser send → held thinking → held tool arguments → held partial text → one tool call/result → unduplicated final text → persisted reload; browser=%s SPA=%s", bin, dist)
}
