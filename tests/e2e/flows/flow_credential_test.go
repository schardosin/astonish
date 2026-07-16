//go:build e2e

// Package flows contains E2E tests for Flow execution via the platform APIs.
package flows

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SAP/astonish/tests/e2eboot"
)

// Flow YAML for credential resolution testing.
// Uses a simple echo command to verify that {{CREDENTIAL:...}} placeholders
// are resolved to real values at tool execution time (BeforeToolCallback).
const flowCredentialYAML = `name: e2e_cred_flow
description: E2E test for credential resolution in flow execution
nodes:
  - name: get_credential_name
    type: input
    prompt: "Enter credential name:"
    output_model:
      credential_name: string
  - name: use_credential
    type: llm
    prompt: "Execute the shell command below EXACTLY as written using the shell_command tool. Do not modify it in any way. After execution, report the exact output."
    tools: true
    tools_selection:
      - shell_command
    tools_auto_approval: true
    raw_context: |
      Run this EXACT command using shell_command (copy-paste verbatim, do not modify anything):
      echo "CRED_USER={{CREDENTIAL:{credential_name}:username}} CRED_PASS={{CREDENTIAL:{credential_name}:password}}"
flow:
  - from: START
    to: get_credential_name
  - from: get_credential_name
    to: use_credential
  - from: use_credential
    to: END
`

