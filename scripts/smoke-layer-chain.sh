#!/usr/bin/env bash
# smoke-layer-chain.sh — Self-driving cluster smoke test for the sandbox layer-chain pipeline.
#
# Creates a real chat session via the API, waits for the sandbox pod to boot,
# verifies that the expected tools (node, vi) are available based on the
# configured layer chain, then cleans up.
#
# This test validates Scenario 3 (Configured Base + Team) which is the
# most complete chain: @base + configuredTop (node) + teamTop (vi).
# Additional scenarios can be tested by resetting DB state before running.
#
# Prerequisites:
#   - kubectl configured with access to the target cluster
#   - ASTONISH_TEST_TOKEN env var set to a valid Bearer token for the API
#   - ASTONISH_API_URL env var (default: https://devastonish.local.muxpie.com)
#   - ASTONISH_SANDBOX_NS env var (default: astonish-sandbox)
#
# Usage:
#   export ASTONISH_TEST_TOKEN=$(astonish platform issue-token --server $URL)
#   ./scripts/smoke-layer-chain.sh
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

API_URL="${ASTONISH_API_URL:-https://devastonish.local.muxpie.com}"
SANDBOX_NS="${ASTONISH_SANDBOX_NS:-astonish-sandbox}"
TOKEN="${ASTONISH_TEST_TOKEN:-}"
TEAM_SLUG="${ASTONISH_TEST_TEAM:-general}"

if [ -z "$TOKEN" ]; then
  echo "ERROR: ASTONISH_TEST_TOKEN is required." >&2
  echo "Obtain one with: astonish platform issue-token --server $API_URL" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

PASS=0
FAIL=0

pass() { echo "  ✓ $1"; PASS=$((PASS + 1)); }
fail() { echo "  ✗ $1"; FAIL=$((FAIL + 1)); }

api_post() {
  local path="$1" body="$2"
  curl -s -X POST \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -H "X-Astonish-Team: $TEAM_SLUG" \
    -d "$body" \
    "$API_URL$path"
}

api_delete() {
  local path="$1"
  curl -s -X DELETE \
    -H "Authorization: Bearer $TOKEN" \
    -H "X-Astonish-Team: $TEAM_SLUG" \
    "$API_URL$path"
}

# Convert session ID to pod name (mirrors Go's podNameForSession).
# Lowercase, replace non-[a-z0-9-] with '-', trim leading/trailing '-', take first 27 chars.
session_to_pod() {
  local sid="$1"
  local clean
  clean=$(echo "$sid" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9-]/-/g' | sed 's/^-*//;s/-*$//')
  clean="${clean:0:27}"
  # Re-trim trailing dash after truncation
  clean="${clean%-}"
  echo "astn-sess-${clean}"
}

