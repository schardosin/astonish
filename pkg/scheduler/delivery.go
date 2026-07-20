package scheduler

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/SAP/astonish/pkg/channels"
)

// maxDeliveryLen is the maximum length of a delivery message.
// Longer results are truncated with an indicator.
const maxDeliveryLen = 3800

// ChannelManagerGetter is a function that returns the current ChannelManager.
// This indirection avoids stale closure captures: the delivery func always
// resolves the live manager at delivery time, even after channel reloads.
type ChannelManagerGetter func() *channels.ChannelManager

// DeliveryTarget is a resolved target for job output delivery.
type DeliveryTarget struct {
	ChannelID string // adapter ID (e.g., "telegram")
	ChatID    string // chat/conversation ID on that channel
}

// DeliveryResolver resolves job delivery modes into concrete channel targets.
// It bridges the scheduler (which knows about users/teams) with the channel
// system (which knows about external chat IDs).
type DeliveryResolver interface {
	// ResolveUserChannels returns all active channel targets for a given user ID.
	// Used for "owner" mode and per-member resolution in "members"/"team" modes.
	ResolveUserChannels(ctx context.Context, userID string) ([]DeliveryTarget, error)

	// ResolveTeamMembers returns all user IDs that belong to the given org/team.
	// Used for "team" mode delivery.
	ResolveTeamMembers(ctx context.Context, orgSlug, teamSlug string) ([]string, error)
}

// DeliveryContext provides org/team context for delivery resolution.
// In platform mode, the multi-tenant scheduler passes this from the
// iteration context. In personal mode it is nil (uses legacy routing).
type DeliveryContext struct {
	OrgSlug  string
	TeamSlug string
}

// deliveryContextKey is the context key for DeliveryContext.
type deliveryContextKey struct{}

// WithDeliveryContext attaches a DeliveryContext to the given context.
func WithDeliveryContext(ctx context.Context, dc *DeliveryContext) context.Context {
	return context.WithValue(ctx, deliveryContextKey{}, dc)
}

// GetDeliveryContext retrieves the DeliveryContext from the context, if set.
func GetDeliveryContext(ctx context.Context) *DeliveryContext {
	dc, _ := ctx.Value(deliveryContextKey{}).(*DeliveryContext)
	return dc
}

// NewDeliverFunc creates a DeliverFunc that delivers job results via channels.
//
// Delivery routing priority:
//  1. Mode "owner"   → deliver to job owner's linked channels
//  2. Mode "team"    → deliver to all team members' linked channels
//  3. Mode "members" → deliver to specified member IDs' linked channels
//  4. Mode "target"  → deliver to Channel+Target directly
//  5. Mode ""        → legacy: Channel+Target if set, otherwise broadcast
//
// The getManager function is called at delivery time to obtain the current
// ChannelManager, ensuring the delivery func survives channel reloads.
func NewDeliverFunc(getManager ChannelManagerGetter) DeliverFunc {
	return func(ctx context.Context, job *Job, result string, execErr error) error {
		mgr := getManager()
		if mgr == nil {
			return fmt.Errorf("no channel manager available")
		}

		msg := formatDeliveryMessage(job, result, execErr)

		outMsg := channels.OutboundMessage{
			Text:   msg,
			Format: channels.FormatHTML,
		}

		// Targeted delivery: if the job specifies a channel and target, send
		// directly to that target instead of broadcasting to everyone.
		if job.Delivery.Channel != "" && job.Delivery.Target != "" {
			target := channels.Target{
				ChannelID: job.Delivery.Channel,
				ChatID:    job.Delivery.Target,
			}
			return mgr.Send(ctx, target, outMsg)
		}

		// Fallback: broadcast to all targets across all channels.
		return broadcastOrError(ctx, mgr, outMsg)
	}
}

