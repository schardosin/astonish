import { useState, useEffect, useCallback, useMemo } from 'react'
import {
  Crosshair, Search, ChevronDown, ChevronRight, Loader, Check, X,
  Trash2, Play, Clock, AlertCircle, Tag, ArrowRight,
  Plus, ChevronLeft, Zap, Shield, Server, Terminal, CheckCircle2,
  XCircle, MinusCircle, BarChart3, ListChecks, Code,
} from 'lucide-react'
import {
  fetchDrillSuites, fetchDrillSuite, fetchDrill, deleteDrillSuite, deleteDrill,
  fetchDrillYaml, saveDrillYaml, fetchSuiteYaml, saveSuiteYaml,
} from '../api/drillApi'
import { buildPath } from '../hooks/useHashRouter'
import YamlDrawer from './YamlDrawer'

// ─── Helpers ───

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

function formatDuration(ms) {
  if (!ms) return '—'
  if (ms < 1000) return `${ms}ms`
  const secs = (ms / 1000).toFixed(1)
  if (secs < 60) return `${secs}s`
  const mins = Math.floor(ms / 60000)
  const remSecs = Math.floor((ms % 60000) / 1000)
  return `${mins}m ${remSecs}s`
}

function statusColor(status) {
  switch (status) {
    case 'passed': return { dot: '#22c55e', bg: 'rgba(34, 197, 94, 0.1)', border: 'rgba(34, 197, 94, 0.3)', text: '#4ade80' }
    case 'failed': return { dot: '#ef4444', bg: 'rgba(239, 68, 68, 0.1)', border: 'rgba(239, 68, 68, 0.3)', text: '#f87171' }
    case 'error':  return { dot: '#f59e0b', bg: 'rgba(245, 158, 11, 0.1)', border: 'rgba(245, 158, 11, 0.3)', text: '#fbbf24' }
    default:       return { dot: '#6b7280', bg: 'rgba(107, 114, 128, 0.1)', border: 'rgba(107, 114, 128, 0.3)', text: '#9ca3af' }
  }
}

function StatusDot({ status, size = 8 }) {
  const color = statusColor(status)
  return (
    <span
      className="inline-block rounded-full flex-shrink-0"
      style={{ width: size, height: size, background: color.dot }}
      title={status || 'unknown'}
    />
  )
}

function StatusBadge({ status }) {
  const color = statusColor(status)
  return (
    <span
      className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium"
      style={{ background: color.bg, border: `1px solid ${color.border}`, color: color.text }}
    >
      {status === 'passed' && <CheckCircle2 size={10} />}
      {status === 'failed' && <XCircle size={10} />}
      {status === 'error' && <AlertCircle size={10} />}
      {!['passed', 'failed', 'error'].includes(status) && <MinusCircle size={10} />}
      {status || 'unknown'}
    </span>
  )
}


// ─── Drill Sidebar ───

function DrillSidebar({ suites, selectedItem, onSelect, onSearch, searchQuery, isLoading }) {
  const [collapsedSections, setCollapsedSections] = useState({})

  const toggleSection = (key) => {
    setCollapsedSections(prev => ({ ...prev, [key]: !prev[key] }))
  }

  const renderSuiteItem = (suite) => {
    const isSelected = selectedItem?.type === 'suite' && selectedItem?.key === suite.name
    const sc = statusColor(suite.last_status)
    return (
      <button
        key={suite.name}
        onClick={() => onSelect({ type: 'suite', key: suite.name })}
        className={`w-full text-left px-4 py-2.5 transition-colors group ${
          isSelected ? 'border-l-2' : 'border-l-2 border-transparent hover:bg-white/5'
        }`}
        style={isSelected ? {
          background: 'rgba(245, 158, 11, 0.08)',
          borderLeftColor: '#f59e0b',
        } : {}}
      >
        <div className="flex items-center gap-2">
          <StatusDot status={suite.last_status} />
          <span className="text-xs font-medium truncate flex-1" style={{ color: isSelected ? '#fbbf24' : 'var(--text-primary)' }}>
            {suite.name}
          </span>
          <span className="text-[10px] px-1.5 py-0.5 rounded-full" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
            {suite.drill_count}
          </span>
        </div>
        {suite.last_summary && (
          <div className="mt-0.5 text-[10px] pl-4" style={{ color: sc.text }}>
            {suite.last_summary}
          </div>
        )}
      </button>
    )
  }

  const renderSection = (title, icon, items, type, renderItem) => {
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
            ) : items.map(renderItem)}
          </div>
        )}
      </div>
    )
  }

  return (
    <div
      className="flex flex-col border-r overflow-hidden"
      style={{ width: 288, minWidth: 288, borderColor: 'var(--border-color)', background: 'var(--bg-primary)' }}
    >
      {/* Search */}
      <div className="p-3 border-b" style={{ borderColor: 'var(--border-color)' }}>
        <div className="relative">
          <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2" style={{ color: 'var(--text-muted)' }} />
          <input
            type="text"
            placeholder="Search drills..."
            value={searchQuery}
            onChange={(e) => onSearch(e.target.value)}
            className="w-full pl-8 pr-3 py-1.5 text-xs rounded-lg border focus:outline-none focus:ring-1"
            style={{
              background: 'var(--bg-secondary)',
              borderColor: 'var(--border-color)',
              color: 'var(--text-primary)',
              '--tw-ring-color': '#f59e0b',
            }}
          />
        </div>
      </div>

      {/* Sections */}
      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <div className="flex items-center justify-center py-8">
            <Loader size={20} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
          </div>
        ) : (
          renderSection('Drill Suites', <ListChecks size={12} />, suites, 'suites', renderSuiteItem)
        )}
      </div>
    </div>
  )
}


