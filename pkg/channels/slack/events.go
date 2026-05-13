package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

// EventsHTTPHandler returns an http.Handler that receives Slack Events API
// webhooks. It verifies the request signature, handles URL verification
// challenges, and dispatches events to the adapter's event processing logic.
//
// This handler should be mounted at POST /slack/events on the daemon's HTTP server.
func (s *SlackChannel) EventsHTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		// Verify request signature (if signing secret is configured)
		if s.config.SigningSecret != "" {
			sv, svErr := slack.NewSecretsVerifier(r.Header, s.config.SigningSecret)
			if svErr != nil {
				http.Error(w, "verification failed", http.StatusUnauthorized)
				return
			}
			if _, svErr = sv.Write(body); svErr != nil {
				http.Error(w, "verification failed", http.StatusUnauthorized)
				return
			}
			if svErr = sv.Ensure(); svErr != nil {
				s.logger.Printf("[slack] Request signature verification failed: %v", svErr)
				http.Error(w, "verification failed", http.StatusUnauthorized)
				return
			}
		}

		// Parse the outer event
		eventsAPIEvent, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
		if err != nil {
			s.logger.Printf("[slack] Failed to parse event: %v", err)
			http.Error(w, "failed to parse event", http.StatusBadRequest)
			return
		}

		// Handle URL verification challenge (Slack sends this during app setup)
		if eventsAPIEvent.Type == slackevents.URLVerification {
			var challenge slackevents.ChallengeResponse
			if err := json.Unmarshal(body, &challenge); err != nil {
				http.Error(w, "challenge parse error", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(challenge.Challenge))
			return
		}

		// Respond immediately (Slack requires 200 within 3 seconds)
		w.WriteHeader(http.StatusOK)

		// Process the event asynchronously
		go s.handleEventsAPIEvent(context.Background(), eventsAPIEvent, eventsAPIEvent.TeamID)
	})
}