// NewPlatformDeliverFunc creates a DeliverFunc with full platform delivery
// resolution support. It uses the resolver to map delivery modes to targets.
func NewPlatformDeliverFunc(getManager ChannelManagerGetter, resolver DeliveryResolver, logger *log.Logger) DeliverFunc {
	return func(ctx context.Context, job *Job, result string, execErr error) error {
		mgr := getManager()
		if mgr == nil {
			return fmt.Errorf("no channel manager available")
		}

		msg := formatDeliveryMessage(job, result, execErr)

		outMsg := channels.OutboundMessage{
			Text:   msg,
			Format: channels.FormatHTML,
		}

		// Resolve delivery targets based on mode
		targets, err := resolveTargets(ctx, job, resolver)
		if err != nil {
			if logger != nil {
				logger.Printf("[delivery] Resolution failed for job %q (mode=%s): %v — falling back to broadcast",
					job.Name, job.Delivery.Mode, err)
			}
			// Fallback to broadcast on resolution failure
			return broadcastOrError(ctx, mgr, outMsg)
		}

		// If resolution returned explicit targets, send to each
		if len(targets) > 0 {
			var errs []string
			for _, t := range targets {
				target := channels.Target{
					ChannelID: t.ChannelID,
					ChatID:    t.ChatID,
				}
				if sendErr := mgr.Send(ctx, target, outMsg); sendErr != nil {
					errs = append(errs, fmt.Sprintf("%s/%s: %v", t.ChannelID, t.ChatID, sendErr))
				}
			}
			if len(errs) > 0 {
				return fmt.Errorf("partial delivery failure (%d/%d): %s",
					len(errs), len(targets), strings.Join(errs, "; "))
			}
			if logger != nil {
				logger.Printf("[delivery] Delivered job %q to %d target(s)", job.Name, len(targets))
			}
			return nil
		}

		// No targets resolved and no error — broadcast (legacy personal mode)
		return broadcastOrError(ctx, mgr, outMsg)
	}
}

// broadcastOrError broadcasts and fails if no channel targets exist (silent no-op).
func broadcastOrError(ctx context.Context, mgr *channels.ChannelManager, msg channels.OutboundMessage) error {
	if mgr.CountBroadcastTargets() == 0 {
		return fmt.Errorf("no delivery targets available")
	}
	return mgr.Broadcast(ctx, msg)
}

// resolveTargets determines concrete delivery targets for a job based on its
// delivery mode and the available resolver.
//
// Team scoping enforcement: In platform mode (when DeliveryContext is present),
// delivery is restricted to users who are members of the team that owns the job.
// This prevents cross-team information leakage via scheduler delivery.
func resolveTargets(ctx context.Context, job *Job, resolver DeliveryResolver) ([]DeliveryTarget, error) {
	mode := job.Delivery.Mode

	// Legacy direct target mode
	if mode == DeliveryModeTarget || (mode == "" && job.Delivery.Channel != "" && job.Delivery.Target != "") {
		return []DeliveryTarget{{
			ChannelID: job.Delivery.Channel,
			ChatID:    job.Delivery.Target,
		}}, nil
	}

	// Modes that require a resolver
	if resolver == nil {
		// No resolver available — return nil (will broadcast)
		return nil, nil
	}

	switch mode {
	case DeliveryModeOwner:
		if job.OwnerID == "" {
			return nil, fmt.Errorf("owner delivery mode but job has no owner_id")
		}
		// Validate owner is a member of the team (defense-in-depth)
		if err := validateTeamMembership(ctx, resolver, []string{job.OwnerID}); err != nil {
			return nil, fmt.Errorf("owner team membership check failed: %w", err)
		}
		return resolveMultipleUsersFiltered(ctx, resolver, []string{job.OwnerID}, &job.Delivery)

	case DeliveryModeTeam:
		dctx := GetDeliveryContext(ctx)
		if dctx == nil || dctx.OrgSlug == "" || dctx.TeamSlug == "" {
			return nil, fmt.Errorf("team delivery mode but no org/team context available")
		}
		memberIDs, err := resolver.ResolveTeamMembers(ctx, dctx.OrgSlug, dctx.TeamSlug)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve team members: %w", err)
		}
		return resolveMultipleUsersFiltered(ctx, resolver, memberIDs, &job.Delivery)

	case DeliveryModeMembers:
		if len(job.Delivery.MemberIDs) == 0 {
			return nil, fmt.Errorf("members delivery mode but no member_ids specified")
		}
		// Validate all specified member IDs are actually members of the team
		if err := validateTeamMembership(ctx, resolver, job.Delivery.MemberIDs); err != nil {
			return nil, fmt.Errorf("members team membership check failed: %w", err)
		}
		return resolveMultipleUsersFiltered(ctx, resolver, job.Delivery.MemberIDs, &job.Delivery)

	default:
		// Unknown mode or empty — no targets (will broadcast)
		return nil, nil
	}
}

