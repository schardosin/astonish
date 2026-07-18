package treesitter

import "testing"

func TestQueryCursor_NilGuards(t *testing.T) {
	c := NewQueryCursor(nil)
	if c == nil {
		t.Fatal("NewQueryCursor(nil) returned nil")
	}
	if c.ptr != 0 {
		t.Fatalf("expected zero ptr, got %#x", c.ptr)
	}

	// Must not panic.
	c.Exec(nil, Node{})
	c.Exec(&Query{}, Node{})
	if _, ok := c.NextMatch(); ok {
		t.Fatal("NextMatch on nil cursor should be false")
	}
	if _, _, ok := c.NextCapture(); ok {
		t.Fatal("NextCapture on nil cursor should be false")
	}
	if c.SetByteRange(0, 1) {
		t.Fatal("SetByteRange on nil cursor should be false")
	}
	c.Close()
}
