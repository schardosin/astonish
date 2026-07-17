package tools

import (
	"testing"

	"github.com/SAP/astonish/pkg/browser"
)

func TestBrowserHighlight_RequiresTarget(t *testing.T) {
	mgr := browser.NewManager(browser.DefaultConfig())
	refs := browser.NewRefMap()
	fn := BrowserHighlight(mgr, refs)
	_, err := fn(nil, BrowserHighlightArgs{})
	if err == nil {
		t.Fatal("expected error when ref and selector are empty")
	}
}

func TestBrowserMoveCursor_RequiresTarget(t *testing.T) {
	mgr := browser.NewManager(browser.DefaultConfig())
	refs := browser.NewRefMap()
	fn := BrowserMoveCursor(mgr, refs)
	_, err := fn(nil, BrowserMoveCursorArgs{})
	if err == nil {
		t.Fatal("expected error when no target")
	}
}

func TestBrowserFullscreen_NoPage(t *testing.T) {
	mgr := browser.NewManager(browser.DefaultConfig())
	fn := BrowserFullscreen(mgr)
	_, err := fn(nil, BrowserFullscreenArgs{Enabled: true})
	if err == nil {
		t.Fatal("expected error without a page")
	}
}

func TestBrowserClearHighlights_NoPage(t *testing.T) {
	mgr := browser.NewManager(browser.DefaultConfig())
	fn := BrowserClearHighlights(mgr)
	_, err := fn(nil, BrowserClearHighlightsArgs{})
	if err == nil {
		t.Fatal("expected error without a page")
	}
}
