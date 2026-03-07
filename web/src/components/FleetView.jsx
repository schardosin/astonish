import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import {
  Rocket, Search, ChevronDown, ChevronRight, Loader, Check, X, Copy,
  Trash2, Play, Square, Radio, ArrowRight, Users, FileText, Eye,
  Wrench, Clock, AlertCircle, ExternalLink, GitBranch, Save,
} from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  fetchFleetPlans, fetchFleets, fetchFleetPlan, fetchFleetPlanYaml, saveFleetPlanYaml,
  activateFleetPlan, deactivateFleetPlan, getFleetPlanStatus, duplicateFleetPlan,
  deleteFleetPlan, startFleetSession, stopFleetSession, fetchFleetTrace,
  fetchFleetSessions, connectFleetStream,
} from '../api/fleetChat'
import { fetchSessions } from '../api/studioChat'
import { buildPath } from '../hooks/useHashRouter'

// Agent identity colors for trace view
const AGENT_COLORS = {
  po: { bg: 'rgba(59, 130, 246, 0.1)', border: 'rgba(59, 130, 246, 0.3)', text: '#60a5fa', label: 'PO' },
  architect: { bg: 'rgba(168, 85, 247, 0.1)', border: 'rgba(168, 85, 247, 0.3)', text: '#c084fc', label: 'Architect' },
  dev: { bg: 'rgba(34, 197, 94, 0.1)', border: 'rgba(34, 197, 94, 0.3)', text: '#4ade80', label: 'Dev' },
  qa: { bg: 'rgba(234, 179, 8, 0.1)', border: 'rgba(234, 179, 8, 0.3)', text: '#facc15', label: 'QA' },
  security: { bg: 'rgba(239, 68, 68, 0.1)', border: 'rgba(239, 68, 68, 0.3)', text: '#f87171', label: 'Security' },
  system: { bg: 'rgba(107, 114, 128, 0.1)', border: 'rgba(107, 114, 128, 0.3)', text: '#9ca3af', label: 'System' },
}

function getAgentColor(name) {
  return AGENT_COLORS[name] || { bg: 'rgba(6, 182, 212, 0.1)', border: 'rgba(6, 182, 212, 0.3)', text: '#22d3ee', label: name }
}

function formatTimeAgo(dateStr) {
  if (!dateStr) return ''
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now - date
  const diffMins = Math.floor(diffMs / 60000)
  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return `${diffMins}m ago`
  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 24) return `${diffHours}h ago`
  const diffDays = Math.floor(diffHours / 24)
  return `${diffDays}d ago`
}

// ─── Fleet Sidebar ───

