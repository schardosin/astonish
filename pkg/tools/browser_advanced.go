package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/schardosin/astonish/pkg/browser"
	"github.com/ysmood/gson"
	"google.golang.org/adk/tool"
)

// --- browser_evaluate ---

type BrowserEvaluateArgs struct {
	Expression string `json:"expression" jsonschema:"JavaScript expression to evaluate in the page context"`
	Ref        string `json:"ref,omitempty" jsonschema:"Element ref to use as 'this' context (optional)"`
}

type BrowserEvaluateResult struct {
	Result interface{} `json:"result"`
	Type   string      `json:"type"`
}

func BrowserEvaluate(mgr *browser.Manager, refs *browser.RefMap) func(tool.Context, BrowserEvaluateArgs) (BrowserEvaluateResult, error) {
	return func(_ tool.Context, args BrowserEvaluateArgs) (BrowserEvaluateResult, error) {
		if args.Expression == "" {
			return BrowserEvaluateResult{}, fmt.Errorf("expression is required")
		}

		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserEvaluateResult{}, err
		}

		var res *proto.RuntimeRemoteObject

		if args.Ref != "" {
			el, err := refs.ResolveElement(pg, args.Ref)
			if err != nil {
				return BrowserEvaluateResult{}, err
			}
			res, err = el.Eval(args.Expression)
			if err != nil {
				return BrowserEvaluateResult{}, fmt.Errorf("eval failed: %w", err)
			}
		} else {
			res, err = pg.Eval(args.Expression)
			if err != nil {
				return BrowserEvaluateResult{}, fmt.Errorf("eval failed: %w", err)
			}
		}

		return BrowserEvaluateResult{
			Result: gsonToInterface(res.Value),
			Type:   string(res.Type),
		}, nil
	}
}

// --- browser_run_code ---

type BrowserRunCodeArgs struct {
	Code string `json:"code" jsonschema:"Multi-line JavaScript code to execute in the page context. Has access to the full browser DOM API."`
}

type BrowserRunCodeResult struct {
	Result interface{} `json:"result"`
	Type   string      `json:"type"`
}

func BrowserRunCode(mgr *browser.Manager) func(tool.Context, BrowserRunCodeArgs) (BrowserRunCodeResult, error) {
	return func(_ tool.Context, args BrowserRunCodeArgs) (BrowserRunCodeResult, error) {
		if args.Code == "" {
			return BrowserRunCodeResult{}, fmt.Errorf("code is required")
		}

		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserRunCodeResult{}, err
		}

		// Wrap in an async IIFE to support await.
		// Apply a 30-second timeout so runaway scripts don't hang forever.
		wrapped := fmt.Sprintf("(async () => { %s })()", args.Code)
		res, err := pg.Timeout(30 * time.Second).Evaluate(rod.Eval(wrapped).ByPromise())
		if err != nil {
			return BrowserRunCodeResult{}, fmt.Errorf("code execution failed: %w", err)
		}

		return BrowserRunCodeResult{
			Result: gsonToInterface(res.Value),
			Type:   string(res.Type),
		}, nil
	}
}

// --- browser_pdf ---

type BrowserPDFArgs struct {
	Path string `json:"path,omitempty" jsonschema:"File path to save the PDF (default: temp file)"`
}

type BrowserPDFResult struct {
	Path      string `json:"path"`
	SizeBytes int    `json:"size_bytes"`
}

func BrowserPDF(mgr *browser.Manager) func(tool.Context, BrowserPDFArgs) (BrowserPDFResult, error) {
	return func(_ tool.Context, args BrowserPDFArgs) (BrowserPDFResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserPDFResult{}, err
		}

		reader, err := pg.PDF(&proto.PagePrintToPDF{
			PrintBackground: true,
		})
		if err != nil {
			return BrowserPDFResult{}, fmt.Errorf("PDF generation failed: %w", err)
		}
		defer reader.Close()

		data, err := io.ReadAll(reader)
		if err != nil {
			return BrowserPDFResult{}, fmt.Errorf("failed to read PDF data: %w", err)
		}

		savePath := args.Path
		if savePath == "" {
			tmpFile, err := os.CreateTemp("", "browser-*.pdf")
			if err != nil {
				return BrowserPDFResult{}, fmt.Errorf("failed to create temp file: %w", err)
			}
			savePath = tmpFile.Name()
			tmpFile.Close()
		} else {
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(savePath), 0755); err != nil {
				return BrowserPDFResult{}, fmt.Errorf("failed to create directory: %w", err)
			}
		}

		if err := os.WriteFile(savePath, data, 0644); err != nil {
			return BrowserPDFResult{}, fmt.Errorf("failed to write PDF: %w", err)
		}

		return BrowserPDFResult{
			Path:      savePath,
			SizeBytes: len(data),
		}, nil
	}
}

// gsonToInterface converts a gson.JSON value to a Go interface.
func gsonToInterface(v gson.JSON) interface{} {
	if v.Nil() {
		return nil
	}

	// Use MarshalJSON to get bytes, then unmarshal to generic interface
	data, err := v.MarshalJSON()
	if err != nil {
		return v.String()
	}

	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return v.String()
	}
	return result
}

// --- browser_response_body ---

type BrowserResponseBodyArgs struct {
	Action     string `json:"action" jsonschema:"Action: listen (start intercepting), read (get captured response), or stop (remove interceptor)"`
	URLPattern string `json:"urlPattern,omitempty" jsonschema:"URL pattern to match (wildcard: * matches any chars). Used with action=listen. Example: '*api/data*'"`
	Timeout    int    `json:"timeout,omitempty" jsonschema:"Max milliseconds to wait for a matching response (used with action=read, default 30000)"`
	MaxChars   int    `json:"maxChars,omitempty" jsonschema:"Maximum characters in response body (used with action=read, default 50000)"`
}

