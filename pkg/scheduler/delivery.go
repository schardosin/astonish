package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/channels"
)

// maxDeliveryLen is the maximum length of a delivery message.
// Longer results are truncated with an indicator.
const maxDeliveryLen = 3800

// NewDeliverFunc creates a DeliverFunc that broadcasts job results to all
// active channel targets. For Telegram, this means every allowed user.
func NewDeliverFunc(channelMgr *channels.ChannelManager) DeliverFunc {
	return func(ctx context.Context, job *Job, result string, execErr error) error {
		if channelMgr == nil {
			return fmt.Errorf("no channel manager available")
		}

		msg := formatDeliveryMessage(job, result, execErr)

		outMsg := channels.OutboundMessage{
			Text:   msg,
			Format: channels.FormatHTML,
		}

		return channelMgr.Broadcast(ctx, outMsg)
	}
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
