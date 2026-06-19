package sandbox

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// nopCloser wraps an io.Reader as an io.ReadCloser with a no-op Close.
type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

func TestJSONLineFilterReader_PureJSON(t *testing.T) {
	// All lines are valid JSON-RPC — they should all pass through.
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" +
		`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}` + "\n"

	r := newJSONLineFilterReader(nopCloser{strings.NewReader(input)})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != input {
		t.Errorf("expected all JSON to pass through\ngot:  %q\nwant: %q", string(out), input)
	}
}

func TestJSONLineFilterReader_DiscardsANSI(t *testing.T) {
	// ANSI spinner output interleaved with JSON responses.
	input := "\x1b[1G\x1b[0K|\x1b[1G\x1b[0K/\x1b[1G\x1b[0K-\n" +
		"\x1b[1G\x1b[0K\\\x1b[1G\x1b[0K|\n" +
		`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}` + "\n" +
		"\x1b[1G\x1b[0K\n"

	expected := `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}` + "\n"

	r := newJSONLineFilterReader(nopCloser{strings.NewReader(input)})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != expected {
		t.Errorf("ANSI lines not filtered\ngot:  %q\nwant: %q", string(out), expected)
	}
}

func TestJSONLineFilterReader_DiscardsBanner(t *testing.T) {
	// MCP server startup banner followed by JSON.
	input := "Tavily MCP server running on stdio\n" +
		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}` + "\n"

	expected := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}` + "\n"

	r := newJSONLineFilterReader(nopCloser{strings.NewReader(input)})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != expected {
		t.Errorf("banner not filtered\ngot:  %q\nwant: %q", string(out), expected)
	}
}

func TestJSONLineFilterReader_CRLFLineEndings(t *testing.T) {
	// PTY output uses \r\n line endings.
	input := "\x1b[1G\x1b[0K|\r\n" +
		`{"jsonrpc":"2.0","id":1,"result":{}}` + "\r\n"

	// The filter passes the full line including \r\n.
	expected := `{"jsonrpc":"2.0","id":1,"result":{}}` + "\r\n"

	r := newJSONLineFilterReader(nopCloser{strings.NewReader(input)})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != expected {
		t.Errorf("CRLF handling wrong\ngot:  %q\nwant: %q", string(out), expected)
	}
}

func TestJSONLineFilterReader_UnterminatedJSONOnEOF(t *testing.T) {
	// Stream closes without final newline — should still pass JSON.
	input := `{"jsonrpc":"2.0","id":1,"result":{}}`

	r := newJSONLineFilterReader(nopCloser{strings.NewReader(input)})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != input {
		t.Errorf("unterminated JSON not passed\ngot:  %q\nwant: %q", string(out), input)
	}
}

func TestJSONLineFilterReader_UnterminatedNonJSONOnEOF(t *testing.T) {
	// Stream closes with unterminated non-JSON — should be discarded.
	input := "some random garbage without newline"

	r := newJSONLineFilterReader(nopCloser{strings.NewReader(input)})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("unterminated non-JSON should be discarded, got: %q", string(out))
	}
}

func TestJSONLineFilterReader_SmallReadBuffer(t *testing.T) {
	// Read with a very small buffer to test partial serving.
	input := `{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"tools":{}}}}` + "\n"

	r := newJSONLineFilterReader(nopCloser{strings.NewReader(input)})

	var result bytes.Buffer
	buf := make([]byte, 5) // tiny buffer
	for {
		n, err := r.Read(buf)
		if n > 0 {
			result.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if result.String() != input {
		t.Errorf("small buffer reads failed\ngot:  %q\nwant: %q", result.String(), input)
	}
}

func TestJSONLineFilterReader_MixedInterleavedContamination(t *testing.T) {
	// Simulates the exact scenario from the bug: npx spinner interleaved
	// between multiple JSON-RPC messages.
	input := "\x1b[1G\x1b[0K⠋ Installing...\n" +
		"\x1b[1G\x1b[0K⠙ Installing...\n" +
		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n" +
		"\x1b[1G\x1b[0K\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		"Node.js ExperimentalWarning: something\n" +
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"search"}]}}` + "\n"

	expected := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"search"}]}}` + "\n"

	r := newJSONLineFilterReader(nopCloser{strings.NewReader(input)})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != expected {
		t.Errorf("interleaved contamination not handled\ngot:  %q\nwant: %q", string(out), expected)
	}
}

func TestJSONLineFilterReader_EmptyInput(t *testing.T) {
	r := newJSONLineFilterReader(nopCloser{strings.NewReader("")})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty output, got: %q", string(out))
	}
}

func TestJSONLineFilterReader_OnlyNonJSON(t *testing.T) {
	// Stream with only non-JSON lines — should produce empty output.
	input := "hello world\n\x1b[31mred text\x1b[0m\nsome banner\n"

	r := newJSONLineFilterReader(nopCloser{strings.NewReader(input)})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty output for non-JSON input, got: %q", string(out))
	}
}

func TestIsJSONRPCLine(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{`{"jsonrpc":"2.0"}` + "\n", true},
		{`{"id":1,"result":{}}` + "\n", true},
		{"\r" + `{"jsonrpc":"2.0"}` + "\r\n", true}, // leading \r stripped
		{`{not-a-quote}` + "\n", false},             // { not followed by "
		{"\x1b[1G\x1b[0K\n", false},                 // ANSI
		{"hello\n", false},                           // plain text
		{"{", false},                                 // too short
		{"{\n", false},                               // { followed by \n, not "
		{`{""}` + "\n", true},                        // minimal valid prefix
	}
	for _, tt := range tests {
		got := isJSONRPCLine([]byte(tt.input))
		if got != tt.want {
			t.Errorf("isJSONRPCLine(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