# Wait for a pod to become Running (up to 120s).
wait_pod_running() {
  local pod_name="$1"
  local i=0
  while [ $i -lt 120 ]; do
    local phase
    phase=$(kubectl get pod -n "$SANDBOX_NS" "$pod_name" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    if [ "$phase" = "Running" ]; then
      return 0
    fi
    sleep 3
    i=$((i + 3))
  done
  echo "    TIMEOUT: pod $pod_name did not reach Running within 120s (last phase=$phase)" >&2
  return 1
}

# Get the layer-chain annotation from a pod.
get_chain() {
  kubectl get pod -n "$SANDBOX_NS" "$1" -o jsonpath='{.metadata.annotations.astonish\.io/layer-chain}' 2>/dev/null
}

# Check if a command is available inside the pod's sandbox rootfs (chroot).
has_command() {
  local pod="$1" cmd="$2"
  kubectl exec -n "$SANDBOX_NS" "$pod" -- chroot /sandbox/rootfs sh -c "command -v $cmd" >/dev/null 2>&1
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

echo "=============================================="
echo " Layer-Chain Smoke Test (Self-Driving)"
echo " API: $API_URL"
echo " Namespace: $SANDBOX_NS"
echo " Team: $TEAM_SLUG"
echo "=============================================="
echo ""

# ---------------------------------------------------------------------------
# Step 1: Create a chat session that triggers sandbox pod creation
# ---------------------------------------------------------------------------

echo "--- Step 1: Creating chat session ---"
echo ""

# Send a message that forces a tool call (sandbox container creation).
# We use a simple shell command request. The SSE stream will emit:
#   event: session\ndata: {"sessionId":"<uuid>","isNew":true}
# We capture the stream briefly, extract the session ID, then move on.

SSE_OUTPUT=$(mktemp)
trap "rm -f $SSE_OUTPUT" EXIT

# Fire the chat request. We need to capture enough of the SSE stream to get
# the session ID. Use timeout to avoid hanging forever.
curl -s -N -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "X-Astonish-Team: $TEAM_SLUG" \
  -d '{"message":"Run this shell command and show the output: echo SMOKE_TEST_OK"}' \
  "$API_URL/api/studio/chat" \
  --max-time 30 \
  > "$SSE_OUTPUT" 2>/dev/null || true

# Extract session ID from SSE stream
SESSION_ID=$(grep -A1 '^event: session$' "$SSE_OUTPUT" | grep '^data:' | head -1 | sed 's/^data: //' | python3 -c "import sys,json; print(json.load(sys.stdin)['sessionId'])" 2>/dev/null || echo "")

if [ -z "$SESSION_ID" ]; then
  echo "ERROR: Failed to extract session ID from SSE response." >&2
  echo "SSE output (first 20 lines):" >&2
  head -20 "$SSE_OUTPUT" >&2
  exit 1
fi

echo "  Session ID: $SESSION_ID"

# Derive the pod name
POD_NAME=$(session_to_pod "$SESSION_ID")
echo "  Expected pod: $POD_NAME"
echo ""

# ---------------------------------------------------------------------------
# Step 2: Wait for the sandbox pod to boot
# ---------------------------------------------------------------------------

echo "--- Step 2: Waiting for pod to reach Running ---"
echo ""

if ! wait_pod_running "$POD_NAME"; then
  echo ""
  echo "ERROR: Pod did not start. Checking pod status:" >&2
  kubectl get pod -n "$SANDBOX_NS" "$POD_NAME" -o yaml 2>&1 | tail -30 >&2
  # Attempt cleanup
  api_delete "/api/studio/sessions/$SESSION_ID" >/dev/null 2>&1 || true
  exit 1
fi

echo "  Pod $POD_NAME is Running."
echo ""

# Give the overlay composition a moment to finish (ENTRYPOINT script).
sleep 3

# ---------------------------------------------------------------------------
# Step 3: Verify layer chain annotation
# ---------------------------------------------------------------------------

echo "--- Step 3: Verifying layer chain ---"
echo ""

CHAIN=$(get_chain "$POD_NAME")
echo "  Chain: $CHAIN"

NUM_LAYERS=$(echo "$CHAIN" | tr ',' '\n' | wc -l)
echo "  Layer count: $NUM_LAYERS"
echo ""

# ---------------------------------------------------------------------------
# Step 4: Verify tool availability
# ---------------------------------------------------------------------------

echo "--- Step 4: Verifying tool availability ---"
echo ""

# Determine expected tools based on chain depth.
# 1 layer  (@base only):          no node, no vi   (Scenario 1)
# 2 layers (@base + delta):       varies            (Scenario 2 or 4)
# 3 layers (@base + base + team): node + vi         (Scenario 3)

case $NUM_LAYERS in
  1)
    echo "  Scenario 1: Fresh install (no configured layers)"
    if ! has_command "$POD_NAME" "node"; then
      pass "node is correctly absent (no configured base)"
    else
      fail "node should NOT be present without Configure Base"
    fi
    if ! has_command "$POD_NAME" "vi" && ! has_command "$POD_NAME" "vim"; then
      pass "vi is correctly absent (no team template)"
    else
      fail "vi should NOT be present without team template"
    fi
    ;;
  2)
    echo "  Scenario 2 or 4: Two-layer chain"
    # Detect which scenario by testing for node
    if has_command "$POD_NAME" "node"; then
      echo "  → Scenario 2: Configured base, no team template"
      pass "node is available (Configure Base installed it)"
      if ! has_command "$POD_NAME" "vi" && ! has_command "$POD_NAME" "vim"; then
        pass "vi is correctly absent (no team template)"
      else
        fail "vi should NOT be present without team template"
      fi
    else
      echo "  → Scenario 4: Fresh base + team template"
      pass "node is correctly absent (no Configure Base)"
      if has_command "$POD_NAME" "vi" || has_command "$POD_NAME" "vim"; then
        pass "vi is available (team template installed it)"
      else
        fail "vi should be available from team template"
      fi
    fi
    ;;
  3)
    echo "  Scenario 3: Configured base + team template (full chain)"
    if has_command "$POD_NAME" "node"; then
      pass "node is available (Configure Base installed it)"
    else
      fail "node should be available from Configure Base layer"
    fi
    if has_command "$POD_NAME" "vi" || has_command "$POD_NAME" "vim"; then
      pass "vi is available (team template installed it)"
    else
      fail "vi should be available from team template layer"
    fi
    ;;
  *)
    echo "  Unexpected chain depth: $NUM_LAYERS"
    fail "unexpected layer count $NUM_LAYERS (expected 1-3)"
    ;;
esac

echo ""

# ---------------------------------------------------------------------------
# Step 5: Cleanup
# ---------------------------------------------------------------------------

echo "--- Step 5: Cleanup ---"
echo ""

# Delete the session (this should also trigger pod deletion via the GC reconciler)
api_delete "/api/studio/sessions/$SESSION_ID" >/dev/null 2>&1
echo "  Deleted session $SESSION_ID"

# Also directly delete the pod for immediate cleanup
kubectl delete pod -n "$SANDBOX_NS" "$POD_NAME" --grace-period=5 --ignore-not-found >/dev/null 2>&1 || true
echo "  Deleted pod $POD_NAME"
echo ""

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

echo "=============================================="
echo " Results: $PASS passed, $FAIL failed"
echo "=============================================="

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
