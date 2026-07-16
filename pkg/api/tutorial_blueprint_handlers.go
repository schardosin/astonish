package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/SAP/astonish/pkg/agent"
	"github.com/SAP/astonish/pkg/tools"
	"google.golang.org/adk/session"
)

// handleTutorialBlueprintIntent processes Approve / Cancel / Request-changes for
// a pending tutorial blueprint.
//
// Returns handled=true when the HTTP/SSE request is fully finished (cancel,
// errors). On successful Approve it returns handled=false and a rewriteMsg so
// the chat handler can fall through into ChatRunner with the converted drill.
func handleTutorialBlueprintIntent(
	r *http.Request,
	w http.ResponseWriter,
	flusher http.Flusher,
	chatAgent *agent.ChatAgent,
	sessionService session.Service,
	userID, sessionID, msg string,
) (handled bool, rewriteMsg string) {
	trimmed := strings.TrimSpace(msg)

	switch {
	case trimmed == "__tutorial_blueprint_approve__":
		pending := chatAgent.GetPendingTutorialBlueprint(sessionID)
		if pending == nil {
			SendSSE(w, flusher, "error", map[string]interface{}{"error": "No pending tutorial blueprint"})
			SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
			return true, ""
		}
		result, err := tools.BlueprintToTutorialDrillFromYAML(pending.YAML, "")
		chatAgent.CancelPendingTutorialBlueprint(sessionID)
		if err != nil {
			errText := fmt.Sprintf("Failed to convert blueprint: %v", err)
			SendSSE(w, flusher, "text", map[string]interface{}{"text": errText})
			persistSessionMessage(r.Context(), sessionService, userID, sessionID, "model", errText)
			SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
			return true, ""
		}
		payload := map[string]any{
			"title":          pending.Title,
			"suite":          pending.Suite,
			"blueprint_yaml": result.BlueprintYAML,
			"drill_yaml":     result.DrillYAML,
			"drill_name":     result.DrillName,
			"message": result.Message + "\n\nNext: refine screen-step selectors, then validate_drill and save_drill. " +
				"Avatar/broll rows remain in the blueprint for a later avatar provider step.",
		}
		SendSSE(w, flusher, "tutorial_blueprint_approved", payload)
		persistTutorialBlueprintApproved(r.Context(), sessionService, userID, sessionID, payload)

		// Fall through into ChatRunner: one user turn carries the drill YAML +
		// continue instructions (no giant model dump, no early done).
		rewriteMsg = fmt.Sprintf(
			"Approve & generate\n\n"+
				"Blueprint approved. Generated tutorial drill %q (%d screen scene(s)).\n"+
				"Replace browser_run_code TODOs with real UI actions (browser_highlight + "+
				"browser_click with animate_cursor:true; prefer nav clicks over navigate). "+
				"Keep warm-up open_app / enter_fullscreen unrecorded. Then validate_drill "+
				"and save_drill into suite %q.\n\n```yaml\n%s\n```",
			result.DrillName, result.ScreenCount, pending.Suite, result.DrillYAML,
		)
		return false, rewriteMsg

	case trimmed == "__tutorial_blueprint_cancel__":
		persistSessionMessage(r.Context(), sessionService, userID, sessionID, "user", "Cancel blueprint")
		chatAgent.CancelPendingTutorialBlueprint(sessionID)
		responseText := "Tutorial blueprint review cancelled."
		SendSSE(w, flusher, "text", map[string]interface{}{"text": responseText})
		persistSessionMessage(r.Context(), sessionService, userID, sessionID, "model", responseText)
		SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
		return true, ""

	case trimmed == "__tutorial_blueprint_revise__":
		// Fall through to the agent with a clear revise prompt (keep pending).
		return false, ""

	default:
		// Natural language while a blueprint is pending: treat as revise feedback
		// unless it looks like an unrelated new request — keep pending and let agent run.
		return false, ""
	}
}