type BrowserResponseBodyResult struct {
	Success      bool              `json:"success"`
	Message      string            `json:"message,omitempty"`
	Listening    bool              `json:"listening,omitempty"`
	URLPattern   string            `json:"urlPattern,omitempty"`
	URL          string            `json:"url,omitempty"`
	Status       int               `json:"status,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         string            `json:"body,omitempty"`
	ResourceType string            `json:"resourceType,omitempty"`
	Truncated    bool              `json:"truncated,omitempty"`
}

func BrowserResponseBody(mgr *browser.Manager) func(tool.Context, BrowserResponseBodyArgs) (BrowserResponseBodyResult, error) {
	return func(_ tool.Context, args BrowserResponseBodyArgs) (BrowserResponseBodyResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserResponseBodyResult{}, err
		}

		ps := mgr.PageStateFor(pg)
		if ps == nil {
			return BrowserResponseBodyResult{}, fmt.Errorf("no page state available")
		}

		switch args.Action {
		case "listen":
			return handleResponseListen(pg, ps, args)
		case "read":
			return handleResponseRead(ps, args)
		case "stop":
			return handleResponseStop(ps)
		default:
			return BrowserResponseBodyResult{}, fmt.Errorf("invalid action %q: must be listen, read, or stop", args.Action)
		}
	}
}

func handleResponseListen(pg *rod.Page, ps *browser.PageState, args BrowserResponseBodyArgs) (BrowserResponseBodyResult, error) {
	if args.URLPattern == "" {
		return BrowserResponseBodyResult{}, fmt.Errorf("urlPattern is required for action=listen")
	}

	// Stop any existing capture first.
	if ps.Capture.IsActive() {
		_ = ps.Capture.Stop()
	}

	router := pg.HijackRequests()
	if err := router.Add(args.URLPattern, "", func(ctx *rod.Hijack) {
		// Let the request proceed to the real server and capture the response.
		if err := ctx.LoadResponse(http.DefaultClient, true); err != nil {
			return
		}

		// Extract headers as a simple map.
		headers := make(map[string]string)
		for k, vs := range ctx.Response.Headers() {
			headers[k] = strings.Join(vs, ", ")
		}

		ps.Capture.AddResponse(browser.CapturedResponse{
			URL:          ctx.Request.URL().String(),
			Status:       ctx.Response.Payload().ResponseCode,
			Headers:      headers,
			Body:         ctx.Response.Body(),
			ResourceType: string(ctx.Request.Type()),
			Timestamp:    time.Now(),
		})
	}); err != nil {
		return BrowserResponseBodyResult{}, fmt.Errorf("failed to set up response interceptor: %w", err)
	}

	ps.Capture.SetRouter(router, args.URLPattern)
	go router.Run()

	return BrowserResponseBodyResult{
		Success:    true,
		Listening:  true,
		URLPattern: args.URLPattern,
		Message:    fmt.Sprintf("Listening for responses matching %q", args.URLPattern),
	}, nil
}

func handleResponseRead(ps *browser.PageState, args BrowserResponseBodyArgs) (BrowserResponseBodyResult, error) {
	if !ps.Capture.IsActive() {
		return BrowserResponseBodyResult{}, fmt.Errorf("no response interceptor active — call with action=\"listen\" first")
	}

	timeout := args.Timeout
	if timeout <= 0 {
		timeout = 30000
	}
	maxChars := args.MaxChars
	if maxChars <= 0 {
		maxChars = 50000
	}

	// Check if we already have captured responses.
	responses := ps.Capture.Responses()
	if len(responses) == 0 {
		// Wait for a response up to timeout.
		timer := time.NewTimer(time.Duration(timeout) * time.Millisecond)
		defer timer.Stop()

		select {
		case <-ps.Capture.WaitChan():
			responses = ps.Capture.Responses()
		case <-timer.C:
			return BrowserResponseBodyResult{
				Success: false,
				Message: fmt.Sprintf("No matching response received within %dms", timeout),
			}, nil
		}
	}

	if len(responses) == 0 {
		return BrowserResponseBodyResult{
			Success: false,
			Message: "No responses captured",
		}, nil
	}

	// Return the most recent response.
	latest := responses[len(responses)-1]
	body := latest.Body
	truncated := false
	if len(body) > maxChars {
		body = body[:maxChars]
		truncated = true
	}

	return BrowserResponseBodyResult{
		Success:      true,
		URL:          latest.URL,
		Status:       latest.Status,
		Headers:      latest.Headers,
		Body:         body,
		ResourceType: latest.ResourceType,
		Truncated:    truncated,
		Message:      fmt.Sprintf("Captured response from %s (status %d, %d chars)", latest.URL, latest.Status, len(latest.Body)),
	}, nil
}

func handleResponseStop(ps *browser.PageState) (BrowserResponseBodyResult, error) {
	if !ps.Capture.IsActive() {
		return BrowserResponseBodyResult{
			Success: true,
			Message: "No active interceptor",
		}, nil
	}

	pattern := ps.Capture.Pattern()
	if err := ps.Capture.Stop(); err != nil {
		return BrowserResponseBodyResult{}, fmt.Errorf("failed to stop interceptor: %w", err)
	}

	return BrowserResponseBodyResult{
		Success: true,
		Message: fmt.Sprintf("Stopped interceptor for %q", pattern),
	}, nil
}
