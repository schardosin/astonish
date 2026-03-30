import { useState, useEffect } from 'react'
import {
  ChevronDown, ChevronRight, Loader, Users,
  Radio, ArrowRight, FileText, Eye,
  Wrench, Clock, AlertCircle, Code,
} from 'lucide-react'
import { fetchFleet } from '../../api/fleetChat'
import type { FleetDefinition } from '../../api/fleetChat'
import type { FleetPlanData, CommFlowNode, FleetAgentDef } from './fleetUtils'
import { getAgentColor } from './fleetUtils'

// ─── Template Detail View ───

interface TemplateDetailProps {
  templateKey: string
  templates: FleetDefinition[]
  onCreatePlan?: (key: string) => void
}

interface TemplateState {
  config: FleetPlanData | null
  key: string | null
  error: string | null
}

export default function TemplateDetail({ templateKey, templates, onCreatePlan }: TemplateDetailProps) {
  const template = templates.find(t => t.key === templateKey)
  // State: { config, key, error } — only set from async callbacks
  const [state, setState] = useState<TemplateState>({ config: null, key: null, error: null })
  const [expandedAgents, setExpandedAgents] = useState<Record<string, boolean>>({})

  // Derive loading: fetching when key doesn't match loaded key
  const loading = Boolean(templateKey && templateKey !== state.key)
  const fullConfig = state.key === templateKey ? state.config : null
  const error = state.key === templateKey ? state.error : null

  // Fetch full template data when templateKey changes
  useEffect(() => {
    if (!templateKey) return
    let cancelled = false
    fetchFleet(templateKey)
      .then(data => {
        if (!cancelled) {
          setState({ config: data.fleet as FleetPlanData, key: templateKey, error: null })
          setExpandedAgents({})
        }
      })
      .catch((err: any) => {
        if (!cancelled) setState({ config: null, key: templateKey, error: err.message })
      })
    return () => { cancelled = true }
  }, [templateKey])

  if (!template) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <p className="text-sm" style={{ color: 'var(--text-muted)' }}>Template not found</p>
      </div>
    )
  }

  const handleCreateWithAI = () => {
    if (onCreatePlan) {
      onCreatePlan(templateKey)
    }
  }

  const toggleAgent = (key: string) => {
    setExpandedAgents(prev => ({ ...prev, [key]: !prev[key] }))
  }

  const commFlow: CommFlowNode[] = fullConfig?.communication?.flow || []
  const agents: [string, FleetAgentDef][] = fullConfig?.agents ? Object.entries(fullConfig.agents) : []
  const settings = fullConfig?.settings || {} as FleetPlanData['settings'] & {}

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-4xl mx-auto p-6 space-y-6">
        {/* Header */}
        <div className="flex items-start justify-between">
          <div>
            <h1 className="text-xl font-bold" style={{ color: 'var(--text-primary)' }}>{template.name}</h1>
            {(fullConfig?.description || template.description) && (
              <p className="text-sm mt-1" style={{ color: 'var(--text-secondary)' }}>{fullConfig?.description || template.description}</p>
            )}
            <div className="flex items-center gap-3 mt-2">
              <span className="text-xs px-2 py-0.5 rounded" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
                Template (read-only)
              </span>
              <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
                {template.agent_count} agent{template.agent_count !== 1 ? 's' : ''}
              </span>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={handleCreateWithAI}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg bg-cyan-600 hover:bg-cyan-500 text-white transition-colors"
            >
              <Users size={12} /> Create Plan with AI Guide
            </button>
          </div>
        </div>

        {/* Loading / Error states for full config */}
        {loading && (
          <div className="flex items-center gap-2 text-xs" style={{ color: 'var(--text-muted)' }}>
            <Loader size={12} className="animate-spin" /> Loading template details...
          </div>
        )}
        {error && (
          <div className="flex items-center gap-2 text-xs" style={{ color: '#f87171' }}>
            <AlertCircle size={12} /> Failed to load details: {error}
          </div>
        )}

        {/* Communication Flow */}
        {commFlow.length > 0 && (
          <div className="rounded-lg p-4" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <h3 className="text-sm font-semibold mb-3" style={{ color: 'var(--text-primary)' }}>Communication Flow</h3>
            <div className="flex items-center flex-wrap gap-2">
              {commFlow.map((node, i) => {
                const color = getAgentColor(node.role)
                return (
                  <div key={node.role} className="flex items-center gap-2">
                    <div
                      className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium"
                      style={{ background: color.bg, border: `1px solid ${color.border}`, color: color.text }}
                    >
                      {node.entry_point && <Radio size={10} />}
                      {node.role}
                    </div>
                    {i < commFlow.length - 1 && (
                      <ArrowRight size={14} style={{ color: 'var(--text-muted)' }} />
                    )}
                  </div>
                )
              })}
            </div>
            <div className="mt-3 space-y-1">
              {commFlow.map(node => (
                <div key={`talks-${node.role}`} className="text-xs" style={{ color: 'var(--text-muted)' }}>
                  <span style={{ color: getAgentColor(node.role).text }}>{node.role}</span>
                  {' talks to: '}
                  {node.talks_to?.join(', ') || 'none'}
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Agents */}
        {agents.length > 0 && (
          <div className="rounded-lg p-4" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <h3 className="text-sm font-semibold mb-3" style={{ color: 'var(--text-primary)' }}>Agents ({agents.length})</h3>
            <div className="space-y-3">
              {agents.map(([key, agent]) => {
                const color = getAgentColor(key)
                const expanded = expandedAgents[key]
                return (
                  <div
                    key={key}
                    className="rounded-lg p-3"
                    style={{ background: color.bg, border: `1px solid ${color.border}` }}
                  >
                    <div
                      className="flex items-center justify-between cursor-pointer"
                      onClick={() => toggleAgent(key)}
                    >
                      <div className="flex items-center gap-2">
                        {expanded ? <ChevronDown size={12} style={{ color: color.text }} /> : <ChevronRight size={12} style={{ color: color.text }} />}
                        <span className="text-sm font-medium" style={{ color: color.text }}>{key}</span>
                      </div>
                      <div className="flex items-center gap-2 text-xs" style={{ color: 'var(--text-muted)' }}>
                        <span>{agent.name}</span>
                        <span>Mode: {agent.mode || 'agentic'}</span>
                      </div>
                    </div>
                    {agent.description && (
                      <p className="text-xs mt-1.5" style={{ color: 'var(--text-secondary)' }}>{agent.description}</p>
                    )}
                    {!expanded && (
                      <>
                        {agent.delegate && (
                          <div className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
                            Delegate: <code className="px-1 py-0.5 rounded text-[11px]" style={{ background: 'rgba(0,0,0,0.3)' }}>{agent.delegate.tool}</code>
                          </div>
                        )}
                        {agent.behaviors && (
                          <div className="text-xs mt-1 line-clamp-2" style={{ color: 'var(--text-muted)' }}>
                            {agent.behaviors.slice(0, 200)}{agent.behaviors.length > 200 ? '...' : ''}
                          </div>
                        )}
                      </>
                    )}
                    {expanded && (
                      <div className="mt-3 space-y-2">
                        {agent.identity && (
                          <div>
                            <div className="text-[11px] font-medium mb-0.5" style={{ color: 'var(--text-muted)' }}>Identity</div>
                            <p className="text-xs" style={{ color: 'var(--text-secondary)' }}>
                              {agent.identity.slice(0, 300)}{agent.identity.length > 300 ? '...' : ''}
                            </p>
                          </div>
                        )}
                        {agent.delegate && (
                          <div>
                            <div className="text-[11px] font-medium mb-0.5" style={{ color: 'var(--text-muted)' }}>Delegate</div>
                            <code className="text-xs px-1 py-0.5 rounded" style={{ background: 'rgba(0,0,0,0.3)', color: 'var(--text-secondary)' }}>
                              {agent.delegate.tool}
                            </code>
                          </div>
                        )}
                        {agent.behaviors && (
                          <div>
                            <div className="text-[11px] font-medium mb-0.5" style={{ color: 'var(--text-muted)' }}>Behaviors</div>
                            <p className="text-xs whitespace-pre-line" style={{ color: 'var(--text-secondary)' }}>{agent.behaviors}</p>
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {/* Fallback: show agent pills when full config hasn't loaded */}
        {agents.length === 0 && !loading && (
          <div className="rounded-lg p-4" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <h3 className="text-sm font-semibold mb-3" style={{ color: 'var(--text-primary)' }}>Agents</h3>
            <div className="flex flex-wrap gap-2">
              {(template.agent_names || []).map(name => {
                const color = getAgentColor(name)
                return (
                  <div
                    key={name}
                    className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium"
                    style={{ background: color.bg, border: `1px solid ${color.border}`, color: color.text }}
                  >
                    {name}
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {/* Settings */}
        {fullConfig && !!(settings.max_turns_per_agent || fullConfig.workspace_base_dir || fullConfig.project_context) && (
          <div className="rounded-lg p-4" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <h3 className="text-sm font-semibold mb-3 flex items-center gap-1.5" style={{ color: 'var(--text-primary)' }}>
              <Wrench size={12} /> Settings
            </h3>
            <div className="space-y-1.5 text-xs" style={{ color: 'var(--text-secondary)' }}>
              {settings.max_turns_per_agent && (
                <div className="flex items-center gap-2">
                  <Clock size={11} style={{ color: 'var(--text-muted)' }} />
                  <span>Max turns per agent: <strong>{String(settings.max_turns_per_agent)}</strong></span>
                </div>
              )}
              {fullConfig.workspace_base_dir && (
                <div className="flex items-center gap-2">
                  <Code size={11} style={{ color: 'var(--text-muted)' }} />
                  <span>Workspace base: <code className="px-1 py-0.5 rounded text-[11px]" style={{ background: 'rgba(0,0,0,0.3)' }}>{fullConfig.workspace_base_dir}</code></span>
                </div>
              )}
              {!!fullConfig.project_context && (
                <div className="flex items-center gap-2">
                  <FileText size={11} style={{ color: 'var(--text-muted)' }} />
                  <span>Project context: <strong>enabled</strong></span>
                </div>
              )}
            </div>
          </div>
        )}

        {/* Info Note */}
        <div className="rounded-lg p-4" style={{ background: 'rgba(6, 182, 212, 0.05)', border: '1px solid rgba(6, 182, 212, 0.2)' }}>
          <p className="text-xs" style={{ color: 'var(--text-secondary)' }}>
            Templates are base fleet configurations. To use a template, create a fleet plan from it.
            Fleet plans add environment-specific settings like communication channels and artifact destinations.
            Use the "Create Plan with AI Guide" button to start an interactive session that helps you configure
            a plan from this template using the <code className="px-1 py-0.5 rounded bg-cyan-500/10 text-cyan-300">/fleet-plan</code> command.
          </p>
        </div>
      </div>
    </div>
  )
}
