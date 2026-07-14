package fleet

import (
	"strings"
)

// collectActivationTargets decides which agent(s) to activate after a routing
// decision. In serial mode (maxParallel <= 1) this returns at most one agent
// key. In parallel mode, when the message @mentions multiple parallelizable
// agents the sender can talk to, all of them are returned so the dispatcher
// can activate them concurrently.
func collectActivationTargets(msg Message, routing RoutingResult, fleetCfg *FleetConfig) []string {
	if fleetCfg == nil {
		if isAgentTarget(routing.Target) {
			return []string{routing.Target}
		}
		return nil
	}

	maxParallel := fleetCfg.Settings.GetMaxParallelAgents()
	if maxParallel <= 1 {
		if isAgentTarget(routing.Target) {
			return []string{routing.Target}
		}
		return nil
	}

	mentions := msg.Mentions
	if len(mentions) == 0 {
		mentions = ParseMentions(msg.Text)
	}

	var parallel []string
	seen := map[string]bool{}
	for _, m := range mentions {
		m = strings.ToLower(m)
		if m == "customer" || m == msg.Sender || seen[m] {
			continue
		}
		agent, ok := fleetCfg.Agents[m]
		if !ok || !agent.IsParallelizable() {
			continue
		}
		if !fleetCfg.CanTalkTo(msg.Sender, m) {
			continue
		}
		seen[m] = true
		parallel = append(parallel, m)
	}
	if len(parallel) >= 2 {
		return parallel
	}

	if isAgentTarget(routing.Target) {
		return []string{routing.Target}
	}
	return nil
}

func isAgentTarget(target string) bool {
	switch target {
	case "", "customer", "self", "none":
		return false
	default:
		return true
	}
}

// partitionPending splits pending targets into a serial head (non-parallelizable
// or when only one slot is available) and a parallel batch.
func partitionPending(pending []string, fleetCfg *FleetConfig, maxSlots int) (serial string, parallel []string, rest []string) {
	if len(pending) == 0 {
		return "", nil, nil
	}
	if maxSlots <= 1 || fleetCfg == nil {
		return pending[0], nil, pending[1:]
	}

	first := pending[0]
	agent, ok := fleetCfg.Agents[first]
	if !ok || !agent.IsParallelizable() {
		return first, nil, pending[1:]
	}

	batch := []string{first}
	rest = nil
	for _, key := range pending[1:] {
		a, ok := fleetCfg.Agents[key]
		if ok && a.IsParallelizable() && len(batch) < maxSlots {
			batch = append(batch, key)
			continue
		}
		rest = append(rest, key)
	}
	if len(batch) == 1 {
		return batch[0], nil, rest
	}
	return "", batch, rest
}
