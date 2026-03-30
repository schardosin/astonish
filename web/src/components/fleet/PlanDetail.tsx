import { useState, useEffect, useCallback } from 'react'
import {
  Rocket, ChevronDown, ChevronRight, Loader, Copy,
  Trash2, Play, Radio, ArrowRight, Code,
  AlertCircle, GitBranch, RotateCcw,
} from 'lucide-react'
import YamlDrawer from '../YamlDrawer'
import {
  fetchFleetPlan, fetchFleetPlanYaml, saveFleetPlanYaml,
  activateFleetPlan, deactivateFleetPlan, getFleetPlanStatus, duplicateFleetPlan,
  deleteFleetPlan, startFleetSession, retryFleetIssue,
} from '../../api/fleetChat'
import { buildPath } from '../../hooks/useHashRouter'
import type { FleetPlanData, FleetPlanStatusExt, CommFlowNode } from './fleetUtils'
import { getAgentColor, formatTimeAgo } from './fleetUtils'

// ─── Plan Detail View ───

interface PlanDetailProps {
  planKey: string
  onNavigate: (path: string) => void
  onRefresh?: () => void
  theme: string
}

export default function PlanDetail({ planKey, onNavigate, onRefresh, theme }: PlanDetailProps) {
  const [plan, setPlan] = useState<FleetPlanData | null>(null)
  const [planStatus, setPlanStatus] = useState<FleetPlanStatusExt | null>(null)
  const [yamlContent, setYamlContent] = useState('')
  const [showYaml, setShowYaml] = useState(false)
  const [isLoading, setIsLoading] = useState(true)
  const [isSaving, setIsSaving] = useState(false)
  const [isActivating, setIsActivating] = useState(false)
  const [isDuplicating, setIsDuplicating] = useState(false)
  const [isDeleting, setIsDeleting] = useState(false)
  const [isLaunching, setIsLaunching] = useState(false)
  const [launchMessage, setLaunchMessage] = useState('')
  const [showLaunchDialog, setShowLaunchDialog] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [statusError, setStatusError] = useState<string | null>(null)
  const [saveStatus, setSaveStatus] = useState<'saved' | 'error' | null>(null)
  const [retryingIssue, setRetryingIssue] = useState<number | null>(null)
  const [expandedAgents, setExpandedAgents] = useState<Record<string, boolean>>({})

  const toggleAgent = (key: string) => {
    setExpandedAgents(prev => ({ ...prev, [key]: !prev[key] }))
  }

  const loadPlan = useCallback(async () => {
    setIsLoading(true)
    setError(null)
    try {
      const [planData, yamlData] = await Promise.all([
        fetchFleetPlan(planKey),
        fetchFleetPlanYaml(planKey),
      ])
      setPlan(planData.plan as FleetPlanData)
      setYamlContent(yamlData)

      // Load activation status for non-chat plans
      const planObj = planData.plan as FleetPlanData
      if (planObj?.channel?.type && planObj.channel.type !== 'chat') {
        try {
          const status = await getFleetPlanStatus(planKey)
          setPlanStatus(status as FleetPlanStatusExt)
          setStatusError(null)
        } catch (statusErr: any) {
          setPlanStatus(null)
          setStatusError(statusErr.message || 'Failed to load activation status')
        }
      } else {
        setPlanStatus(null)
        setStatusError(null)
      }
    } catch (err: any) {
      setError(err.message)
    } finally {
      setIsLoading(false)
    }
  }, [planKey])

  useEffect(() => { loadPlan() }, [loadPlan])

  const handleActivateToggle = async () => {
    if (isActivating) return
    setIsActivating(true)
    try {
      if (planStatus?.activated) {
        await deactivateFleetPlan(planKey)
      } else {
        await activateFleetPlan(planKey)
      }
      const newStatus = await getFleetPlanStatus(planKey)
      setPlanStatus(newStatus as FleetPlanStatusExt)
      if (onRefresh) onRefresh()
    } catch (err: any) {
      alert('Activation failed: ' + err.message)
    } finally {
      setIsActivating(false)
    }
  }

  const handleSaveYaml = async () => {
    if (isSaving) return
    setIsSaving(true)
    setSaveStatus(null)
    try {
      await saveFleetPlanYaml(planKey, yamlContent)
      setSaveStatus('saved')
      await loadPlan()
      if (onRefresh) onRefresh()
      setTimeout(() => setSaveStatus(null), 2000)
    } catch (err: any) {
      setSaveStatus('error')
      alert('Save failed: ' + err.message)
    } finally {
      setIsSaving(false)
    }
  }

  const handleDuplicate = async () => {
    if (isDuplicating) return
    setIsDuplicating(true)
    try {
      const result = await duplicateFleetPlan(planKey)
      if (onRefresh) onRefresh()
      onNavigate(buildPath('fleet', { subView: 'plan', subKey: result.key }))
    } catch (err: any) {
      alert('Duplicate failed: ' + err.message)
    } finally {
      setIsDuplicating(false)
    }
  }

  const handleDelete = async () => {
    if (isDeleting) return
    if (!window.confirm(`Delete fleet plan "${plan?.name || planKey}"? This cannot be undone.`)) return
    setIsDeleting(true)
    try {
      await deleteFleetPlan(planKey)
      if (onRefresh) onRefresh()
      onNavigate(buildPath('fleet'))
    } catch (err: any) {
      alert('Delete failed: ' + err.message)
    } finally {
      setIsDeleting(false)
    }
  }

  const handleLaunch = async () => {
    if (isLaunching) return
    setIsLaunching(true)
    try {
      const result = await startFleetSession({ planKey, message: launchMessage })
      setShowLaunchDialog(false)
      setLaunchMessage('')
      if (onRefresh) onRefresh()
      // Chat plans are interactive — open in StudioChat where the user can
      // type messages. Non-chat plans go to the read-only session trace.
      const isChat = plan?.channel?.type === 'chat' || !plan?.channel?.type
      if (isChat) {
        onNavigate(buildPath('chat', { sessionId: result.session_id }))
      } else {
        onNavigate(buildPath('fleet', { subView: 'session', subKey: result.session_id }))
      }
    } catch (err: any) {
      alert('Launch failed: ' + err.message)
    } finally {
      setIsLaunching(false)
    }
  }

  const handleRetryIssue = async (issueNumber: number) => {
    if (retryingIssue) return
    setRetryingIssue(issueNumber)
    try {
      const result = await retryFleetIssue(planKey, issueNumber)
      // Refresh status to update the failed issues list
      const newStatus = await getFleetPlanStatus(planKey)
      setPlanStatus(newStatus as FleetPlanStatusExt)
      if (onRefresh) onRefresh()
      // Navigate to the recovering session
      if (result.session_id) {
        onNavigate(buildPath('fleet', { subView: 'session', subKey: result.session_id }))
      }
    } catch (err: any) {
      alert('Retry failed: ' + err.message)
    } finally {
      setRetryingIssue(null)
    }
  }

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader size={24} className="animate-spin text-cyan-400" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <AlertCircle size={32} className="mx-auto mb-2 text-red-400" />
          <p className="text-sm" style={{ color: 'var(--text-muted)' }}>{error}</p>
        </div>
      </div>
    )
  }

  if (!plan) return null

  const agents = plan.agents ? Object.entries(plan.agents) : []
  const commFlow: CommFlowNode[] = plan.communication?.flow || []
  const artifacts = plan.artifacts ? Object.entries(plan.artifacts) : []

  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      {/* Visual content area - shrinks to half when source is open */}
      <div className={`${showYaml ? 'h-1/2' : 'flex-1'} overflow-y-auto`}>
        <div className="max-w-4xl mx-auto p-6 space-y-6">
          {/* Header */}
        <div className="flex items-start justify-between">
          <div>
            <h1 className="text-xl font-bold" style={{ color: 'var(--text-primary)' }}>{plan.name}</h1>
            {plan.description && (
              <p className="text-sm mt-1" style={{ color: 'var(--text-secondary)' }}>{plan.description}</p>
            )}
            <div className="flex items-center gap-3 mt-2">
              {plan.created_from && (
                <span className="text-xs px-2 py-0.5 rounded" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
                  Base: {plan.created_from}
                </span>
              )}
              <span className="text-xs px-2 py-0.5 rounded" style={{ background: 'rgba(6, 182, 212, 0.15)', color: '#22d3ee' }}>
                {plan.channel?.type || 'chat'}
              </span>
              <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
                {agents.length} agent{agents.length !== 1 ? 's' : ''}
              </span>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setShowYaml(!showYaml)}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg transition-colors"
              style={{ background: showYaml ? 'rgba(6, 182, 212, 0.15)' : 'var(--bg-tertiary)', color: showYaml ? '#22d3ee' : 'var(--text-secondary)' }}
            >
              <Code size={12} />
              {showYaml ? 'Hide Source' : 'View Source'}
            </button>
            <button
              onClick={() => setShowLaunchDialog(true)}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg bg-cyan-600 hover:bg-cyan-500 text-white transition-colors"
            >
              <Play size={12} /> Launch
            </button>
            <button
              onClick={handleDuplicate}
              disabled={isDuplicating}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg hover:bg-white/10 transition-colors disabled:opacity-50"
              style={{ color: 'var(--text-secondary)', border: '1px solid var(--border-color)' }}
            >
              <Copy size={12} /> {isDuplicating ? 'Duplicating...' : 'Duplicate'}
            </button>
            <button
              onClick={handleDelete}
              disabled={isDeleting}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg hover:bg-red-500/20 transition-colors disabled:opacity-50 text-red-400"
              style={{ border: '1px solid rgba(239, 68, 68, 0.3)' }}
            >
              <Trash2 size={12} /> {isDeleting ? 'Deleting...' : 'Delete'}
            </button>
          </div>
        </div>

        {/* Activation Controls (non-chat plans) */}
        {plan.channel?.type && plan.channel.type !== 'chat' && (
          statusError ? (
            /* Status fetch failed — show error with retry */
            <div className="rounded-lg p-4" style={{ background: 'rgba(239, 68, 68, 0.08)', border: '1px solid rgba(239, 68, 68, 0.25)' }}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <AlertCircle size={16} className="text-red-400 flex-shrink-0" />
                  <div>
                    <span className="text-sm font-medium text-red-400">
                      Could not load activation status
                    </span>
                    <p className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
                      {statusError}
                    </p>
                  </div>
                </div>
                <button
                  onClick={loadPlan}
                  className="px-3 py-1.5 text-xs font-medium rounded-lg transition-colors"
                  style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}
                >
                  Retry
                </button>
              </div>
            </div>
          ) : planStatus?.activated ? (
            /* Activated — show green status with deactivate option */
            <div className="rounded-lg p-4" style={{ background: 'rgba(34, 197, 94, 0.08)', border: '1px solid rgba(34, 197, 94, 0.25)' }}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <div className="w-3 h-3 rounded-full bg-green-400 animate-pulse" />
                  <div>
                    <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                      Channel Monitoring
                    </span>
                    <span className="text-xs ml-2 text-green-400">Active</span>
                  </div>
                </div>
                <button
                  onClick={handleActivateToggle}
                  disabled={isActivating}
                  className="px-4 py-1.5 text-xs font-medium rounded-lg bg-red-600/20 text-red-400 hover:bg-red-600/30 transition-colors disabled:opacity-50"
                >
                  {isActivating ? 'Working...' : 'Deactivate'}
                </button>
              </div>
              <div className="mt-3 grid grid-cols-3 gap-4 text-xs" style={{ color: 'var(--text-muted)' }}>
                <div>
                  <div className="font-medium mb-0.5" style={{ color: 'var(--text-secondary)' }}>Last Poll</div>
                  {planStatus.last_poll_at ? formatTimeAgo(planStatus.last_poll_at) : 'Never'}
                  {planStatus.last_poll_status && ` (${planStatus.last_poll_status})`}
                </div>
                <div>
                  <div className="font-medium mb-0.5" style={{ color: 'var(--text-secondary)' }}>Sessions Started</div>
                  {planStatus.sessions_started || 0}
                </div>
                <div>
                  <div className="font-medium mb-0.5" style={{ color: 'var(--text-secondary)' }}>Schedule</div>
                  {plan.channel?.schedule || 'Default'}
                </div>
                {planStatus.last_poll_error && (
                  <div className="col-span-3 text-red-400">
                    Error: {planStatus.last_poll_error}
                  </div>
                )}
              </div>
            </div>
          ) : (
            /* Not activated — prominent call to action */
            <div className="rounded-lg p-4" style={{ background: 'rgba(234, 179, 8, 0.08)', border: '1px solid rgba(234, 179, 8, 0.3)' }}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <div className="w-3 h-3 rounded-full bg-yellow-500" />
                  <div>
                    <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                      Not Activated
                    </span>
                    <p className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
                      This plan monitors <strong className="text-yellow-400">{plan.channel.type!.replace('_', ' ')}</strong> but
                      is not yet active. Activate to start polling{plan.channel?.schedule ? ` on schedule (${plan.channel.schedule})` : ''}.
                    </p>
                  </div>
                </div>
                <button
                  onClick={handleActivateToggle}
                  disabled={isActivating}
                  className="px-5 py-2 text-sm font-semibold rounded-lg bg-green-600 hover:bg-green-500 text-white transition-colors disabled:opacity-50"
                >
                  {isActivating ? 'Activating...' : 'Activate'}
                </button>
              </div>
            </div>
          )
        )}

        {/* Failed Sessions */}
        {planStatus?.failed_issues && planStatus.failed_issues.length > 0 && (
          <div className="rounded-lg p-4" style={{ background: 'rgba(239, 68, 68, 0.06)', border: '1px solid rgba(239, 68, 68, 0.2)' }}>
            <div className="flex items-center gap-2 mb-3">
              <AlertCircle size={16} className="text-red-400" />
              <h3 className="text-sm font-semibold text-red-400">
                Failed Sessions ({planStatus.failed_issues.length})
              </h3>
            </div>
            <div className="space-y-2">
              {planStatus.failed_issues.map(issue => (
                <div
                  key={issue.issue_number}
                  className="rounded-lg p-3 flex items-start justify-between gap-3"
                  style={{ background: 'rgba(0, 0, 0, 0.15)', border: '1px solid rgba(239, 68, 68, 0.15)' }}
                >
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                        Issue #{issue.issue_number}
                      </span>
                      {issue.session_id && (
                        <button
                          onClick={() => onNavigate(buildPath('fleet', { subView: 'session', subKey: issue.session_id! }))}
                          className="text-[10px] px-1.5 py-0.5 rounded hover:bg-white/10 transition-colors cursor-pointer"
                          style={{ color: '#22d3ee', background: 'rgba(6, 182, 212, 0.1)' }}
                        >
                          trace {issue.session_id.slice(0, 8)}
                        </button>
                      )}
                      {issue.failed_at && (
                        <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
                          {formatTimeAgo(issue.failed_at)}
                        </span>
                      )}
                    </div>
                    <p className="text-xs truncate" style={{ color: 'var(--text-muted)' }}>
                      {issue.error || 'Unknown error'}
                    </p>
                  </div>
                  <button
                    onClick={() => handleRetryIssue(issue.issue_number)}
                    disabled={retryingIssue === issue.issue_number}
                    className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg bg-amber-600/20 text-amber-400 hover:bg-amber-600/30 transition-colors disabled:opacity-50 flex-shrink-0"
                  >
                    <RotateCcw size={12} className={retryingIssue === issue.issue_number ? 'animate-spin' : ''} />
                    {retryingIssue === issue.issue_number ? 'Retrying...' : 'Continue'}
                  </button>
                </div>
              ))}
            </div>
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

        {/* Artifacts */}
        {artifacts.length > 0 && (
          <div className="rounded-lg p-4" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <h3 className="text-sm font-semibold mb-3" style={{ color: 'var(--text-primary)' }}>Artifacts</h3>
            <div className="space-y-2">
              {artifacts.map(([key, artifact]) => (
                <div key={key} className="flex items-center gap-3 text-xs">
                  <GitBranch size={14} className="text-cyan-400" />
                  <span className="font-medium" style={{ color: 'var(--text-secondary)' }}>{key}</span>
                  <span style={{ color: 'var(--text-muted)' }}>
                    {artifact.type === 'git_repo' ? artifact.repo : artifact.path}
                  </span>
                  {artifact.auto_pr && (
                    <span className="px-1.5 py-0.5 rounded text-[10px] bg-purple-500/20 text-purple-300">auto-PR</span>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Channel Config */}
        {plan.channel?.config && Object.keys(plan.channel.config).length > 0 && (
          <div className="rounded-lg p-4" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <h3 className="text-sm font-semibold mb-3" style={{ color: 'var(--text-primary)' }}>Channel Configuration</h3>
            <div className="space-y-1">
              {Object.entries(plan.channel.config).map(([k, v]) => (
                <div key={k} className="flex items-center gap-2 text-xs">
                  <span className="font-medium" style={{ color: 'var(--text-secondary)' }}>{k}:</span>
                  <span style={{ color: 'var(--text-muted)' }}>{typeof v === 'object' ? JSON.stringify(v) : String(v)}</span>
                </div>
              ))}
            </div>
          </div>
        )}
        </div>
      </div>

      {/* Bottom Panel - YAML Source Drawer */}
      {showYaml && (
        <div className="h-1/2" style={{ borderTop: '1px solid var(--border-color)' }}>
          <YamlDrawer
            content={yamlContent}
            onChange={setYamlContent}
            onClose={() => setShowYaml(false)}
            theme={theme as 'dark' | 'light'}
            subtitle="Fleet plan configuration"
            onSave={handleSaveYaml}
            isSaving={isSaving}
            saveStatus={saveStatus}
          />
        </div>
      )}

      {/* Launch Dialog */}
      {showLaunchDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
          <div className="w-full max-w-md rounded-xl shadow-2xl" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <div className="px-6 py-4" style={{ borderBottom: '1px solid var(--border-color)' }}>
              <h2 className="text-lg font-semibold flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
                <Rocket size={20} className="text-cyan-400" />
                Launch Fleet Session
              </h2>
              <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
                Launch "{plan.name}" with an optional initial task
              </p>
            </div>
            <div className="px-6 py-4 space-y-4">
              <div>
                <label className="block text-xs font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Initial request (optional)</label>
                <textarea
                  value={launchMessage}
                  onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) => setLaunchMessage(e.target.value)}
                  placeholder="Describe what you want the team to work on..."
                  rows={3}
                  className="w-full px-3 py-2 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-cyan-500 resize-none"
                  style={{ background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }}
                />
              </div>
              <div className="flex justify-end gap-2 pt-2">
                <button
                  onClick={() => { setShowLaunchDialog(false); setLaunchMessage('') }}
                  className="px-4 py-2 text-sm rounded-lg hover:bg-white/5 transition-colors"
                  style={{ color: 'var(--text-secondary)' }}
                >
                  Cancel
                </button>
                <button
                  onClick={handleLaunch}
                  disabled={isLaunching}
                  className="px-4 py-2 text-sm bg-cyan-600 hover:bg-cyan-500 text-white rounded-lg transition-colors disabled:opacity-50"
                >
                  {isLaunching ? 'Launching...' : 'Launch'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