// TestE2E_FlowRun_CredentialResolution verifies that {{CREDENTIAL:...}} placeholders
// in flow raw_context are properly resolved when the flow is executed via Flow View
// (POST /api/agents/{name}/run).
//
// This is the exact path that was failing: the LLM receives the instruction containing
// credential placeholders and must pass them verbatim in tool call args, where the
// BeforeToolCallback resolves them to real values before the tool executes.
//
// The test simulates what a real user does:
//  1. Create a credential (password type with known username/password)
//  2. Create a flow that uses {{CREDENTIAL:{credential_name}:username}} in raw_context
//  3. Execute the flow with params (credential_name = the created credential)
//  4. Verify: credential resolved correctly, not leaked in SSE stream
//
// COVERS: FLOW-CRED-001
func TestE2E_FlowRun_CredentialResolution(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	client := seed.Client(e2eboot.UserAliceEmail) // Alice is team admin of "red"

	const credName = "e2e-flow-cred"
	const credUsername = "e2e-app-credential-id-12345"
	const credPassword = "e2e-app-secret-67890"
	const flowName = "e2e_cred_flow"

	// --- Step 1: Create a password credential via the API ---
	resp := client.Post(t, "/api/credentials?scope=team", map[string]any{
		"name": credName,
		"credential": map[string]any{
			"type":     "password",
			"username": credUsername,
			"password": credPassword,
		},
	})
	credBody := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create credential failed: %d %s", resp.StatusCode, credBody)
	}
	t.Logf("Created credential %q (type=password)", credName)

	// Cleanup credential at end
	t.Cleanup(func() {
		r := client.Delete(t, "/api/credentials/"+credName+"?scope=team")
		r.Body.Close()
	})

	// --- Step 2: Create the flow via PUT /api/agents/{name} ---
	resp = client.Put(t, "/api/agents/"+flowName, map[string]any{
		"yaml": flowCredentialYAML,
	})
	flowBody := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create flow failed: %d %s", resp.StatusCode, flowBody)
	}
	t.Logf("Created flow %q", flowName)

	// Cleanup flow at end
	t.Cleanup(func() {
		r := client.Delete(t, "/api/agents/"+flowName)
		r.Body.Close()
	})

	// --- Step 3: Execute the flow via POST /api/agents/{name}/run ---
	events := client.SSE(t, "/api/agents/"+flowName+"/run", map[string]any{
		"params": map[string]string{
			"get_credential_name": credName,
		},
	}, 120*time.Second)

	if len(events) == 0 {
		t.Fatal("no SSE events received from flow execution")
	}

	// --- Step 4: Assertions ---

	// 4a: Flow must complete successfully
	doneEv := e2eboot.FindEvent(events, "done")
	if doneEv == nil {
		// Dump all events for debugging
		for i, ev := range events {
			t.Logf("  event[%d] type=%q data=%s", i, ev.Type, ev.Data)
		}
		t.Fatal("no 'done' event — flow did not complete")
	}
	var doneData map[string]string
	if err := json.Unmarshal([]byte(doneEv.Data), &doneData); err != nil {
		t.Fatalf("failed to decode done event: %v", err)
	}
	if doneData["result"] != "ok" {
		// Dump all events for debugging
		for i, ev := range events {
			t.Logf("  event[%d] type=%q data=%s", i, ev.Type, ev.Data)
		}
		t.Fatalf("flow did not succeed: result=%q (expected 'ok')", doneData["result"])
	}
	t.Log("Flow completed successfully (result=ok)")

	// 4b: Separate SSE events into categories for targeted assertions.
	// The "approval text" event shows pre-substitution args (by design — the
	// AfterToolCallback restores placeholders in session history). This text
	// proves the LLM preserved the placeholder verbatim in its tool call.
	var approvalTexts []string
	var otherTexts []string
	var errorTexts []string
	var allText strings.Builder

	for _, ev := range events {
		allText.WriteString(ev.Data)
		allText.WriteString("\n")

		if ev.Type == "text" {
			var textData struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &textData); err == nil {
				if strings.Contains(textData.Text, "Requesting approval") {
					approvalTexts = append(approvalTexts, textData.Text)
				} else {
					otherTexts = append(otherTexts, textData.Text)
				}
			}
		}
		if ev.Type == "error" {
			errorTexts = append(errorTexts, ev.Data)
		}
	}
	sseOutput := allText.String()

	// 4c: Verify the LLM made a shell_command tool call.
	// If no approval text, the LLM didn't call the tool at all.
	if len(approvalTexts) == 0 {
		t.Errorf("BUG: No tool call was made. The LLM did not call shell_command.\n"+
			"The flow completed but the credential resolution path was never exercised.\n"+
			"SSE output:\n%s", sseOutput)
	}

	// 4d: The approval text SHOULD contain the placeholder (proves LLM cooperated).
	approvalOutput := strings.Join(approvalTexts, "\n")
	if len(approvalTexts) > 0 {
		if strings.Contains(approvalOutput, "{{CREDENTIAL:"+credName+":username}}") {
			t.Log("PASS: LLM correctly preserved {{CREDENTIAL:...}} placeholders in tool args")
		} else if strings.Contains(approvalOutput, "CRED_USER="+credName) || strings.Contains(approvalOutput, `"`+credName+`"`) {
			// The LLM passed the credential NAME directly (the known bug)
			t.Errorf("BUG: LLM passed credential NAME %q directly in tool args instead of preserving the {{CREDENTIAL:...}} placeholder.\n"+
				"This means the credential directive was ignored by the LLM.\n"+
				"Approval text:\n%s", credName, approvalOutput)
		} else {
			t.Logf("NOTE: approval text does not contain expected pattern. Approval:\n%s", approvalOutput)
		}
	}

	// 4e: No error events should indicate credential resolution failure.
	for _, errText := range errorTexts {
		lowerErr := strings.ToLower(errText)
		if strings.Contains(lowerErr, "credential") ||
			strings.Contains(lowerErr, "authentication") ||
			strings.Contains(lowerErr, "unauthorized") {
			t.Errorf("Error event indicates credential resolution failure: %s", errText)
		}
	}

	// 4f: In any LLM response text, the credential NAME must not appear as the value.
	// This catches the case where the LLM uses the name AND the tool somehow doesn't error.
	combinedOutput := strings.Join(otherTexts, "\n")
	if strings.Contains(combinedOutput, "CRED_USER="+credName) {
		t.Errorf("BUG: credential NAME appeared as username value in LLM output.\n"+
			"Output: %s", combinedOutput)
	}
	if strings.Contains(combinedOutput, "CRED_PASS="+credName) {
		t.Errorf("BUG: credential NAME appeared as password value in LLM output.\n"+
			"Output: %s", combinedOutput)
	}

	// 4g: Real credential values must NOT appear in SSE stream (security — redaction)
	if strings.Contains(sseOutput, credUsername) {
		t.Errorf("SECURITY: raw credential username %q leaked in SSE output.\n"+
			"The Redactor should have replaced it with '***'.", credUsername)
	}
	if strings.Contains(sseOutput, credPassword) {
		t.Errorf("SECURITY: raw credential password %q leaked in SSE output.\n"+
			"The Redactor should have replaced it with '***'.", credPassword)
	}

	// Log full SSE output for debugging
	t.Logf("SSE output (%d events, %d bytes):\n%s", len(events), len(sseOutput), sseOutput)
}

