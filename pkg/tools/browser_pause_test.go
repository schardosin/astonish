package tools

import "testing"

func TestBrowserPause_ShortDuration(t *testing.T) {
	fn := BrowserPause(nil)
	res, err := fn(nil, BrowserPauseArgs{Ms: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.PausedMs != 20 {
		t.Fatalf("PausedMs = %d, want 20", res.PausedMs)
	}
}

func TestBrowserPause_RejectsZero(t *testing.T) {
	fn := BrowserPause(nil)
	if _, err := fn(nil, BrowserPauseArgs{Ms: 0}); err == nil {
		t.Fatal("expected error for ms=0")
	}
}

func TestBrowserPause_RejectsOverCap(t *testing.T) {
	fn := BrowserPause(nil)
	if _, err := fn(nil, BrowserPauseArgs{Ms: browserPauseMaxMs + 1}); err == nil {
		t.Fatal("expected error for ms over cap")
	}
}