function FleetSidebar({
  plans, sessions, templates, selectedItem, onSelect, onSearch, searchQuery,
  isLoading, theme,
}) {
  const [collapsedSections, setCollapsedSections] = useState({})

  const toggleSection = (key) => {
    setCollapsedSections(prev => ({ ...prev, [key]: !prev[key] }))
  }

  const renderSection = (title, icon, items, type, badgeColor) => {
    const isCollapsed = collapsedSections[type]
    return (
      <div key={type}>
        <button
          onClick={() => toggleSection(type)}
          className="w-full flex items-center gap-2 px-4 py-2 text-xs font-semibold uppercase tracking-wider hover:bg-white/5 transition-colors"
          style={{ color: 'var(--text-muted)' }}
        >
          {isCollapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
          {icon}
          <span>{title}</span>
          <span className="ml-auto text-[10px] font-normal px-1.5 py-0.5 rounded-full" style={{ background: 'var(--bg-tertiary)' }}>
            {items.length}
          </span>
        </button>
        {!isCollapsed && (
          <div className="pb-1">
            {items.length === 0 ? (
              <div className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>
                No {title.toLowerCase()} found
              </div>
            ) : items.map(item => {
              const isSelected = selectedItem?.type === type && selectedItem?.key === item.key
              return (
                <button
                  key={`${type}-${item.key}`}
                  onClick={() => onSelect({ type, key: item.key })}
                  className={`w-full text-left px-4 py-2.5 transition-colors ${
                    isSelected ? 'bg-cyan-500/15 border-l-2 border-cyan-400' : 'hover:bg-white/5 border-l-2 border-transparent'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    {type === 'plan' && item.activated && (
                      <div className="w-2 h-2 rounded-full bg-green-400 flex-shrink-0" title="Active" />
                    )}
                    <span
                      className="text-sm font-medium truncate flex-1"
                      style={{ color: isSelected ? '#22d3ee' : 'var(--text-primary)' }}
                    >
                      {item.name}
                    </span>
                    {badgeColor && item.badge && (
                      <span className="text-[10px] px-1.5 py-0.5 rounded" style={{ background: badgeColor, color: '#fff' }}>
                        {item.badge}
                      </span>
                    )}
                  </div>
                  {item.subtitle && (
                    <div className="text-xs mt-0.5 truncate" style={{ color: 'var(--text-muted)' }}>
                      {item.subtitle}
                    </div>
                  )}
                </button>
              )
            })}
          </div>
        )}
      </div>
    )
  }

  const planItems = plans.map(p => ({
    key: p.key,
    name: p.name,
    subtitle: `${p.created_from || 'custom'} | ${p.agent_names?.join(', ') || ''}`,
    activated: p.activated,
    badge: p.channel_type !== 'chat' ? p.channel_type : null,
  }))

  const sessionItems = sessions.map(s => ({
    key: s.id,
    name: s.title || `Session ${s.id.slice(0, 8)}`,
    subtitle: `${s.id.slice(0, 8)} | ${s.issueNumber ? `#${s.issueNumber}` : ''} ${formatTimeAgo(s.updatedAt)}`.trim(),
    badge: null,
  }))

  const templateItems = templates.map(t => ({
    key: t.key,
    name: t.name,
    subtitle: `${t.agent_count} agents: ${t.agent_names?.join(', ') || ''}`,
    badge: null,
  }))

  return (
    <div
      className="flex flex-col h-full"
      style={{
        width: '288px',
        minWidth: '288px',
        borderRight: '1px solid var(--border-color)',
        background: theme === 'dark' ? 'rgba(15, 23, 42, 0.5)' : 'var(--bg-secondary)',
      }}
    >
      {/* Search */}
      <div className="px-3 py-3" style={{ borderBottom: '1px solid var(--border-color)' }}>
        <div className="relative">
          <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2" style={{ color: 'var(--text-muted)' }} />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => onSearch(e.target.value)}
            placeholder="Search fleet..."
            className="w-full pl-8 pr-3 py-1.5 text-xs rounded-lg focus:outline-none focus:ring-1 focus:ring-cyan-500"
            style={{
              background: 'var(--bg-tertiary)',
              color: 'var(--text-primary)',
              border: '1px solid var(--border-color)',
            }}
          />
        </div>
      </div>

      {/* Sections */}
      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <div className="flex items-center justify-center py-12">
            <Loader size={18} className="animate-spin text-cyan-400" />
          </div>
        ) : (
          <>
            {renderSection('Fleet Plans', <FileText size={12} />, planItems, 'plan', 'rgba(6, 182, 212, 0.6)')}
            {renderSection('Active Sessions', <Radio size={12} />, sessionItems, 'session', null)}
            {renderSection('Templates', <Eye size={12} />, templateItems, 'template', null)}
          </>
        )}
      </div>
    </div>
  )
}

// ─── Plan Detail View ───

function PlanDetail({ planKey, onNavigate, onRefresh }) {
  const [plan, setPlan] = useState(null)
  const [planStatus, setPlanStatus] = useState(null)
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
  const [error, setError] = useState(null)
  const [saveStatus, setSaveStatus] = useState(null)

  const loadPlan = useCallback(async () => {
    setIsLoading(true)
    setError(null)
    try {
      const [planData, yamlData] = await Promise.all([
        fetchFleetPlan(planKey),
        fetchFleetPlanYaml(planKey),
      ])
      setPlan(planData.plan)
      setYamlContent(yamlData)

      // Load activation status for non-chat plans
      if (planData.plan?.channel?.type && planData.plan.channel.type !== 'chat') {
        const status = await getFleetPlanStatus(planKey).catch(() => null)
        setPlanStatus(status)
      } else {
        setPlanStatus(null)
      }
    } catch (err) {
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
      setPlanStatus(newStatus)
      if (onRefresh) onRefresh()
    } catch (err) {
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
    } catch (err) {
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
    } catch (err) {
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
    } catch (err) {
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
      onNavigate(buildPath('fleet', { subView: 'session', subKey: result.session_id }))
    } catch (err) {
      alert('Launch failed: ' + err.message)
    } finally {
      setIsLaunching(false)
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
  const commFlow = plan.communication?.flow || []
  const artifacts = plan.artifacts ? Object.entries(plan.artifacts) : []

  return (
    <div className="flex-1 overflow-y-auto">
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

        {/* Activation Controls (non-chat plans only) */}
        {planStatus && (
          <div className="rounded-lg p-4" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <div className={`w-3 h-3 rounded-full ${planStatus.activated ? 'bg-green-400 animate-pulse' : 'bg-gray-500'}`} />
                <div>
                  <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                    Channel Monitoring
                  </span>
                  <span className="text-xs ml-2" style={{ color: 'var(--text-muted)' }}>
                    {planStatus.activated ? 'Active' : 'Inactive'}
                  </span>
                </div>
              </div>
              <button
                onClick={handleActivateToggle}
                disabled={isActivating}
                className={`px-4 py-1.5 text-xs font-medium rounded-lg transition-colors ${
                  planStatus.activated
                    ? 'bg-red-600/20 text-red-400 hover:bg-red-600/30'
                    : 'bg-green-600/20 text-green-400 hover:bg-green-600/30'
                } disabled:opacity-50`}
              >
                {isActivating ? 'Working...' : planStatus.activated ? 'Deactivate' : 'Activate'}
              </button>
            </div>
            {planStatus.activated && (
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
            )}
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
              return (
                <div
                  key={key}
                  className="rounded-lg p-3"
                  style={{ background: color.bg, border: `1px solid ${color.border}` }}
                >
                  <div className="flex items-center justify-between mb-1">
                    <span className="text-sm font-medium" style={{ color: color.text }}>{key}</span>
                    <div className="flex items-center gap-2 text-xs" style={{ color: 'var(--text-muted)' }}>
                      <span>Persona: {agent.persona}</span>
                      <span>Mode: {agent.mode || 'agentic'}</span>
                    </div>
                  </div>
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

        {/* YAML Editor */}
        <div className="rounded-lg overflow-hidden" style={{ border: '1px solid var(--border-color)' }}>
          <button
            onClick={() => setShowYaml(!showYaml)}
            className="w-full flex items-center gap-2 px-4 py-3 hover:bg-white/5 transition-colors"
            style={{ background: 'var(--bg-secondary)', color: 'var(--text-primary)' }}
          >
            {showYaml ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            <FileText size={14} className="text-cyan-400" />
            <span className="text-sm font-semibold">YAML Editor</span>
          </button>
          {showYaml && (
            <div style={{ background: 'var(--bg-primary)' }}>
              <textarea
                value={yamlContent}
                onChange={(e) => setYamlContent(e.target.value)}
                className="w-full font-mono text-xs p-4 focus:outline-none resize-none"
                style={{
                  background: 'transparent',
                  color: 'var(--text-primary)',
                  minHeight: '400px',
                  border: 'none',
                }}
                spellCheck={false}
              />
              <div className="flex items-center justify-between px-4 py-2" style={{ borderTop: '1px solid var(--border-color)' }}>
                <div className="text-xs" style={{ color: 'var(--text-muted)' }}>
                  {saveStatus === 'saved' && <span className="text-green-400">Saved successfully</span>}
                  {saveStatus === 'error' && <span className="text-red-400">Save failed</span>}
                </div>
                <button
                  onClick={handleSaveYaml}
                  disabled={isSaving}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg bg-cyan-600 hover:bg-cyan-500 text-white transition-colors disabled:opacity-50"
                >
                  <Save size={12} /> {isSaving ? 'Saving...' : 'Save'}
                </button>
              </div>
            </div>
          )}
        </div>
      </div>

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
                  onChange={(e) => setLaunchMessage(e.target.value)}
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

// ─── Session Execution Trace View ───

function SessionTrace({ sessionId, onRefresh }) {
  const [trace, setTrace] = useState(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState(null)
  const [toolsOnly, setToolsOnly] = useState(false)
  const [expandedEntries, setExpandedEntries] = useState(new Set())
  const [isStopping, setIsStopping] = useState(false)
  const [liveSession, setLiveSession] = useState(null)
  const [liveMessages, setLiveMessages] = useState([])
  const scrollRef = useRef(null)
  const abortRef = useRef(null)
  const pollRef = useRef(null)

  const loadTrace = useCallback(async () => {
    try {
      const data = await fetchFleetTrace(sessionId, { toolsOnly })
      setTrace(data)
      setError(null)
    } catch (err) {
      setError(err.message)
    } finally {
      setIsLoading(false)
    }
  }, [sessionId, toolsOnly])

  // Check if session is active and connect to live stream
  useEffect(() => {
    let cancelled = false
    const checkAndConnect = async () => {
      try {
        const data = await fetchFleetSessions()
        const active = (data.sessions || []).find(s => s.id === sessionId)
        if (active && !cancelled) {
          setLiveSession(active)
          // Connect to SSE stream for live updates
          const controller = connectFleetStream({
            sessionId,
            onEvent: (type, eventData) => {
              if (type === 'fleet_message' || type === 'message') {
                setLiveMessages(prev => [...prev, eventData])
              }
              if (type === 'fleet_state') {
                setLiveSession(prev => prev ? { ...prev, state: eventData.state, active_agent: eventData.active_agent } : prev)
              }
              if (type === 'fleet_done') {
                setLiveSession(prev => prev ? { ...prev, state: 'stopped' } : prev)
              }
            },
            onError: () => {},
            onDone: () => {},
          })
          abortRef.current = controller
        }
      } catch {
        // Session not active, just show trace
      }
    }
    checkAndConnect()
    return () => {
      cancelled = true
      if (abortRef.current) abortRef.current.abort()
    }
  }, [sessionId])

  // Load trace data
  useEffect(() => {
    setIsLoading(true)
    loadTrace()
  }, [loadTrace])

  // Poll trace every 10s for active sessions
  useEffect(() => {
    if (!liveSession || liveSession.state === 'stopped' || liveSession.state === 'completed') return
    pollRef.current = setInterval(loadTrace, 10000)
    return () => { if (pollRef.current) clearInterval(pollRef.current) }
  }, [liveSession, loadTrace])

  // Auto-scroll on new live messages
  useEffect(() => {
    if (scrollRef.current && liveMessages.length > 0) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [liveMessages])

  const handleStop = async () => {
    if (isStopping) return
    setIsStopping(true)
    try {
      await stopFleetSession(sessionId)
      setLiveSession(prev => prev ? { ...prev, state: 'stopped' } : prev)
      if (onRefresh) onRefresh()
    } catch (err) {
      alert('Stop failed: ' + err.message)
    } finally {
      setIsStopping(false)
    }
  }

  const toggleEntry = (index) => {
    setExpandedEntries(prev => {
      const next = new Set(prev)
      if (next.has(index)) next.delete(index)
      else next.add(index)
      return next
    })
  }

  if (isLoading && !trace) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader size={24} className="animate-spin text-cyan-400" />
      </div>
    )
  }

  if (error && !trace) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <AlertCircle size={32} className="mx-auto mb-2 text-red-400" />
          <p className="text-sm" style={{ color: 'var(--text-muted)' }}>{error}</p>
        </div>
      </div>
    )
  }

  const events = trace?.events || []
  const summary = trace?.summary || {}
  const isActive = liveSession && liveSession.state !== 'stopped' && liveSession.state !== 'completed'

  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-3" style={{ borderBottom: '1px solid var(--border-color)' }}>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            {isActive ? (
              <div className="w-2.5 h-2.5 rounded-full bg-green-400 animate-pulse" />
            ) : (
              <div className="w-2.5 h-2.5 rounded-full bg-gray-500" />
            )}
            <span className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>
              Session {sessionId.slice(0, 8)}
            </span>
          </div>
          {liveSession?.active_agent && (
            <span className="text-xs px-2 py-0.5 rounded" style={{ background: getAgentColor(liveSession.active_agent).bg, color: getAgentColor(liveSession.active_agent).text }}>
              @{liveSession.active_agent}
            </span>
          )}
          <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
            {summary.total_events || 0} events | {summary.tool_calls || 0} tool calls | {summary.errors || 0} errors
          </span>
        </div>
        <div className="flex items-center gap-2">
          <label className="flex items-center gap-1.5 text-xs cursor-pointer" style={{ color: 'var(--text-secondary)' }}>
            <input
              type="checkbox"
              checked={toolsOnly}
              onChange={(e) => setToolsOnly(e.target.checked)}
              className="accent-cyan-500"
            />
            Tools only
          </label>
          {isActive && (
            <button
              onClick={handleStop}
              disabled={isStopping}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg bg-red-600/20 text-red-400 hover:bg-red-600/30 transition-colors disabled:opacity-50"
            >
              <Square size={12} /> {isStopping ? 'Stopping...' : 'Stop'}
            </button>
          )}
        </div>
      </div>

      {/* Timeline */}
      <div className="flex-1 overflow-y-auto px-6 py-4 space-y-1" ref={scrollRef}>
        {events.length === 0 && !isActive ? (
          <div className="text-center py-12">
            <FileText size={32} className="mx-auto mb-2" style={{ color: 'var(--text-muted)' }} />
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>No trace events found</p>
          </div>
        ) : (
          events.map((entry, i) => (
            <TraceEntryRow key={i} entry={entry} index={i} expanded={expandedEntries.has(i)} onToggle={toggleEntry} />
          ))
        )}

        {/* Live messages not yet in trace */}
        {liveMessages.length > 0 && (
          <div className="pt-2 border-t border-cyan-500/20 mt-2">
            {liveMessages.map((msg, i) => {
              const color = getAgentColor(msg.sender || 'system')
              return (
                <div key={`live-${i}`} className="flex items-start gap-2 py-1.5 text-xs">
                  <span className="font-medium flex-shrink-0" style={{ color: color.text }}>
                    {msg.sender || 'system'}:
                  </span>
                  <span style={{ color: 'var(--text-secondary)' }}>{msg.text?.slice(0, 200) || ''}</span>
                </div>
              )
            })}
          </div>
        )}

        {isActive && (
          <div className="flex items-center gap-2 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>
            <Loader size={12} className="animate-spin text-cyan-400" />
            <span>Session is active, trace updates every 10s...</span>
          </div>
        )}
      </div>
    </div>
  )
}

// Single trace entry row
function TraceEntryRow({ entry, index, expanded, onToggle }) {
  const time = entry.timestamp ? new Date(entry.timestamp).toLocaleTimeString() : ''
  const sessionLabel = entry.session || ''

  if (entry.type === 'user' || entry.type === 'model' || entry.type === 'thinking') {
    const isUser = entry.type === 'user'
    const isThinking = entry.type === 'thinking'
    const color = sessionLabel ? getAgentColor(sessionLabel) : (isUser ? getAgentColor('system') : { text: '#c084fc', bg: 'rgba(168, 85, 247, 0.05)' })
    const textPreview = entry.text?.length > 300 ? entry.text.slice(0, 300) + '...' : entry.text

    return (
      <div className="py-1">
        <div className="flex items-start gap-2 text-xs">
          <span className="text-[10px] font-mono flex-shrink-0 w-16 text-right" style={{ color: 'var(--text-muted)' }}>{time}</span>
          {sessionLabel && (
            <span className="text-[10px] px-1 py-0.5 rounded flex-shrink-0" style={{ background: color.bg, color: color.text }}>
              {sessionLabel}
            </span>
          )}
          <span className="text-[10px] font-medium flex-shrink-0 w-10" style={{ color: isUser ? '#60a5fa' : isThinking ? '#9ca3af' : color.text }}>
            {isUser ? 'USER' : isThinking ? 'THINK' : 'MODEL'}
          </span>
          <div className="flex-1 min-w-0">
            {expanded ? (
              <div className="rounded p-2 text-xs" style={{ background: 'rgba(255,255,255,0.03)', color: 'var(--text-secondary)' }}>
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{entry.text || ''}</ReactMarkdown>
              </div>
            ) : (
              <button
                onClick={() => onToggle(index)}
                className="text-left w-full hover:underline cursor-pointer"
                style={{ color: 'var(--text-secondary)' }}
              >
                {textPreview}
              </button>
            )}
            {expanded && (
              <button onClick={() => onToggle(index)} className="text-[10px] text-cyan-400 hover:text-cyan-300 mt-1 cursor-pointer">
                Collapse
              </button>
            )}
          </div>
        </div>
      </div>
    )
  }

  if (entry.type === 'tool_call') {
    const argsStr = entry.args ? JSON.stringify(entry.args) : ''
    const argsPreview = argsStr.length > 100 ? argsStr.slice(0, 100) + '...' : argsStr

    return (
      <div className="py-0.5">
        <div className="flex items-start gap-2 text-xs">
          <span className="text-[10px] font-mono flex-shrink-0 w-16 text-right" style={{ color: 'var(--text-muted)' }}>{time}</span>
          {sessionLabel && (
            <span className="text-[10px] px-1 py-0.5 rounded flex-shrink-0" style={{ background: getAgentColor(sessionLabel).bg, color: getAgentColor(sessionLabel).text }}>
              {sessionLabel}
            </span>
          )}
          <Wrench size={10} className="text-purple-400 flex-shrink-0 mt-0.5" />
          <span className="font-medium" style={{ color: '#c084fc' }}>{entry.tool_name}</span>
          {expanded ? (
            <div className="flex-1 min-w-0">
              <pre className="text-[11px] font-mono p-2 rounded whitespace-pre-wrap break-words" style={{ background: 'rgba(0,0,0,0.3)', color: 'var(--text-muted)' }}>
                {JSON.stringify(entry.args, null, 2)}
              </pre>
              <button onClick={() => onToggle(index)} className="text-[10px] text-cyan-400 hover:text-cyan-300 mt-1 cursor-pointer">Collapse</button>
            </div>
          ) : (
            <button
              onClick={() => onToggle(index)}
              className="text-left flex-1 min-w-0 truncate hover:underline cursor-pointer"
              style={{ color: 'var(--text-muted)' }}
            >
              {argsPreview}
            </button>
          )}
        </div>
      </div>
    )
  }

  if (entry.type === 'tool_result') {
    const isError = entry.error
    const durationStr = entry.duration_ms > 0 ? (entry.duration_ms < 1000 ? `${entry.duration_ms}ms` : `${(entry.duration_ms / 1000).toFixed(1)}s`) : ''
    const resultStr = entry.result ? JSON.stringify(entry.result) : ''
    const resultPreview = resultStr.length > 100 ? resultStr.slice(0, 100) + '...' : resultStr

    return (
      <div className="py-0.5">
        <div className="flex items-start gap-2 text-xs">
          <span className="text-[10px] font-mono flex-shrink-0 w-16 text-right" style={{ color: 'var(--text-muted)' }}>{time}</span>
          {sessionLabel && (
            <span className="text-[10px] px-1 py-0.5 rounded flex-shrink-0" style={{ background: getAgentColor(sessionLabel).bg, color: getAgentColor(sessionLabel).text }}>
              {sessionLabel}
            </span>
          )}
          {isError ? (
            <X size={10} className="text-red-400 flex-shrink-0 mt-0.5" />
          ) : (
            <Check size={10} className="text-green-400 flex-shrink-0 mt-0.5" />
          )}
          <span className="font-medium" style={{ color: isError ? '#f87171' : '#4ade80' }}>
            {entry.tool_name || 'result'}
          </span>
          {durationStr && (
            <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>({durationStr})</span>
          )}
          {expanded ? (
            <div className="flex-1 min-w-0">
              <pre className="text-[11px] font-mono p-2 rounded whitespace-pre-wrap break-words" style={{ background: 'rgba(0,0,0,0.3)', color: isError ? '#fca5a5' : 'var(--text-muted)' }}>
                {isError ? entry.error : JSON.stringify(entry.result, null, 2)}
              </pre>
              <button onClick={() => onToggle(index)} className="text-[10px] text-cyan-400 hover:text-cyan-300 mt-1 cursor-pointer">Collapse</button>
            </div>
          ) : (
            <button
              onClick={() => onToggle(index)}
              className="text-left flex-1 min-w-0 truncate hover:underline cursor-pointer"
              style={{ color: isError ? '#fca5a5' : 'var(--text-muted)' }}
            >
              {isError ? entry.error : (resultPreview || 'OK')}
            </button>
          )}
        </div>
      </div>
    )
  }

  return null
}

// ─── Template Detail View ───

function TemplateDetail({ templateKey, templates, onNavigate }) {
  const template = templates.find(t => t.key === templateKey)

  if (!template) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <p className="text-sm" style={{ color: 'var(--text-muted)' }}>Template not found</p>
      </div>
    )
  }

  const handleCreateWithAI = () => {
    // Navigate to chat with /fleet-plan command
    onNavigate(buildPath('chat'))
    // Small delay to let the route change, then we'd need a mechanism to send /fleet-plan
    // For now, just navigate to chat view
  }

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-4xl mx-auto p-6 space-y-6">
        {/* Header */}
        <div className="flex items-start justify-between">
          <div>
            <h1 className="text-xl font-bold" style={{ color: 'var(--text-primary)' }}>{template.name}</h1>
            {template.description && (
              <p className="text-sm mt-1" style={{ color: 'var(--text-secondary)' }}>{template.description}</p>
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

        {/* Agent List */}
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

// ─── Empty State ───

function EmptyState() {
  return (
    <div className="flex-1 flex items-center justify-center">
      <div className="text-center">
        <Rocket size={48} className="mx-auto mb-4 text-cyan-400/30" />
        <h2 className="text-lg font-semibold mb-2" style={{ color: 'var(--text-primary)' }}>Fleet Management</h2>
        <p className="text-sm max-w-md" style={{ color: 'var(--text-muted)' }}>
          Select a fleet plan, session, or template from the sidebar to get started.
          Fleet plans define autonomous agent teams that can be launched manually or
          activated to monitor external channels like GitHub Issues.
        </p>
      </div>
    </div>
  )
}

// ─── Main FleetView Component ───

export default function FleetView({ theme, path, onNavigate }) {
  const [plans, setPlans] = useState([])
  const [templates, setTemplates] = useState([])
  const [sessions, setSessions] = useState([])
  const [isLoading, setIsLoading] = useState(true)
  const [searchQuery, setSearchQuery] = useState('')
  const refreshInterval = useRef(null)

  // Derive selected item from URL path
  const selectedItem = useMemo(() => {
    const subView = path?.params?.subView
    const subKey = path?.params?.subKey
    if (subView && subKey) {
      return { type: subView, key: subKey }
    }
    return null
  }, [path])

  // Load data
  const loadData = useCallback(async () => {
    try {
      const [planData, fleetData, sessionData] = await Promise.all([
        fetchFleetPlans().catch(() => ({ plans: [] })),
        fetchFleets().catch(() => ({ fleets: [] })),
        fetchSessions().catch(() => []),
      ])
      setPlans(planData.plans || [])
      setTemplates(fleetData.fleets || [])
      // Filter sessions to fleet sessions only
      const allSessions = Array.isArray(sessionData) ? sessionData : []
      setSessions(allSessions.filter(s => s.fleetKey))
    } catch (err) {
      console.error('Failed to load fleet data:', err)
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    loadData()
    // Refresh every 30s
    refreshInterval.current = setInterval(loadData, 30000)
    return () => { if (refreshInterval.current) clearInterval(refreshInterval.current) }
  }, [loadData])

  const handleSelect = useCallback((item) => {
    const hashPath = buildPath('fleet', { subView: item.type, subKey: item.key })
    onNavigate('#' + hashPath)
  }, [onNavigate])

  const handleNavigate = useCallback((hashPath) => {
    onNavigate('#' + hashPath)
  }, [onNavigate])

  // Filter items by search query
  const filteredPlans = useMemo(() => {
    if (!searchQuery) return plans
    const q = searchQuery.toLowerCase()
    return plans.filter(p => p.name?.toLowerCase().includes(q) || p.key?.toLowerCase().includes(q))
  }, [plans, searchQuery])

  const filteredSessions = useMemo(() => {
    if (!searchQuery) return sessions
    const q = searchQuery.toLowerCase()
    return sessions.filter(s =>
      s.title?.toLowerCase().includes(q) ||
      s.id?.toLowerCase().includes(q) ||
      s.fleetName?.toLowerCase().includes(q)
    )
  }, [sessions, searchQuery])

  const filteredTemplates = useMemo(() => {
    if (!searchQuery) return templates
    const q = searchQuery.toLowerCase()
    return templates.filter(t => t.name?.toLowerCase().includes(q) || t.key?.toLowerCase().includes(q))
  }, [templates, searchQuery])

  // Render main content based on selection
  const renderContent = () => {
    if (!selectedItem) return <EmptyState />

    switch (selectedItem.type) {
      case 'plan':
        return (
          <PlanDetail
            key={selectedItem.key}
            planKey={selectedItem.key}
            onNavigate={handleNavigate}
            onRefresh={loadData}
          />
        )
      case 'session':
        return (
          <SessionTrace
            key={selectedItem.key}
            sessionId={selectedItem.key}
            onRefresh={loadData}
          />
        )
      case 'template':
        return (
          <TemplateDetail
            key={selectedItem.key}
            templateKey={selectedItem.key}
            templates={templates}
            onNavigate={handleNavigate}
          />
        )
      default:
        return <EmptyState />
    }
  }

  return (
    <div className="flex flex-1 overflow-hidden">
      <FleetSidebar
        plans={filteredPlans}
        sessions={filteredSessions}
        templates={filteredTemplates}
        selectedItem={selectedItem}
        onSelect={handleSelect}
        onSearch={setSearchQuery}
        searchQuery={searchQuery}
        isLoading={isLoading}
        theme={theme}
      />
      <div className="flex-1 flex flex-col overflow-hidden">
        {renderContent()}
      </div>
    </div>
  )
}