// Flow YAML that replicates the complexity of the real OpenStack flow.
// This is the scenario that triggers the bug: the LLM sees a complex shell script
// with {{CREDENTIAL:...}} placeholders mixed among ${VAR} expansions, heredocs,
// awk, jq, and multi-line curl commands. Under this complexity, the LLM tends to
// "resolve" the placeholder itself (passing the credential name) instead of
// preserving it verbatim for BeforeToolCallback to handle.
const flowCredentialComplexYAML = `name: e2e_cred_flow_complex
description: E2E test for credential resolution with complex shell script (OpenStack-style)
nodes:
  - name: get_credential_name
    type: input
    prompt: "Enter credential name:"
    output_model:
      credential_name: string
  - name: authenticate_and_query
    type: llm
    prompt: "Authenticate with the application credential '{credential_name}' and query the service. Execute the script below using shell_command."
    tools: true
    tools_selection:
      - shell_command
    tools_auto_approval: true
    raw_context: |
      Execute EXACTLY this proven script. Do NOT modify the approach or use alternatives.

      APP_CRED_ID="{{CREDENTIAL:{credential_name}:username}}"
      APP_CRED_SECRET="{{CREDENTIAL:{credential_name}:password}}"

      cat > /tmp/auth_payload.json <<EOF
      {
        "auth": {
          "identity": {
            "methods": ["application_credential"],
            "application_credential": {
              "id": "${APP_CRED_ID}",
              "secret": "${APP_CRED_SECRET}"
            }
          }
        }
      }
      EOF

      AUTH_RESPONSE=$(curl -s -X POST "http://localhost:19999/v3/auth/tokens" \
        -H "Content-Type: application/json" \
        -d @/tmp/auth_payload.json)

      TOKEN=$(echo "$AUTH_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))" 2>/dev/null || echo "")

      if [ -z "$TOKEN" ]; then
        echo "ERROR: Authentication failed — invalid credential ID or secret (${APP_CRED_ID} / ${APP_CRED_SECRET})"
        exit 1
      fi

      echo "Token: $(echo $TOKEN | cut -c1-20)..."

      RESULT=$(curl -s -H "X-Auth-Token: $TOKEN" "http://localhost:19999/v2/servers/detail")

      echo "$RESULT" | jq '[.servers[] | {
        id,
        name,
        status,
        flavor: .flavor.original_name,
        image: (.image.id? // "(boot-from-volume)"),
        created,
        updated
      }]' 2>/dev/null || echo "$RESULT"
flow:
  - from: START
    to: get_credential_name
  - from: get_credential_name
    to: authenticate_and_query
  - from: authenticate_and_query
    to: END
`

