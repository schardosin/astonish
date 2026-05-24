//go:build e2e

package e2eboot

import "testing"

// TestClassifyToolOutcome covers the three classification paths the
// helper is meant to distinguish: "the LLM never tried this tool"
// (test should skip), "the LLM tried and the sandbox executed it"
// (test continues), and "the LLM tried but every tool_result reports
// a recognizable sandbox/k8s infrastructure failure" (test should
// FAIL loudly so a broken cluster doesn't masquerade as a green skip).
func TestClassifyToolOutcome(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		events []SSEEvent
		want   ToolOutcome
	}{
		{
			name: "tool never called -> ToolNotCalled",
			events: []SSEEvent{
				{Type: "text", Data: "I'll just answer directly."},
			},
			want: ToolNotCalled,
		},
		{
			name: "tool called and succeeded -> ToolSucceeded",
			events: []SSEEvent{
				{Type: "tool_call", Data: `{"name":"write_file","args":{"path":"/tmp/x"}}`},
				{Type: "tool_result", Data: `{"tool_name":"write_file","result":"OK 12 bytes written"}`},
			},
			want: ToolSucceeded,
		},
		{
			name: "tool called, every result is k8s 'no host assigned' -> ToolInfraFailure",
			events: []SSEEvent{
				{Type: "tool_call", Data: `{"name":"write_file","args":{"path":"/tmp/x"}}`},
				{Type: "tool_result", Data: `{"tool_name":"write_file","error":"backend exec: sandbox/k8s: Exec(s1): unable to upgrade connection: pod astn-sess-abc does not have a host assigned"}`},
			},
			want: ToolInfraFailure,
		},
		{
			name: "tool called multiple times, all fail with infra patterns -> ToolInfraFailure",
			events: []SSEEvent{
				{Type: "tool_call", Data: `{"name":"write_file"}`},
				{Type: "tool_result", Data: `{"tool_name":"write_file","error":"failed to create pod: insufficient memory"}`},
				{Type: "tool_call", Data: `{"name":"write_file"}`},
				{Type: "tool_result", Data: `{"tool_name":"write_file","error":"unable to upgrade connection"}`},
			},
			want: ToolInfraFailure,
		},
		{
			name: "tool called, mixed infra failure + eventual success -> ToolSucceeded",
			events: []SSEEvent{
				{Type: "tool_call", Data: `{"name":"write_file"}`},
				{Type: "tool_result", Data: `{"tool_name":"write_file","error":"unable to upgrade connection"}`},
				{Type: "tool_call", Data: `{"name":"write_file"}`},
				{Type: "tool_result", Data: `{"tool_name":"write_file","result":"wrote 5 bytes"}`},
			},
			want: ToolSucceeded,
		},
		{
			name: "tool called, ordinary tool error (not infra) -> ToolSucceeded",
			// A non-infra error like "permission denied" still counts as
			// "tool ran" — it's the test's job to interpret the error.
			events: []SSEEvent{
				{Type: "tool_call", Data: `{"name":"write_file"}`},
				{Type: "tool_result", Data: `{"tool_name":"write_file","error":"permission denied"}`},
			},
			want: ToolSucceeded,
		},
		{
			name: "tool called but no tool_result arrived -> ToolInfraFailure",
			// An open SSE stream that ended before the tool_result arrived
			// is itself an infrastructure problem (backend died mid-stream).
			events: []SSEEvent{
				{Type: "tool_call", Data: `{"name":"write_file"}`},
			},
			want: ToolInfraFailure,
		},
		{
			name: "different tool was called, target tool ignored -> ToolNotCalled",
			events: []SSEEvent{
				{Type: "tool_call", Data: `{"name":"read_file"}`},
				{Type: "tool_result", Data: `{"tool_name":"read_file","result":"contents"}`},
			},
			want: ToolNotCalled,
		},
		{
			name: "case-insensitive infra pattern match",
			events: []SSEEvent{
				{Type: "tool_call", Data: `{"name":"write_file"}`},
				{Type: "tool_result", Data: `{"tool_name":"write_file","error":"Pod XYZ Does Not Have A Host Assigned"}`},
			},
			want: ToolInfraFailure,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyToolOutcome(tc.events, "write_file")
			if got != tc.want {
				t.Errorf("ClassifyToolOutcome = %v, want %v", got, tc.want)
			}
		})
	}
}
