/** Keep in sync with pkg/fleet/config.go CapabilityRegistry and validation enums. */

export const FLEET_AGENT_MODES = ['agentic', 'simple'] as const
export type FleetAgentMode = (typeof FLEET_AGENT_MODES)[number]

/**
 * Domain-neutral capability hints for the editor.
 * Runtime accepts any capability name — use custom tags for domain-specific work
 * (e.g. genetics.analysis, pharmacology.research, code.write).
 */
export const FLEET_CAPABILITY_GROUPS = [
  {
    label: 'Discovery & reasoning',
    options: ['research', 'analysis', 'synthesis'],
  },
  {
    label: 'Planning & orchestration',
    options: ['planning', 'coordination', 'supervisor'],
  },
  {
    label: 'Content lifecycle',
    options: ['writing', 'review', 'editing', 'publishing'],
  },
  {
    label: 'Creation & delivery',
    options: ['design', 'implementation', 'prototyping'],
  },
  {
    label: 'Quality',
    options: ['validation', 'quality-assurance'],
  },
  {
    label: 'Data',
    options: ['data-collection', 'data-processing'],
  },
  {
    label: 'Interaction',
    options: ['customer-facing'],
  },
] as const

/** Flat advisory registry derived from {@link FLEET_CAPABILITY_GROUPS}. */
export const FLEET_CAPABILITY_REGISTRY = FLEET_CAPABILITY_GROUPS.flatMap(group => group.options)

export const FLEET_WORKSPACE_MODES = ['shared', 'isolated', 'none'] as const

export const FLEET_ROUTING_MODES = ['llm_mentions', 'explicit_queue', 'supervisor'] as const

export const FLEET_MEMORY_VISIBILITY = ['scoped', 'shared', 'private_plus_handoffs'] as const

export const FLEET_TASK_CLAIM_POLICIES = ['first_come', 'capability_match', 'supervisor_assigned'] as const

const CAPABILITY_KEY_PATTERN = /^[a-z0-9][a-z0-9._-]*$/

export function normalizeCapabilityKey(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[\s_]+/g, '-')
    .replace(/[^a-z0-9._-]/g, '')
}

export function isValidCapabilityKey(value: string): boolean {
  return CAPABILITY_KEY_PATTERN.test(value)
}

export function enabledCapabilityKeys(capabilities?: Record<string, boolean>): string[] {
  return Object.entries(capabilities || {})
    .filter(([, enabled]) => enabled)
    .map(([key]) => key)
}

export function capabilityMapFromKeys(keys: string[]): Record<string, boolean> {
  const caps: Record<string, boolean> = {}
  for (const key of keys) caps[key] = true
  return caps
}

export function extraCapabilityKeys(
  capabilities: Record<string, boolean> | undefined,
  registry: readonly string[],
): string[] {
  const registrySet = new Set(registry)
  return enabledCapabilityKeys(capabilities).filter(key => !registrySet.has(key))
}