// validateTeamMembership checks that all given user IDs are members of the team
// indicated by the DeliveryContext. If no DeliveryContext is set (personal mode),
// validation is skipped. Returns an error listing non-member IDs if any fail.
func validateTeamMembership(ctx context.Context, resolver DeliveryResolver, userIDs []string) error {
	dctx := GetDeliveryContext(ctx)
	if dctx == nil || dctx.OrgSlug == "" || dctx.TeamSlug == "" {
		// No team context (personal mode) — skip validation
		return nil
	}

	teamMembers, err := resolver.ResolveTeamMembers(ctx, dctx.OrgSlug, dctx.TeamSlug)
	if err != nil {
		return fmt.Errorf("failed to resolve team roster: %w", err)
	}

	memberSet := make(map[string]struct{}, len(teamMembers))
	for _, m := range teamMembers {
		memberSet[m] = struct{}{}
	}

	var nonMembers []string
	for _, uid := range userIDs {
		if _, ok := memberSet[uid]; !ok {
			nonMembers = append(nonMembers, uid)
		}
	}

	if len(nonMembers) > 0 {
		return fmt.Errorf("user(s) %s are not members of team %s/%s",
			strings.Join(nonMembers, ", "), dctx.OrgSlug, dctx.TeamSlug)
	}
	return nil
}

// resolveMultipleUsersFiltered resolves channels for multiple user IDs, applies
// channel filtering from the delivery config, and deduplicates results.
//
// Channel filtering priority (per user):
//  1. If delivery.MemberChannels[userID] is set → only those channel types
//  2. Else if delivery.ChannelFilter is set → only those channel types
//  3. Else → all linked channels (no filtering)
func resolveMultipleUsersFiltered(ctx context.Context, resolver DeliveryResolver, userIDs []string, delivery *JobDelivery) ([]DeliveryTarget, error) {
	seen := make(map[string]bool)
	var targets []DeliveryTarget

	// Build global filter set (if any)
	var globalFilter map[string]bool
	if delivery != nil && len(delivery.ChannelFilter) > 0 {
		globalFilter = make(map[string]bool, len(delivery.ChannelFilter))
		for _, ch := range delivery.ChannelFilter {
			globalFilter[ch] = true
		}
	}

	for _, uid := range userIDs {
		userTargets, err := resolver.ResolveUserChannels(ctx, uid)
		if err != nil {
			// Skip users with resolution errors (they may have no linked channels)
			continue
		}

		// Determine channel filter for this user
		var userFilter map[string]bool
		if delivery != nil && delivery.MemberChannels != nil {
			if perMember, ok := delivery.MemberChannels[uid]; ok && len(perMember) > 0 {
				userFilter = make(map[string]bool, len(perMember))
				for _, ch := range perMember {
					userFilter[ch] = true
				}
			}
		}
		// Fall back to global filter if no per-member override
		if userFilter == nil {
			userFilter = globalFilter
		}

		for _, t := range userTargets {
			// Apply channel filter
			if userFilter != nil && !userFilter[t.ChannelID] {
				continue
			}
			key := t.ChannelID + ":" + t.ChatID
			if !seen[key] {
				seen[key] = true
				targets = append(targets, t)
			}
		}
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("no delivery targets resolved for %d users", len(userIDs))
	}
	return targets, nil
}

// formatDeliveryMessage creates a human-friendly delivery message.
func formatDeliveryMessage(job *Job, result string, execErr error) string {
	var b strings.Builder

	// Header
	modeLabel := "Routine"
	if job.Mode == ModeAdaptive {
		modeLabel = "Adaptive"
	}

	if execErr != nil {
		b.WriteString(fmt.Sprintf("**Scheduled Job Failed: %s** (%s)\n\n", job.Name, modeLabel))
		b.WriteString(fmt.Sprintf("**Error:** %s\n", execErr.Error()))

		if result != "" {
			b.WriteString(fmt.Sprintf("\n**Partial Output:**\n%s", truncateResult(result)))
		}

		b.WriteString(fmt.Sprintf("\n\n_Failures: %d consecutive_", job.ConsecutiveFailures+1))
	} else {
		b.WriteString(fmt.Sprintf("**Scheduled Job: %s** (%s)\n\n", job.Name, modeLabel))
		b.WriteString(truncateResult(result))
	}

	b.WriteString(fmt.Sprintf("\n\n_%s_", time.Now().Format("Jan 2, 2006 3:04 PM")))

	return b.String()
}

// truncateResult shortens a result string if it exceeds maxDeliveryLen.
func truncateResult(s string) string {
	if len(s) <= maxDeliveryLen {
		return s
	}
	return s[:maxDeliveryLen] + "\n\n... (truncated)"
}