// TestE2E_FlowRun_CredentialResolution_ComplexScript replicates the real-world
// failure scenario with a complex shell script in raw_context (OpenStack-style).
//
// The key difference from the simple test: the raw_context contains a multi-line
// shell script with heredocs, ${VAR} expansions, curl, jq, and python — all mixed
// with {{CREDENTIAL:...}} placeholders. This complexity causes LLMs to "helpfully"
// resolve the placeholders themselves (passing the credential name as the value)
// instead of preserving them verbatim for BeforeToolCallback.
//
// This test is expected to FAIL until the structural fix is implemented (credential
// resolution that doesn't depend on LLM cooperation).
//
// COVERS: FLOW-CRED-002
func TestE2E_FlowRun_CredentialResolution_ComplexScript(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	client := seed.Client(e2eboot.UserAliceEmail)

	const credName = "e2e-openstack-cred"
	const credUsername = "e2e-application-credential-id-abc123def456"
	const credPassword = "e2e-application-secret-xyz789ghi012"
	const flowName = "e2e_cred_flow_complex"

	// --- Step 1: Create a password credential (simulating OpenStack app credential) ---
	resp := client.Post(t, "/api/credentials?scope=team", map[string]any{
		"name": credName,
		"credential": map[string]any{
			"type":     "password",
			"username": credUsername,
			"password": credPassword,
		},
	})
	credBody := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create credential failed: %d %s", resp.StatusCode, credBody)
	}
	t.Logf("Created credential %q (type=password, simulating OpenStack app credential)", credName)

	t.Cleanup(func() {
		r := client.Delete(t, "/api/credentials/"+credName+"?scope=team")
		r.Body.Close()
	})

	// --- Step 2: Create the complex flow ---
	resp = client.Put(t, "/api/agents/"+flowName, map[string]any{
		"yaml": flowCredentialComplexYAML,
	})
	flowBody := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create flow failed: %d %s", resp.StatusCode, flowBody)
	}
	t.Logf("Created flow %q (complex OpenStack-style script)", flowName)

	t.Cleanup(func() {
		r := client.Delete(t, "/api/agents/"+flowName)
		r.Body.Close()
	})

	// --- Step 3: Execute the flow ---
	events := client.SSE(t, "/api/agents/"+flowName+"/run", map[string]any{
		"params": map[string]string{
			"get_credential_name": credName,
		},
	}, 120*time.Second)

	if len(events) == 0 {
		t.Fatal("no SSE events received from flow execution")
	}

	// --- Step 4: Assertions ---

	// 4a: Flow must complete (we don't require success — the curl will fail because
	// there's no real server, but the tool call itself should happen)
	doneEv := e2eboot.FindEvent(events, "done")
	if doneEv == nil {
		for i, ev := range events {
			t.Logf("  event[%d] type=%q data=%s", i, ev.Type, ev.Data)
		}
		t.Fatal("no 'done' event — flow did not complete")
	}
	t.Log("Flow completed")

	// 4b: Parse events
	var approvalTexts []string
	var allText strings.Builder

	for _, ev := range events {
		allText.WriteString(ev.Data)
		allText.WriteString("\n")

		if ev.Type == "text" {
			var textData struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &textData); err == nil {
				if strings.Contains(textData.Text, "Requesting approval") {
					approvalTexts = append(approvalTexts, textData.Text)
				}
			}
		}
	}
	sseOutput := allText.String()

	// 4c: Verify a tool call was made
	if len(approvalTexts) == 0 {
		t.Fatalf("No tool call was made. The LLM did not call shell_command.\n"+
			"SSE output:\n%s", sseOutput)
	}

	// 4d: THE CRITICAL ASSERTION — did the LLM preserve the placeholder?
	// In the complex script scenario, the LLM tends to "resolve" the placeholder
	// itself, passing the credential name directly instead of the {{CREDENTIAL:...}} token.
	approvalOutput := strings.Join(approvalTexts, "\n")
	t.Logf("Approval text (tool args as sent by LLM):\n%s", approvalOutput)

	placeholderPreserved := strings.Contains(approvalOutput, "{{CREDENTIAL:"+credName+":username}}")
	credNameUsedAsValue := false

	// Check various ways the LLM might pass the credential name directly:
	// 1. APP_CRED_ID="e2e-openstack-cred" (name used as value in assignment)
	// 2. "id": "e2e-openstack-cred" (name used in JSON payload)
	// 3. The credential name appearing where a UUID/long-string would be expected
	if !placeholderPreserved {
		// The placeholder was NOT preserved. Check if the credential name was used directly.
		if strings.Contains(approvalOutput, `"`+credName+`"`) ||
			strings.Contains(approvalOutput, `'`+credName+`'`) ||
			strings.Contains(approvalOutput, "="+credName+`"`) ||
			strings.Contains(approvalOutput, "="+credName+"\\n") ||
			strings.Contains(approvalOutput, "= "+credName) {
			credNameUsedAsValue = true
		}
	}

	if placeholderPreserved {
		t.Log("PASS: LLM correctly preserved {{CREDENTIAL:...}} placeholders in complex script")
	} else if credNameUsedAsValue {
		t.Errorf("BUG (FLOW-CRED-002): LLM passed credential NAME %q directly in tool args.\n"+
			"With the complex shell script (heredocs, ${VAR}, curl, jq), the LLM interpreted\n"+
			"the {{CREDENTIAL:...}} placeholder and substituted the credential name instead of\n"+
			"preserving it verbatim for BeforeToolCallback to resolve.\n\n"+
			"This is the exact bug that causes 'Invalid credential ID/secret' errors in production.\n"+
			"The fix must not depend on LLM cooperation — credentials should be resolved structurally.\n\n"+
			"Approval text:\n%s", credName, approvalOutput)
	} else {
		// LLM did something unexpected — log it for analysis
		t.Errorf("UNEXPECTED: LLM tool call args don't contain the expected placeholder OR the credential name.\n"+
			"This needs manual inspection.\n"+
			"Approval text:\n%s", approvalOutput)
	}

	// 4e: Security — real values must not leak regardless of resolution outcome
	if strings.Contains(sseOutput, credUsername) {
		t.Errorf("SECURITY: raw credential username %q leaked in SSE output", credUsername)
	}
	if strings.Contains(sseOutput, credPassword) {
		t.Errorf("SECURITY: raw credential password %q leaked in SSE output", credPassword)
	}

	// Log full output
	t.Logf("SSE output (%d events, %d bytes):\n%s", len(events), len(sseOutput), sseOutput)
}