// ─── Suite Detail ───

function SuiteDetail({ suiteKey, onNavigate, onRunSuite, onAddDrills, onRefresh, theme }) {
  const [suite, setSuite] = useState(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState(null)
  const [confirmDelete, setConfirmDelete] = useState(null) // null | 'suite' | drillName
  const [activeTab, setActiveTab] = useState('drills')
  const [expandedDrills, setExpandedDrills] = useState({})
  const [showYaml, setShowYaml] = useState(false)
  const [yamlContent, setYamlContent] = useState('')
  const [isSaving, setIsSaving] = useState(false)
  const [saveStatus, setSaveStatus] = useState(null)

  const loadSuite = useCallback(async () => {
    try {
      const [suiteData, yamlData] = await Promise.all([
        fetchDrillSuite(suiteKey),
        fetchSuiteYaml(suiteKey),
      ])
      setSuite(suiteData)
      setYamlContent(yamlData)
    } catch (err) {
      setError(err.message)
    } finally {
      setIsLoading(false)
    }
  }, [suiteKey])

  useEffect(() => {
    setIsLoading(true)
    setError(null)
    loadSuite()
  }, [loadSuite])

  const handleDeleteSuite = async () => {
    try {
      await deleteDrillSuite(suiteKey)
      setConfirmDelete(null)
      if (onRefresh) onRefresh()
      onNavigate('#' + buildPath('drill'))
    } catch (err) {
      setError(err.message)
    }
  }

  const handleDeleteDrill = async (drillName) => {
    try {
      await deleteDrill(suiteKey, drillName)
      setConfirmDelete(null)
      const data = await fetchDrillSuite(suiteKey)
      setSuite(data)
      if (onRefresh) onRefresh()
    } catch (err) {
      setError(err.message)
    }
  }

  const toggleReportDrill = (name) => {
    setExpandedDrills(prev => ({ ...prev, [name]: !prev[name] }))
  }

  const handleSaveYaml = async () => {
    if (isSaving) return
    setIsSaving(true)
    setSaveStatus(null)
    try {
      await saveSuiteYaml(suiteKey, yamlContent)
      setSaveStatus('saved')
      // Re-fetch suite JSON to sync the visual view with saved YAML
      const suiteData = await fetchDrillSuite(suiteKey)
      setSuite(suiteData)
      if (onRefresh) onRefresh()
      setTimeout(() => setSaveStatus(null), 2000)
    } catch (err) {
      setSaveStatus('error')
      setError(err.message)
    } finally {
      setIsSaving(false)
    }
  }

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader size={24} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <AlertCircle size={32} className="mx-auto mb-2" style={{ color: '#ef4444' }} />
          <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>{error}</p>
        </div>
      </div>
    )
  }

  if (!suite) return null

  const report = suite.last_report
  const reportTests = report?.tests || []

  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      {/* Header */}
      <div className="p-6 pb-0">
        <div className="flex items-start justify-between mb-4">
          <div>
            <h1 className="text-xl font-bold mb-1" style={{ color: 'var(--text-primary)' }}>{suite.name}</h1>
            {suite.description && (
              <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>{suite.description}</p>
            )}
            <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>{suite.file}</p>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => onRunSuite(suiteKey)}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium text-white transition-colors hover:opacity-90"
              style={{ background: 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)' }}
            >
              <Play size={12} /> Run
            </button>
            <button
              onClick={() => onAddDrills(suiteKey)}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors hover:bg-white/10"
              style={{ border: '1px solid var(--border-color)', color: 'var(--text-secondary)' }}
            >
              <Plus size={12} /> Add Drills
            </button>
            <button
              onClick={() => setConfirmDelete('suite')}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors hover:bg-red-500/10"
              style={{ border: '1px solid var(--border-color)', color: 'var(--text-secondary)' }}
            >
              <Trash2 size={12} />
            </button>
            <button
              onClick={() => setShowYaml(!showYaml)}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
              style={{
                background: showYaml ? 'rgba(245, 158, 11, 0.15)' : 'var(--bg-tertiary)',
                color: showYaml ? '#f59e0b' : 'var(--text-secondary)',
                border: '1px solid var(--border-color)',
              }}
            >
              <Code size={12} />
              {showYaml ? 'Hide Source' : 'View Source'}
            </button>
          </div>
        </div>

        {/* Delete confirmation */}
        {confirmDelete === 'suite' && (
          <div className="mb-4 p-3 rounded-lg flex items-center justify-between" style={{ background: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.3)' }}>
            <span className="text-xs" style={{ color: '#f87171' }}>Delete suite &ldquo;{suiteKey}&rdquo; and all its drills?</span>
            <div className="flex gap-2">
              <button onClick={handleDeleteSuite} className="px-2 py-1 rounded text-xs font-medium text-white bg-red-600 hover:bg-red-700">Delete</button>
              <button onClick={() => setConfirmDelete(null)} className="px-2 py-1 rounded text-xs font-medium" style={{ color: 'var(--text-secondary)' }}>Cancel</button>
            </div>
          </div>
        )}

        {/* Tab bar */}
        <div className="flex gap-0 border-b" style={{ borderColor: 'var(--border-color)' }}>
          <button
            onClick={() => setActiveTab('drills')}
            className="px-4 py-2 text-xs font-medium transition-colors relative"
            style={{ color: activeTab === 'drills' ? '#f59e0b' : 'var(--text-muted)' }}
          >
            <span className="flex items-center gap-1.5">
              <ListChecks size={12} />
              Drills ({suite.drills?.length || 0})
            </span>
            {activeTab === 'drills' && (
              <div className="absolute bottom-0 left-0 right-0 h-0.5" style={{ background: '#f59e0b' }} />
            )}
          </button>
          <button
            onClick={() => setActiveTab('report')}
            className="px-4 py-2 text-xs font-medium transition-colors relative"
            style={{ color: activeTab === 'report' ? '#f59e0b' : 'var(--text-muted)' }}
          >
            <span className="flex items-center gap-1.5">
              <BarChart3 size={12} />
              Report
              {report && <StatusDot status={report.status} size={6} />}
            </span>
            {activeTab === 'report' && (
              <div className="absolute bottom-0 left-0 right-0 h-0.5" style={{ background: '#f59e0b' }} />
            )}
          </button>
        </div>
      </div>

      {/* Tab content */}
      <div className={`${showYaml ? 'h-1/2' : 'flex-1'} overflow-y-auto p-6`}>
        {activeTab === 'drills' ? (
          <>
            {/* Suite Config / Infrastructure */}
            {suite.suite_config && (
              <div className="mb-6 p-4 rounded-xl" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
                <h3 className="text-xs font-semibold uppercase tracking-wider mb-3" style={{ color: 'var(--text-muted)' }}>Infrastructure</h3>
                <div className="grid grid-cols-2 gap-3 text-xs">
                  {suite.suite_config.template && (
                    <div className="flex items-center gap-2">
                      <Server size={12} style={{ color: '#f59e0b' }} />
                      <span style={{ color: 'var(--text-secondary)' }}>Template:</span>
                      <span className="font-mono" style={{ color: 'var(--text-primary)' }}>{suite.suite_config.template}</span>
                    </div>
                  )}
                  {suite.suite_config.base_url && (
                    <div className="flex items-center gap-2">
                      <Zap size={12} style={{ color: '#f59e0b' }} />
                      <span style={{ color: 'var(--text-secondary)' }}>Base URL:</span>
                      <span className="font-mono" style={{ color: 'var(--text-primary)' }}>{suite.suite_config.base_url}</span>
                    </div>
                  )}
                  {suite.suite_config.services && suite.suite_config.services.length > 0 && (
                    <div className="flex items-center gap-2 col-span-2">
                      <Terminal size={12} style={{ color: '#f59e0b' }} />
                      <span style={{ color: 'var(--text-secondary)' }}>Services:</span>
                      <span className="font-mono" style={{ color: 'var(--text-primary)' }}>{suite.suite_config.services.join(', ')}</span>
                    </div>
                  )}
                  {suite.suite_config.setup && (
                    <div className="flex items-start gap-2 col-span-2">
                      <Terminal size={12} className="mt-0.5" style={{ color: '#f59e0b' }} />
                      <span style={{ color: 'var(--text-secondary)' }}>Setup:</span>
                      <code className="font-mono text-[11px] bg-black/20 px-1.5 py-0.5 rounded" style={{ color: 'var(--text-primary)' }}>{suite.suite_config.setup}</code>
                    </div>
                  )}
                  {suite.suite_config.ready_check && (
                    <div className="flex items-start gap-2 col-span-2">
                      <Shield size={12} className="mt-0.5" style={{ color: '#f59e0b' }} />
                      <span style={{ color: 'var(--text-secondary)' }}>Ready Check:</span>
                      <code className="font-mono text-[11px] bg-black/20 px-1.5 py-0.5 rounded" style={{ color: 'var(--text-primary)' }}>{suite.suite_config.ready_check}</code>
                    </div>
                  )}
                </div>
              </div>
            )}

            {/* Drills List */}
            <div className="space-y-2">
              {(suite.drills || []).map(drill => (
                <div
                  key={drill.name}
                  className="p-3 rounded-lg cursor-pointer hover:bg-white/5 transition-colors group"
                  style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}
                  onClick={() => onNavigate('#' + buildPath('drill', { subView: 'drill', subKey: suiteKey, subKey2: drill.name }))}
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <Crosshair size={12} style={{ color: '#f59e0b' }} />
                      <span className="text-xs font-medium" style={{ color: 'var(--text-primary)' }}>{drill.name}</span>
                      {drill.step_count > 0 && (
                        <span className="text-[10px] px-1.5 py-0.5 rounded-full" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
                          {drill.step_count} step{drill.step_count !== 1 ? 's' : ''}
                        </span>
                      )}
                    </div>
                    <div className="flex items-center gap-2">
                      {drill.tags && drill.tags.length > 0 && (
                        <div className="flex gap-1">
                          {drill.tags.map(tag => (
                            <span key={tag} className="text-[10px] px-1.5 py-0.5 rounded-full" style={{ background: 'rgba(245, 158, 11, 0.1)', color: '#fbbf24' }}>
                              {tag}
                            </span>
                          ))}
                        </div>
                      )}
                      {drill.timeout > 0 && (
                        <span className="text-[10px] flex items-center gap-0.5" style={{ color: 'var(--text-muted)' }}>
                          <Clock size={10} /> {drill.timeout}s
                        </span>
                      )}
                      {confirmDelete === drill.name ? (
                        <div className="flex gap-1" onClick={e => e.stopPropagation()}>
                          <button onClick={() => handleDeleteDrill(drill.name)} className="px-1.5 py-0.5 rounded text-[10px] font-medium text-white bg-red-600 hover:bg-red-700">Delete</button>
                          <button onClick={() => setConfirmDelete(null)} className="px-1.5 py-0.5 rounded text-[10px] font-medium" style={{ color: 'var(--text-muted)' }}>Cancel</button>
                        </div>
                      ) : (
                        <button
                          onClick={(e) => { e.stopPropagation(); setConfirmDelete(drill.name) }}
                          className="opacity-0 group-hover:opacity-100 transition-opacity p-1 rounded hover:bg-red-500/10"
                          style={{ color: 'var(--text-muted)' }}
                        >
                          <Trash2 size={11} />
                        </button>
                      )}
                      <ArrowRight size={12} className="opacity-0 group-hover:opacity-50 transition-opacity" style={{ color: 'var(--text-muted)' }} />
                    </div>
                  </div>
                  {drill.description && (
                    <p className="text-[11px] mt-1 pl-5" style={{ color: 'var(--text-muted)' }}>{drill.description}</p>
                  )}
                </div>
              ))}
              {(!suite.drills || suite.drills.length === 0) && (
                <div className="text-center py-8">
                  <Crosshair size={32} className="mx-auto mb-2" style={{ color: 'rgba(245, 158, 11, 0.2)' }} />
                  <p className="text-xs" style={{ color: 'var(--text-muted)' }}>No drills yet. Click &ldquo;Add Drills&rdquo; to create some.</p>
                </div>
              )}
            </div>
          </>
        ) : (
          /* Report tab */
          report ? (
            <>
              {/* Report summary header */}
              <div className="flex items-center gap-3 mb-4">
                <StatusBadge status={report.status} />
                <span className="text-xs" style={{ color: 'var(--text-secondary)' }}>{report.summary}</span>
                <span className="text-xs" style={{ color: 'var(--text-muted)' }}>&middot; {formatDuration(report.duration)}</span>
                <span className="text-xs" style={{ color: 'var(--text-muted)' }}>&middot; {formatTimeAgo(report.finished_at)}</span>
              </div>

              {/* Pass/Fail Summary Bar */}
              {reportTests.length > 0 && (
                <div className="mb-6">
                  <div className="flex rounded-full overflow-hidden h-2">
                    {(() => {
                      const passed = reportTests.filter(t => t.status === 'passed').length
                      const failed = reportTests.filter(t => t.status === 'failed').length
                      const errored = reportTests.filter(t => t.status === 'error').length
                      const skipped = reportTests.length - passed - failed - errored
                      const total = reportTests.length
                      return (
                        <>
                          {passed > 0 && <div style={{ width: `${(passed / total) * 100}%`, background: '#22c55e' }} />}
                          {failed > 0 && <div style={{ width: `${(failed / total) * 100}%`, background: '#ef4444' }} />}
                          {errored > 0 && <div style={{ width: `${(errored / total) * 100}%`, background: '#f59e0b' }} />}
                          {skipped > 0 && <div style={{ width: `${(skipped / total) * 100}%`, background: '#6b7280' }} />}
                        </>
                      )
                    })()}
                  </div>
                  <div className="flex gap-4 mt-2 text-[10px]" style={{ color: 'var(--text-muted)' }}>
                    <span className="flex items-center gap-1"><span className="w-2 h-2 rounded-full inline-block" style={{ background: '#22c55e' }} /> {reportTests.filter(t => t.status === 'passed').length} passed</span>
                    <span className="flex items-center gap-1"><span className="w-2 h-2 rounded-full inline-block" style={{ background: '#ef4444' }} /> {reportTests.filter(t => t.status === 'failed').length} failed</span>
                    <span className="flex items-center gap-1"><span className="w-2 h-2 rounded-full inline-block" style={{ background: '#f59e0b' }} /> {reportTests.filter(t => t.status === 'error').length} errored</span>
                  </div>
                </div>
              )}

              {/* Per-Drill Results */}
              <div className="space-y-2">
                {reportTests.map((test) => {
                  const isExpanded = expandedDrills[test.name]
                  const steps = test.steps || []
                  const triage = test.triage

                  return (
                    <div
                      key={test.name}
                      className="rounded-lg overflow-hidden"
                      style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}
                    >
                      <button
                        onClick={() => toggleReportDrill(test.name)}
                        className="w-full flex items-center justify-between p-3 hover:bg-white/5 transition-colors"
                      >
                        <div className="flex items-center gap-2">
                          <StatusDot status={test.status} />
                          <span className="text-xs font-medium" style={{ color: 'var(--text-primary)' }}>{test.name}</span>
                          {test.duration > 0 && (
                            <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>{formatDuration(test.duration)}</span>
                          )}
                        </div>
                        <div className="flex items-center gap-2">
                          <StatusBadge status={test.status} />
                          {isExpanded ? <ChevronDown size={12} style={{ color: 'var(--text-muted)' }} /> : <ChevronRight size={12} style={{ color: 'var(--text-muted)' }} />}
                        </div>
                      </button>

                      {isExpanded && (
                        <div className="px-3 pb-3 space-y-3">
                          {test.error && (
                            <div className="p-2 rounded text-xs font-mono" style={{ background: 'rgba(239, 68, 68, 0.1)', color: '#f87171' }}>
                              {test.error}
                            </div>
                          )}

                          {steps.length > 0 && (
                            <div>
                              <span className="text-[10px] font-semibold uppercase" style={{ color: 'var(--text-muted)' }}>Steps</span>
                              <div className="mt-1 space-y-1.5">
                                {steps.map((step, si) => (
                                  <ReportStepCard key={si} step={step} index={si} />
                                ))}
                              </div>
                            </div>
                          )}

                          {triage && (
                            <div className="p-3 rounded-lg" style={{ background: 'rgba(168, 85, 247, 0.06)', border: '1px solid rgba(168, 85, 247, 0.2)' }}>
                              <div className="flex items-center gap-2 mb-2">
                                <Zap size={12} style={{ color: '#a855f7' }} />
                                <span className="text-[10px] font-semibold uppercase" style={{ color: '#c084fc' }}>AI Triage</span>
                                {triage.verdict && <StatusBadge status={triage.verdict} />}
                              </div>
                              {triage.summary && (
                                <p className="text-xs mb-2" style={{ color: 'var(--text-secondary)' }}>{triage.summary}</p>
                              )}
                              {triage.root_cause && (
                                <div className="text-xs">
                                  <span className="font-medium" style={{ color: 'var(--text-muted)' }}>Root Cause: </span>
                                  <span style={{ color: 'var(--text-secondary)' }}>{triage.root_cause}</span>
                                </div>
                              )}
                              {triage.suggestion && (
                                <div className="text-xs mt-1">
                                  <span className="font-medium" style={{ color: 'var(--text-muted)' }}>Suggestion: </span>
                                  <span style={{ color: 'var(--text-secondary)' }}>{triage.suggestion}</span>
                                </div>
                              )}
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </>
          ) : (
            <div className="text-center py-12">
              <BarChart3 size={32} className="mx-auto mb-2" style={{ color: 'rgba(245, 158, 11, 0.2)' }} />
              <p className="text-xs" style={{ color: 'var(--text-muted)' }}>No report yet. Run the suite to generate one.</p>
            </div>
          )
        )}
      </div>

      {/* YAML Source Editor */}
      {showYaml && (
        <div className="h-1/2" style={{ borderTop: '1px solid var(--border-color)' }}>
          <YamlDrawer
            content={yamlContent}
            onChange={setYamlContent}
            onClose={() => setShowYaml(false)}
            theme={theme}
            subtitle="Suite definition YAML"
            onSave={handleSaveYaml}
            isSaving={isSaving}
            saveStatus={saveStatus}
          />
        </div>
      )}
    </div>
  )
}


// ─── Drill Detail ───

function DrillDetail({ suiteKey, drillKey, onNavigate, theme }) {
  const [drill, setDrill] = useState(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState(null)
  const [showYaml, setShowYaml] = useState(false)
  const [yamlContent, setYamlContent] = useState('')
  const [isSaving, setIsSaving] = useState(false)
  const [saveStatus, setSaveStatus] = useState(null)

  const loadDrill = useCallback(async () => {
    try {
      const [drillData, yamlData] = await Promise.all([
        fetchDrill(suiteKey, drillKey),
        fetchDrillYaml(suiteKey, drillKey),
      ])
      setDrill(drillData)
      setYamlContent(yamlData)
    } catch (err) {
      setError(err.message)
    } finally {
      setIsLoading(false)
    }
  }, [suiteKey, drillKey])

  useEffect(() => {
    setIsLoading(true)
    setError(null)
    loadDrill()
  }, [loadDrill])

  const handleSaveYaml = async () => {
    if (isSaving) return
    setIsSaving(true)
    setSaveStatus(null)
    try {
      await saveDrillYaml(suiteKey, drillKey, yamlContent)
      setSaveStatus('saved')
      // Reload drill JSON to sync the visual view with saved YAML
      const drillData = await fetchDrill(suiteKey, drillKey)
      setDrill(drillData)
      setTimeout(() => setSaveStatus(null), 2000)
    } catch (err) {
      setSaveStatus('error')
      setError(err.message)
    } finally {
      setIsSaving(false)
    }
  }

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader size={24} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
      </div>
    )
  }

  if (error && !drill) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <AlertCircle size={32} className="mx-auto mb-2" style={{ color: '#ef4444' }} />
          <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>{error}</p>
        </div>
      </div>
    )
  }

  if (!drill) return null

  const nodes = Array.isArray(drill.nodes) ? drill.nodes : []

  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      {/* Visual detail view */}
      <div className={`${showYaml ? 'h-1/2' : 'flex-1'} overflow-y-auto p-6`}>
        {/* Back + View Source header */}
        <div className="flex items-center justify-between mb-4">
          <button
            onClick={() => onNavigate('#' + buildPath('drill', { subView: 'suite', subKey: suiteKey }))}
            className="flex items-center gap-1 text-xs hover:opacity-80 transition-opacity"
            style={{ color: '#f59e0b' }}
          >
            <ChevronLeft size={14} /> Back to {suiteKey}
          </button>
          <button
            onClick={() => setShowYaml(!showYaml)}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
            style={{
              background: showYaml ? 'rgba(245, 158, 11, 0.15)' : 'var(--bg-tertiary)',
              color: showYaml ? '#f59e0b' : 'var(--text-secondary)',
              border: '1px solid var(--border-color)',
            }}
          >
            <Code size={12} />
            {showYaml ? 'Hide Source' : 'View Source'}
          </button>
        </div>

        {/* Header */}
        <div className="mb-6">
          <div className="flex items-center gap-2 mb-1">
            <Crosshair size={16} style={{ color: '#f59e0b' }} />
            <h1 className="text-xl font-bold" style={{ color: 'var(--text-primary)' }}>{drill.name}</h1>
          </div>
          {drill.description && (
            <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>{drill.description}</p>
          )}
          <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>{drill.file}</p>
        </div>

        {/* Metadata */}
        <div className="flex flex-wrap gap-3 mb-6">
          {drill.tags && drill.tags.length > 0 && (
            <div className="flex items-center gap-1.5">
              <Tag size={12} style={{ color: '#f59e0b' }} />
              <div className="flex gap-1">
                {drill.tags.map(tag => (
                  <span key={tag} className="text-[10px] px-1.5 py-0.5 rounded-full" style={{ background: 'rgba(245, 158, 11, 0.1)', color: '#fbbf24' }}>
                    {tag}
                  </span>
                ))}
              </div>
            </div>
          )}
          {drill.timeout > 0 && (
            <div className="flex items-center gap-1.5 text-xs" style={{ color: 'var(--text-secondary)' }}>
              <Clock size={12} style={{ color: '#f59e0b' }} /> Timeout: {drill.timeout}s
            </div>
          )}
          {drill.step_timeout > 0 && (
            <div className="flex items-center gap-1.5 text-xs" style={{ color: 'var(--text-secondary)' }}>
              <Clock size={12} style={{ color: '#f59e0b' }} /> Step Timeout: {drill.step_timeout}s
            </div>
          )}
          {drill.on_fail && (
            <div className="flex items-center gap-1.5 text-xs" style={{ color: 'var(--text-secondary)' }}>
              <AlertCircle size={12} style={{ color: '#f59e0b' }} /> On Fail: {drill.on_fail}
            </div>
          )}
        </div>

        {/* Steps Timeline */}
        <div>
          <h3 className="text-xs font-semibold uppercase tracking-wider mb-3" style={{ color: 'var(--text-muted)' }}>
            Steps ({nodes.length})
          </h3>
          <div className="relative">
            {nodes.length > 1 && (
              <div
                className="absolute left-[15px] top-4 bottom-4 w-px"
                style={{ background: 'var(--border-color)' }}
              />
            )}
            <div className="space-y-3">
              {nodes.map((node, idx) => (
                <StepCard key={node.name || idx} node={node} index={idx} />
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* YAML Source Editor */}
      {showYaml && (
        <div className="h-1/2" style={{ borderTop: '1px solid var(--border-color)' }}>
          <YamlDrawer
            content={yamlContent}
            onChange={setYamlContent}
            onClose={() => setShowYaml(false)}
            theme={theme}
            subtitle="Drill YAML source"
            onSave={handleSaveYaml}
            isSaving={isSaving}
            saveStatus={saveStatus}
          />
        </div>
      )}
    </div>
  )
}


// ─── Step Card ───

function StepCard({ node, index }) {
  const [expanded, setExpanded] = useState(false)

  const args = node.args || {}
  const toolName = args.tool || node.type || 'step'
  const assertion = node.assert // single object or null
  // Build display args: everything in args except the "tool" key
  const displayArgs = Object.entries(args).filter(([k]) => k !== 'tool')

  return (
    <div
      className="relative pl-9 cursor-pointer"
      onClick={() => setExpanded(!expanded)}
    >
      {/* Timeline dot */}
      <div
        className="absolute left-2 top-3 w-[14px] h-[14px] rounded-full border-2 flex items-center justify-center"
        style={{ borderColor: '#f59e0b', background: 'var(--bg-primary)' }}
      >
        <span className="text-[8px] font-bold" style={{ color: '#f59e0b' }}>{index + 1}</span>
      </div>

      <div
        className="p-3 rounded-lg transition-colors"
        style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}
      >
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="text-xs font-medium" style={{ color: 'var(--text-primary)' }}>
              {node.name || `Step ${index + 1}`}
            </span>
            <span className="text-[10px] font-mono px-1.5 py-0.5 rounded" style={{ background: 'rgba(245, 158, 11, 0.1)', color: '#fbbf24' }}>
              {toolName}
            </span>
          </div>
          <div className="flex items-center gap-2">
            {assertion && (
              <span className="text-[10px] flex items-center gap-0.5" style={{ color: 'var(--text-muted)' }}>
                <Shield size={10} /> 1 assertion
              </span>
            )}
            {expanded ? <ChevronDown size={12} style={{ color: 'var(--text-muted)' }} /> : <ChevronRight size={12} style={{ color: 'var(--text-muted)' }} />}
          </div>
        </div>

        {/* Show a preview of the command/action even when collapsed */}
        {!expanded && displayArgs.length > 0 && (
          <p className="text-[11px] mt-1 font-mono truncate" style={{ color: 'var(--text-muted)' }}>
            {displayArgs.map(([k, v]) => `${k}: ${typeof v === 'string' ? v : JSON.stringify(v)}`).join(', ')}
          </p>
        )}

        {expanded && (
          <div className="mt-3 space-y-2">
            {/* Tool Arguments */}
            {displayArgs.length > 0 && (
              <div>
                <span className="text-[10px] font-semibold uppercase" style={{ color: 'var(--text-muted)' }}>Arguments</span>
                <div className="mt-1 space-y-1">
                  {displayArgs.map(([key, value]) => (
                    <div key={key} className="flex items-start gap-2 text-[11px] p-1.5 rounded" style={{ background: 'rgba(0,0,0,0.2)' }}>
                      <span className="font-mono font-medium flex-shrink-0" style={{ color: '#fbbf24' }}>{key}:</span>
                      <span className="font-mono break-all" style={{ color: 'var(--text-primary)' }}>
                        {typeof value === 'string' ? value : JSON.stringify(value, null, 2)}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Assertion */}
            {assertion && (
              <div>
                <span className="text-[10px] font-semibold uppercase" style={{ color: 'var(--text-muted)' }}>Assertion</span>
                <div className="mt-1 p-2 rounded" style={{ background: 'rgba(0,0,0,0.2)' }}>
                  <div className="flex items-start gap-2 text-[11px] font-mono">
                    <Shield size={10} className="mt-0.5 flex-shrink-0" style={{ color: '#f59e0b' }} />
                    <div>
                      <span style={{ color: '#fbbf24' }}>{assertion.type}</span>
                      {assertion.source && assertion.source !== 'output' && (
                        <span style={{ color: 'var(--text-muted)' }}> (source: {assertion.source})</span>
                      )}
                      <span style={{ color: 'var(--text-muted)' }}> = </span>
                      <span style={{ color: 'var(--text-primary)' }}>{assertion.expected}</span>
                      {assertion.on_fail && (
                        <span className="ml-2 text-[10px]" style={{ color: 'var(--text-muted)' }}>[on_fail: {assertion.on_fail}]</span>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            )}

            {/* Prompt (for LLM nodes) */}
            {node.prompt && (
              <div>
                <span className="text-[10px] font-semibold uppercase" style={{ color: 'var(--text-muted)' }}>Prompt</span>
                <pre className="mt-1 text-[11px] font-mono p-2 rounded overflow-x-auto whitespace-pre-wrap" style={{ background: 'rgba(0,0,0,0.2)', color: 'var(--text-primary)' }}>
                  {node.prompt}
                </pre>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}


// ─── Report Step Card ───

function ReportStepCard({ step, index }) {
  const [expanded, setExpanded] = useState(false)
  const sc = statusColor(step.status)
  const assertions = step.assertions || []

  return (
    <div
      className="p-2 rounded cursor-pointer hover:bg-white/5 transition-colors"
      style={{ background: sc.bg, border: `1px solid ${sc.border}` }}
      onClick={() => setExpanded(!expanded)}
    >
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <StatusDot status={step.status} size={6} />
          <span className="text-[11px] font-medium" style={{ color: 'var(--text-primary)' }}>
            {step.name || `Step ${index + 1}`}
          </span>
          {step.tool && (
            <span className="text-[10px] font-mono px-1 rounded" style={{ background: 'rgba(0,0,0,0.2)', color: 'var(--text-muted)' }}>
              {step.tool}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {step.duration > 0 && (
            <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>{formatDuration(step.duration)}</span>
          )}
          {assertions.length > 0 && (
            <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
              {assertions.filter(a => a.passed).length}/{assertions.length}
            </span>
          )}
          {expanded ? <ChevronDown size={10} style={{ color: 'var(--text-muted)' }} /> : <ChevronRight size={10} style={{ color: 'var(--text-muted)' }} />}
        </div>
      </div>

      {expanded && (
        <div className="mt-2 space-y-2">
          {/* Assertions */}
          {assertions.length > 0 && (
            <div className="space-y-1">
              {assertions.map((a, i) => (
                <div key={i} className="flex items-start gap-1.5 text-[10px]">
                  {a.passed ? (
                    <Check size={10} className="mt-0.5 flex-shrink-0" style={{ color: '#22c55e' }} />
                  ) : (
                    <X size={10} className="mt-0.5 flex-shrink-0" style={{ color: '#ef4444' }} />
                  )}
                  <span style={{ color: 'var(--text-primary)' }}>
                    {a.message || a.description || `${a.type}: ${a.expected || ''}`}
                  </span>
                  {!a.passed && a.actual && (
                    <span className="font-mono" style={{ color: '#f87171' }}> (got: {a.actual})</span>
                  )}
                </div>
              ))}
            </div>
          )}

          {/* Raw output */}
          {step.output && (
            <div>
              <span className="text-[10px] font-semibold uppercase" style={{ color: 'var(--text-muted)' }}>Output</span>
              <pre className="mt-1 text-[10px] font-mono p-2 rounded overflow-x-auto max-h-40 overflow-y-auto" style={{ background: 'rgba(0,0,0,0.2)', color: 'var(--text-primary)' }}>
                {typeof step.output === 'string' ? step.output : JSON.stringify(step.output, null, 2)}
              </pre>
            </div>
          )}

          {/* Error */}
          {step.error && (
            <div className="p-1.5 rounded text-[10px] font-mono" style={{ background: 'rgba(239, 68, 68, 0.1)', color: '#f87171' }}>
              {step.error}
            </div>
          )}
        </div>
      )}
    </div>
  )
}


// ─── Empty State ───

function EmptyState() {
  return (
    <div className="flex-1 flex items-center justify-center">
      <div className="text-center">
        <Crosshair size={48} className="mx-auto mb-4" style={{ color: 'rgba(245, 158, 11, 0.3)' }} />
        <h2 className="text-lg font-semibold mb-2" style={{ color: 'var(--text-primary)' }}>Drill Management</h2>
        <p className="text-sm max-w-md" style={{ color: 'var(--text-muted)' }}>
          Select a drill suite from the sidebar to get started.
          Drills are AI-composed, mechanically-replayed sequences of tool calls
          with assertions and reporting — perfect for health checks, deployment
          verification, and repeatable multi-step automation.
        </p>
      </div>
    </div>
  )
}


// ─── Main DrillView ───

export default function DrillView({ path, onNavigate, onRunSuite, onAddDrills, theme }) {
  const [suites, setSuites] = useState([])
  const [isLoading, setIsLoading] = useState(true)
  const [searchQuery, setSearchQuery] = useState('')

  const loadData = useCallback(async () => {
    try {
      const suitesData = await fetchDrillSuites()
      setSuites(Array.isArray(suitesData) ? suitesData : [])
    } catch (err) {
      console.error('Failed to load drill data:', err)
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    loadData()
    const interval = setInterval(loadData, 30000)
    return () => clearInterval(interval)
  }, [loadData])

  const selectedItem = useMemo(() => {
    const subView = path?.params?.subView
    const subKey = path?.params?.subKey
    if (subView && subKey) {
      return { type: subView, key: subKey }
    }
    return null
  }, [path])

  const handleSelect = useCallback((item) => {
    const hashPath = buildPath('drill', { subView: item.type, subKey: item.key })
    onNavigate('#' + hashPath)
  }, [onNavigate])

  const handleNavigate = useCallback((hashPath) => {
    onNavigate(hashPath)
  }, [onNavigate])

  const filteredSuites = useMemo(() => {
    if (!searchQuery) return suites
    const q = searchQuery.toLowerCase()
    return suites.filter(s =>
      s.name.toLowerCase().includes(q) ||
      (s.description && s.description.toLowerCase().includes(q))
    )
  }, [suites, searchQuery])

  const renderContent = () => {
    if (!selectedItem) return <EmptyState />

    switch (selectedItem.type) {
      case 'suite':
        return (
          <SuiteDetail
            key={selectedItem.key}
            suiteKey={selectedItem.key}
            onNavigate={handleNavigate}
            onRunSuite={onRunSuite}
            onAddDrills={onAddDrills}
            onRefresh={loadData}
            theme={theme}
          />
        )
      case 'drill':
        return (
          <DrillDetail
            key={`${path?.params?.subKey}-${path?.params?.subKey2}`}
            suiteKey={path?.params?.subKey}
            drillKey={path?.params?.subKey2}
            onNavigate={handleNavigate}
            theme={theme}
          />
        )
      default:
        return <EmptyState />
    }
  }

  return (
    <div className="flex flex-1 overflow-hidden">
      <DrillSidebar
        suites={filteredSuites}
        selectedItem={selectedItem}
        onSelect={handleSelect}
        onSearch={setSearchQuery}
        searchQuery={searchQuery}
        isLoading={isLoading}
      />
      <div className="flex-1 flex flex-col overflow-hidden">
        {renderContent()}
      </div>
    </div>
  )
}
