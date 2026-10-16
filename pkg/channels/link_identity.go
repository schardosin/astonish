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
// Slack users preselect only their member ID (U…/W…). Workspace is taken from
// Slack's authenticated event when the code is redeemed. A stored T…:U…/W…
// principal remains valid.
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
		if !slackMemberID.MatchString(i.UserID) {
			break
		}
		if i.WorkspaceID == "" {
			return i.UserID, nil
		}
		if slackWorkspaceID.MatchString(i.WorkspaceID) {
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

// ParseLinkPrincipal accepts Slack member IDs (U…/W…) and T…:member principals.
func ParseLinkPrincipal(channel LinkChannel, principal string) (LinkIdentity, error) {
	identity := LinkIdentity{UserID: principal}
	if channel == LinkSlack {
		parts := strings.Split(principal, ":")
		switch len(parts) {
		case 1:
			identity = LinkIdentity{UserID: parts[0]}
		case 2:
			identity = LinkIdentity{WorkspaceID: parts[0], UserID: parts[1]}
		default:
			return LinkIdentity{}, fmt.Errorf("valid expected %s identity is required", channel)
		}
	}
	_, err := identity.Principal(channel)
	return identity, err
}

// MatchesLinkBinding rejects unbound codes, wrong channels and wrong senders.
// A member-only Slack binding matches the same member from any workspace; a
// workspace-qualified binding requires that workspace.
func MatchesLinkBinding(binding string, channel LinkChannel, principal string) bool {
	presented, err := ParseLinkPrincipal(channel, principal)
	if err != nil {
		return false
	}
	prefix := "link:v1:" + string(channel) + ":"
	if !strings.HasPrefix(binding, prefix) {
		return false
	}
	bound, err := ParseLinkPrincipal(channel, strings.TrimPrefix(binding, prefix))
	if err != nil {
		return false
	}
	if bound.UserID != presented.UserID {
		return false
	}
	if bound.WorkspaceID != "" && bound.WorkspaceID != presented.WorkspaceID {
		return false
	}
	return true
}
