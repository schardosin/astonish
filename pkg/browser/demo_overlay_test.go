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

func TestClearHighlights_NoPage(t *testing.T) {
	m := NewManager(DefaultConfig())
	// No browser launched — CurrentPage should fail.
	if err := m.ClearHighlights(); err == nil {
		t.Fatal("expected error without a page")
	}
}

func TestMoveMouseAnimated_NilPage(t *testing.T) {
	m := NewManager(DefaultConfig())
	if err := m.MoveMouseAnimated(nil, proto.NewPoint(1, 2), 1); err == nil {
		t.Fatal("expected error for nil page")
	}
}

func TestEnableDemoCursor_NoPage(t *testing.T) {
	m := NewManager(DefaultConfig())
	if err := m.EnableDemoCursor(); err == nil {
		t.Fatal("expected error without a page")
	}
}

func TestSetFullscreen_NoPage(t *testing.T) {
	m := NewManager(DefaultConfig())
	if err := m.SetFullscreen(true); err == nil {
		t.Fatal("expected error without a page")
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