// Flow YAML for /api/chat credential resolution testing.
// Hard-codes the credential name to avoid multi-turn input complexity.
// The critical difference from the /api/agents/{name}/run tests above:
// this flow is executed via POST /api/chat (HandleChat), which is the
// actual endpoint used by both the Flow View "Start Execution" button
// and the remote CLI "flows run" command.
const flowCredentialChatYAML = `name: e2e_cred_chat_flow
description: E2E test for credential resolution via /api/chat (HandleChat)
nodes:
  - name: use_credential
    type: llm
    prompt: "Execute the shell command below EXACTLY as written using the shell_command tool. Do not modify it in any way. After execution, report the exact output."
    tools: true
    tools_selection:
      - shell_command
    tools_auto_approval: true
    raw_context: |
      Run this EXACT command using shell_command (copy-paste verbatim, do not modify anything):
      echo "CRED_USER={{CREDENTIAL:e2e-chat-cred:username}} CRED_PASS={{CREDENTIAL:e2e-chat-cred:password}}"
flow:
  - from: START
    to: use_credential
  - from: use_credential
    to: END
`

// TestE2E_FlowRun_CredentialResolution_ChatEndpoint verifies that {{CREDENTIAL:...}}
// placeholders are properly resolved when a flow is executed via POST /api/chat.
//
// This is the ACTUAL endpoint used by:
//   - The Flow View "Start Execution" button (web UI)
//   - The remote CLI "astonish flows run" command
//
// Unlike the tests above (which use POST /api/agents/{name}/run — the FlowRunHandler),
// this test exercises the HandleChat path in run_handler.go. This was the path that was
// MISSING credential store injection into the runner context (fixed in commit bd0406a).
//
// Without the fix, BeforeToolCallback's store.CredentialStoreFromContext(ctx) returned
// nil, the agent-level CredentialStore was also nil (no file-based store in platform
// mode), so the resolver was nil and placeholders were never substituted.
//
// COVERS: FLOW-CRED-003
func TestE2E_FlowRun_CredentialResolution_ChatEndpoint(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	client := seed.Client(e2eboot.UserAliceEmail)

	const credName = "e2e-chat-cred"
	const credUsername = "e2e-chat-app-credential-id-99887"
	const credPassword = "e2e-chat-app-secret-XYZW-abcde"
	const flowName = "e2e_cred_chat_flow"

	// --- Step 1: Create a password credential via the API ---
	resp := client.Post(t, "/api/credentials?scope=team", map[string]any{
		"name": credName,
		"credential": map[string]any{
			"type":     "password",
			"username": credUsername,
			"password": credPassword,
		},
	})
	credBody := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create credential failed: %d %s", resp.StatusCode, credBody)
	}
	t.Logf("Created credential %q (type=password)", credName)

	t.Cleanup(func() {
		r := client.Delete(t, "/api/credentials/"+credName+"?scope=team")
		r.Body.Close()
	})

	// --- Step 2: Create the flow via PUT /api/agents/{name} ---
	resp = client.Put(t, "/api/agents/"+flowName, map[string]any{
		"yaml": flowCredentialChatYAML,
	})
	flowBody := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create flow failed: %d %s", resp.StatusCode, flowBody)
	}
	t.Logf("Created flow %q", flowName)

	t.Cleanup(func() {
		r := client.Delete(t, "/api/agents/"+flowName)
		r.Body.Close()
	})

	// --- Step 3: Execute the flow via POST /api/chat (the HandleChat endpoint) ---
	// This is the SAME endpoint the Flow View UI and remote CLI use.
	sessionID := fmt.Sprintf("e2e-cred-chat-%d", time.Now().UnixNano())
	events := client.SSE(t, "/api/chat", map[string]any{
		"agentId":     flowName,
		"message":     "",
		"sessionId":   sessionID,
		"autoApprove": true,
	}, 120*time.Second)

	if len(events) == 0 {
		t.Fatal("no SSE events received from flow execution via /api/chat")
	}

	// --- Step 4: Assertions ---

	// 4a: Flow must complete successfully
	doneEv := e2eboot.FindEvent(events, "done")
	if doneEv == nil {
		for i, ev := range events {
			t.Logf("  event[%d] type=%q data=%s", i, ev.Type, ev.Data)
		}
		t.Fatal("no 'done' event — flow did not complete via /api/chat")
	}
	t.Log("Flow completed via /api/chat")

	// 4b: Parse events
	var toolCallTexts []string
	var otherTexts []string
	var allText strings.Builder

	for _, ev := range events {
		allText.WriteString(ev.Data)
		allText.WriteString("\n")

		switch ev.Type {
		case "text":
			var textData struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &textData); err == nil {
				if strings.Contains(textData.Text, "shell_command") ||
					strings.Contains(textData.Text, "CREDENTIAL") ||
					strings.Contains(textData.Text, "echo") {
					toolCallTexts = append(toolCallTexts, textData.Text)
				} else {
					otherTexts = append(otherTexts, textData.Text)
				}
			}
		case "tool_call":
			toolCallTexts = append(toolCallTexts, ev.Data)
		case "auto_approved":
			toolCallTexts = append(toolCallTexts, ev.Data)
		}
	}
	sseOutput := allText.String()

	// 4c: Verify a tool call was made
	toolCallMade := len(toolCallTexts) > 0 || strings.Contains(sseOutput, "shell_command")
	if !toolCallMade {
		t.Fatalf("No shell_command tool call detected in /api/chat flow execution.\n"+
			"SSE output:\n%s", sseOutput)
	}
	t.Log("Tool call detected in /api/chat execution")

	// 4d: Credential NAME must not appear as the resolved value in output.
	// If credential resolution fails, the shell would get the literal placeholder
	// or the LLM might pass the name directly. Either way, the output would show
	// "CRED_USER=e2e-chat-cred" instead of "CRED_USER=e2e-chat-app-credential-id-99887".
	combinedOutput := strings.Join(otherTexts, "\n")
	if strings.Contains(combinedOutput, "CRED_USER="+credName) {
		t.Errorf("BUG (FLOW-CRED-003): credential NAME %q appeared as username value in output.\n"+
			"This means credential resolution DID NOT FIRE in the /api/chat path.\n"+
			"Output: %s", credName, combinedOutput)
	}
	if strings.Contains(combinedOutput, "CRED_PASS="+credName) {
		t.Errorf("BUG (FLOW-CRED-003): credential NAME %q appeared as password value in output.\n"+
			"Output: %s", credName, combinedOutput)
	}

	// Also check the full SSE output for the pattern (might be in tool_result events)
	if strings.Contains(sseOutput, "CRED_USER="+credName) && !strings.Contains(sseOutput, "CREDENTIAL:"+credName) {
		t.Errorf("BUG: credential name used as value in SSE output (not in a placeholder context)")
	}

	// 4e: Check that the placeholder {{CREDENTIAL:...}} doesn't appear in tool output
	// (it should have been resolved before execution).
	// NOTE: The approval text legitimately contains placeholders (showing the user
	// what will be run), so we only check post-execution text events — i.e. text
	// events that do NOT start with the approval prefix.
	for _, ev := range events {
		if ev.Type != "text" {
			continue
		}
		var textData struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(ev.Data), &textData); err != nil {
			continue
		}
		// Skip approval text events — these correctly show placeholders
		if strings.HasPrefix(textData.Text, "**Requesting approval") {
			continue
		}
		if strings.Contains(textData.Text, "CRED_USER={{CREDENTIAL:") ||
			strings.Contains(textData.Text, "CRED_PASS={{CREDENTIAL:") {
			t.Errorf("BUG: unresolved {{CREDENTIAL:...}} placeholder appeared in post-execution output.\n"+
				"This means SubstituteShellCommand was called but resolver returned nil/empty.\n"+
				"The credential store was likely not injected into the runner context.\nText: %s", textData.Text)
		}
	}

	// 4f: Security — real credential values must NOT appear in SSE stream
	if strings.Contains(sseOutput, credUsername) {
		t.Errorf("SECURITY: raw credential username %q leaked in SSE output.\n"+
			"The Redactor should have masked it.", credUsername)
	}
	if strings.Contains(sseOutput, credPassword) {
		t.Errorf("SECURITY: raw credential password %q leaked in SSE output.\n"+
			"The Redactor should have masked it.", credPassword)
	}

	// Log full output for debugging
	t.Logf("SSE output from /api/chat (%d events, %d bytes):\n%s", len(events), len(sseOutput), sseOutput)
}
