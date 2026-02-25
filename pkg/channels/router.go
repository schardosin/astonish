package channels

import "fmt"

// RouteResult contains the routing decision for an inbound message.
type RouteResult struct {
	// SessionKey is the persistent session identifier for this conversation.
	// Format: <channel>:<chatType>:<chatID>
	SessionKey string
}

// Router determines which agent/session handles an inbound message.
// Phase 1 uses a trivial router: all messages go to the default ChatAgent,
// with session keys derived from the message metadata.
type Router struct{}

// NewRouter creates a new message router.
func NewRouter() *Router {
	return &Router{}
}

// Route determines the session key for an inbound message.
// In Phase 1, all messages are routed to the default ChatAgent.
func (r *Router) Route(msg InboundMessage) RouteResult {
	return RouteResult{
		SessionKey: fmt.Sprintf("%s:%s:%s", msg.ChannelID, msg.ChatType, msg.ChatID),
	}
}
