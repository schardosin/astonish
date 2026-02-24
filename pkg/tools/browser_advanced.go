package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
