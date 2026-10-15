package channels

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// LinkChannel names the channels that support sender-bound linking.
type LinkChannel string

const (
	LinkTelegram LinkChannel = "telegram"
	LinkSlack    LinkChannel = "slack"
	LinkEmail    LinkChannel = "email"
)

// LinkIdentity is selected by the authenticated owner BEFORE a code is issued.
// Slack identities always include workspace: a member ID alone is not a principal.
type LinkIdentity struct {
	UserID      string `json:"user_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

var slackWorkspaceID = regexp.MustCompile(`^T[A-Z0-9]{2,63}$`)
var slackMemberID = regexp.MustCompile(`^[UW][A-Z0-9]{2,63}$`)

func (i LinkIdentity) Principal(channel LinkChannel) (string, error) {
	switch channel {
	case LinkTelegram:
		id, err := strconv.ParseInt(i.UserID, 10, 64)
		if err == nil && id > 0 && strconv.FormatInt(id, 10) == i.UserID && i.WorkspaceID == "" {
			return i.UserID, nil
		}
	case LinkSlack:
		if slackWorkspaceID.MatchString(i.WorkspaceID) && slackMemberID.MatchString(i.UserID) {
			return i.WorkspaceID + ":" + i.UserID, nil
		}
	}
	return "", fmt.Errorf("valid expected %s identity is required", channel)
}

// LinkBinding encodes a validated binding in the existing pending channel column.
func LinkBinding(channel LinkChannel, identity LinkIdentity) (string, error) {
	principal, err := identity.Principal(channel)
	if err != nil {
		return "", err
	}
	return "link:v1:" + string(channel) + ":" + principal, nil
}

// ParseLinkPrincipal rejects unqualified Slack IDs and noncanonical Telegram IDs.
func ParseLinkPrincipal(channel LinkChannel, principal string) (LinkIdentity, error) {
	identity := LinkIdentity{UserID: principal}
	if channel == LinkSlack {
		parts := strings.Split(principal, ":")
		if len(parts) != 2 {
			return LinkIdentity{}, fmt.Errorf("workspace-qualified Slack identity required")
		}
		identity = LinkIdentity{WorkspaceID: parts[0], UserID: parts[1]}
	}
	_, err := identity.Principal(channel)
	return identity, err
}

// MatchesLinkBinding rejects legacy unbound codes, wrong channels and wrong senders.
func MatchesLinkBinding(binding string, channel LinkChannel, principal string) bool {
	identity, err := ParseLinkPrincipal(channel, principal)
	if err != nil {
		return false
	}
	expected, err := LinkBinding(channel, identity)
	return err == nil && binding == expected
}
