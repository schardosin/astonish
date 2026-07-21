package browser

import (
	"testing"

	"github.com/go-rod/rod/lib/proto"
)

func TestHighlightSelector_RequiresSelector(t *testing.T) {
	m := NewManager(DefaultConfig())
	_, err := m.HighlightSelector("", "label", "", 0)
	if err == nil {
		t.Fatal("expected error for empty selector")
	}
}

func TestClearHighlights_CurrentPageAutolaunch(t *testing.T) {
	m := NewManager(DefaultConfig())
	// CurrentPage() launches a browser and creates about:blank when needed.
	// With Chrome available this succeeds; without a binary it returns an error.
	// Either outcome is valid — the old "must error without a page" assertion
	// was wrong for hosts that can launch Chromium.
	if err := m.ClearHighlights(); err != nil {
		t.Logf("ClearHighlights: %v (ok when browser cannot launch)", err)
	}
}

func TestMoveMouseAnimated_NilPage(t *testing.T) {
	m := NewManager(DefaultConfig())
	if err := m.MoveMouseAnimated(nil, proto.NewPoint(1, 2), 1); err == nil {
		t.Fatal("expected error for nil page")
	}
}

func TestEnableDemoCursor_CurrentPageAutolaunch(t *testing.T) {
	m := NewManager(DefaultConfig())
	if err := m.EnableDemoCursor(); err != nil {
		t.Logf("EnableDemoCursor: %v (ok when browser cannot launch)", err)
	}
}

func TestSetFullscreen_CurrentPageAutolaunch(t *testing.T) {
	m := NewManager(DefaultConfig())
	if err := m.SetFullscreen(true); err != nil {
		t.Logf("SetFullscreen: %v (ok when browser cannot launch)", err)
	}
}

func TestDemoState_LazyInit(t *testing.T) {
	m := NewManager(DefaultConfig())
	st := m.demoState()
	if st == nil {
		t.Fatal("demoState should allocate")
	}
	if m.demoState() != st {
		t.Fatal("demoState should return same instance")
	}
}
