import { useState, useEffect, useCallback } from 'react'
import { Save, AlertCircle, Check, Play, Trash2, Clock, Loader2, CheckCircle, XCircle, Pause, ChevronRight, ChevronDown, X, AlertTriangle } from 'lucide-react'
import { saveFullConfigSection, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'

// --- Types ---

interface SchedulerJob {
  id: string
  name: string
  enabled: boolean
  mode: string
  schedule?: {
    cron?: string
    timezone?: string
  }
  last_status?: string
  last_run?: string
  next_run?: string
  delivery?: {
    channel?: string
    target?: string
  }
  consecutive_failures?: number
  payload?: {
    instructions?: string
    flow?: string
    params?: Record<string, unknown>
  }
  last_error?: string
  [key: string]: unknown
}

interface SchedulerConfig {
  enabled?: boolean
  [key: string]: unknown
}

interface SchedulerSettingsProps {
  config: SchedulerConfig | null
  onSaved?: () => void
}

// API helpers
const fetchJobs = async (): Promise<{ jobs?: SchedulerJob[] }> => {
  const res = await fetch('/api/scheduler/jobs')
  if (!res.ok) throw new Error('Failed to fetch jobs')
  return res.json()
}

const updateJob = async (id: string, data: Record<string, unknown>): Promise<Record<string, unknown>> => {
  const res = await fetch(`/api/scheduler/jobs/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data)
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to update job')
  }
  return res.json()
}

const deleteJob = async (id: string): Promise<Record<string, unknown>> => {
  const res = await fetch(`/api/scheduler/jobs/${encodeURIComponent(id)}`, {
    method: 'DELETE'
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to delete job')
  }
  return res.json()
}

const runJobNow = async (id: string): Promise<Record<string, unknown>> => {
  const res = await fetch(`/api/scheduler/jobs/${encodeURIComponent(id)}/run`, {
    method: 'POST'
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to run job')
  }
  return res.json()
}

function formatRelativeTime(dateStr: string): string {
  if (!dateStr) return 'Never'
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = date.getTime() - now.getTime()
  const absDiff = Math.abs(diffMs)

  if (absDiff < 60000) return diffMs > 0 ? 'in < 1 min' : '< 1 min ago'
  if (absDiff < 3600000) {
    const mins = Math.round(absDiff / 60000)
    return diffMs > 0 ? `in ${mins} min` : `${mins} min ago`
  }
  if (absDiff < 86400000) {
    const hours = Math.round(absDiff / 3600000)
    return diffMs > 0 ? `in ${hours}h` : `${hours}h ago`
  }
  const days = Math.round(absDiff / 86400000)
  return diffMs > 0 ? `in ${days}d` : `${days}d ago`
}

function StatusBadge({ status }: { status: string | undefined }) {
  if (!status || status === 'pending') {
    return (
      <span className="text-xs px-1.5 py-0.5 rounded" style={{ background: 'rgba(100,100,100,0.2)', color: 'var(--text-muted)' }}>
        pending
      </span>
    )
  }
  if (status === 'success') {
    return (
      <span className="text-xs px-1.5 py-0.5 rounded flex items-center gap-1" style={{ background: 'rgba(34, 197, 94, 0.15)', color: '#22c55e' }}>
        <CheckCircle size={10} /> success
      </span>
    )
  }
  return (
    <span className="text-xs px-1.5 py-0.5 rounded flex items-center gap-1" style={{ background: 'rgba(239, 68, 68, 0.1)', color: '#f87171' }}>
      <XCircle size={10} /> failed
    </span>
  )
}

function ModeBadge({ mode }: { mode: string }) {
  const isAdaptive = mode === 'adaptive'
  return (
    <span className="text-xs px-1.5 py-0.5 rounded" style={{
      background: isAdaptive ? 'rgba(168, 85, 247, 0.15)' : 'rgba(59, 130, 246, 0.15)',
      color: isAdaptive ? '#a855f7' : '#3b82f6'
    }}>
      {mode}
    </span>
  )
}

export default function SchedulerSettings({ config, onSaved }: SchedulerSettingsProps) {
  const [enabled, setEnabled] = useState(true)
  const [jobs, setJobs] = useState<SchedulerJob[]>([])
  const [jobsLoading, setJobsLoading] = useState(false)
  const [jobsError, setJobsError] = useState<string | null>(null)
  const [expandedJob, setExpandedJob] = useState<string | null>(null)
  const [runningJob, setRunningJob] = useState<string | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<SchedulerJob | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  // Config save state
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (config) {
      setEnabled(config.enabled !== false)
    }
  }, [config])

  const loadJobs = useCallback(async () => {
    setJobsLoading(true)
    setJobsError(null)
    try {
      const data = await fetchJobs()
      setJobs(data.jobs || [])
    } catch (err: any) {
      setJobsError(err.message)
    } finally {
      setJobsLoading(false)
    }
  }, [])

  useEffect(() => {
    loadJobs()
  }, [loadJobs])

  const handleSaveConfig = async () => {
    setSaving(true)
    setSaveSuccess(false)
    setError(null)
    try {
      await saveFullConfigSection('scheduler', { enabled })
      setSaveSuccess(true)
      if (onSaved) onSaved()
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err: any) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const handleToggleJob = async (job: SchedulerJob) => {
    setActionError(null)
    try {
      await updateJob(job.id, { ...job, enabled: !job.enabled })
      loadJobs()
    } catch (err: any) {
      setActionError(err.message)
    }
  }

  const handleRunJob = async (job: SchedulerJob) => {
    setRunningJob(job.id)
    setActionError(null)
    try {
      await runJobNow(job.id)
      // Reload after a brief delay to show updated status
      setTimeout(() => loadJobs(), 1000)
    } catch (err: any) {
      setActionError(err.message)
    } finally {
      setRunningJob(null)
    }
  }

  const handleDeleteJob = async (id: string) => {
    setActionError(null)
    try {
      await deleteJob(id)
      setDeleteConfirm(null)
      if (expandedJob === id) setExpandedJob(null)
      loadJobs()
    } catch (err: any) {
      setActionError(err.message)
    }
  }

  const enabledJobs = jobs.filter(j => j.enabled)

  return (
    <div className="max-w-2xl space-y-6">
      {/* Master Toggle */}
      <div className="flex items-center justify-between">
        <div>
          <label className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
            Enable Scheduler
          </label>
          <p className="text-xs mt-0.5" style={hintStyle}>
            Run scheduled jobs automatically via cron expressions. Default: enabled.
          </p>
        </div>
        <button
          onClick={() => setEnabled(!enabled)}
          className="relative w-11 h-6 rounded-full transition-colors"
          style={{
            background: enabled ? '#a855f7' : 'var(--bg-tertiary)',
            border: `1px solid ${enabled ? '#a855f7' : 'var(--border-color)'}`
          }}
        >
          <span
            className="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform bg-white"
            style={{ transform: enabled ? 'translateX(20px)' : 'translateX(0)' }}
          />
        </button>
      </div>

      {/* Save Config */}
      <div className="flex items-center gap-3">
        <button
          onClick={handleSaveConfig}
          disabled={saving}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-white font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
          style={saveButtonStyle}
        >
          <Save size={16} />
          {saving ? 'Saving...' : 'Save'}
        </button>
        {saveSuccess && (
          <span className="flex items-center gap-1 text-green-400 text-sm">
            <Check size={16} /> Saved
          </span>
        )}
        {error && (
          <span className="flex items-center gap-1 text-sm" style={{ color: 'var(--danger)' }}>
            <AlertCircle size={16} /> {error}
          </span>
        )}
      </div>

      {/* Jobs List */}
      <div className="pt-4 border-t" style={sectionBorderStyle}>
        <div className="flex items-center justify-between mb-3">
          <h4 className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
            Scheduled Jobs
            {!jobsLoading && jobs.length > 0 && (
              <span className="ml-2 text-xs font-normal" style={hintStyle}>
                {enabledJobs.length} active, {jobs.length} total
              </span>
            )}
          </h4>
        </div>

        {actionError && (
          <div className="flex items-center gap-2 p-3 rounded-lg text-sm mb-3"
            style={{ background: 'rgba(239, 68, 68, 0.1)', color: 'var(--danger)' }}>
            <AlertCircle size={14} /> {actionError}
            <button onClick={() => setActionError(null)} className="ml-auto p-0.5"><X size={14} /></button>
          </div>
        )}

        {jobsLoading && (
          <div className="flex items-center gap-2 py-4">
            <Loader2 size={16} className="animate-spin" style={{ color: 'var(--accent)' }} />
            <span className="text-sm" style={hintStyle}>Loading jobs...</span>
          </div>
        )}

        {jobsError && (
          <div className="flex items-center gap-2 p-3 rounded-lg text-sm mb-3"
            style={{ background: 'rgba(239, 68, 68, 0.1)', color: 'var(--danger)' }}>
            <AlertCircle size={14} /> {jobsError}
          </div>
        )}

        {!jobsLoading && jobs.length === 0 && !jobsError && (
          <div className="py-6 text-center">
            <Clock size={32} className="mx-auto mb-2" style={{ color: 'var(--text-muted)', opacity: 0.5 }} />
            <p className="text-sm" style={hintStyle}>
              No scheduled jobs yet.
            </p>
            <p className="text-xs mt-1" style={hintStyle}>
              Jobs are created through chat. Ask the AI to schedule a task for you, or use <code>astonish scheduler</code> from the CLI.
            </p>
          </div>
        )}

        {/* Job Cards */}
        <div className="space-y-2">
          {jobs.map(job => {
            const isExpanded = expandedJob === job.id
            const isRunning = runningJob === job.id

            return (
              <div
                key={job.id}
                className="rounded-lg border transition-all"
                style={{
                  background: 'var(--bg-secondary)',
                  borderColor: isExpanded ? 'rgba(168, 85, 247, 0.4)' : 'var(--border-color)'
                }}
              >
                {/* Job Header */}
                <div
                  className="px-4 py-3 cursor-pointer flex items-center gap-3"
                  onClick={() => setExpandedJob(isExpanded ? null : job.id)}
                >
                  {isExpanded
                    ? <ChevronDown size={14} style={{ color: 'var(--text-muted)' }} />
                    : <ChevronRight size={14} style={{ color: 'var(--text-muted)' }} />
                  }

                  {/* Enable/Disable indicator */}
                  <div className="flex-shrink-0" title={job.enabled ? 'Enabled' : 'Disabled'}>
                    {job.enabled ? (
                      <CheckCircle size={16} style={{ color: '#22c55e' }} />
                    ) : (
                      <Pause size={16} style={{ color: 'var(--text-muted)' }} />
                    )}
                  </div>

                  {/* Name & badges */}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium truncate" style={{ color: job.enabled ? 'var(--text-primary)' : 'var(--text-muted)' }}>
                        {job.name}
                      </span>
                      <ModeBadge mode={job.mode} />
                      <StatusBadge status={job.last_status} />
                    </div>
                    <div className="text-xs flex items-center gap-3 mt-0.5" style={hintStyle}>
                      <span className="font-mono">{job.schedule?.cron}</span>
                      {job.schedule?.timezone && (
                        <span>{job.schedule.timezone}</span>
                      )}
                      {job.next_run && job.enabled && (
                        <span>Next: {formatRelativeTime(job.next_run)}</span>
                      )}
                    </div>
                  </div>

                  {/* Quick actions */}
                  <div className="flex items-center gap-1 flex-shrink-0" onClick={(e) => e.stopPropagation()}>
                    <button
                      onClick={() => handleToggleJob(job)}
                      className="p-1.5 rounded-lg transition-colors hover:bg-gray-600/30"
                      style={{ color: 'var(--text-muted)' }}
                      title={job.enabled ? 'Disable' : 'Enable'}
                    >
                      {job.enabled ? <Pause size={14} /> : <Play size={14} />}
                    </button>
                    <button
                      onClick={() => handleRunJob(job)}
                      disabled={isRunning}
                      className="p-1.5 rounded-lg transition-colors hover:bg-gray-600/30 disabled:opacity-50"
                      style={{ color: 'var(--accent)' }}
                      title="Run now"
                    >
                      {isRunning ? <Loader2 size={14} className="animate-spin" /> : <Play size={14} />}
                    </button>
                    <button
                      onClick={() => setDeleteConfirm(job)}
                      className="p-1.5 rounded-lg transition-colors hover:bg-red-600/20"
                      style={{ color: 'var(--text-muted)' }}
                      title="Delete"
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                </div>

                {/* Expanded Details */}
                {isExpanded && (
                  <div className="px-4 pb-4 border-t space-y-3" style={sectionBorderStyle}>
                    <div className="pt-3 grid grid-cols-2 gap-4 text-xs">
                      <div>
                        <div className="font-medium mb-1" style={hintStyle}>Mode</div>
                        <div style={{ color: 'var(--text-primary)' }}>
                          {job.mode === 'adaptive' ? 'Adaptive (LLM agent)' : 'Routine (headless flow)'}
                        </div>
                      </div>
                      <div>
                        <div className="font-medium mb-1" style={hintStyle}>Schedule</div>
                        <div className="font-mono" style={{ color: 'var(--text-primary)' }}>
                          {job.schedule?.cron}
                          {job.schedule?.timezone && <span className="ml-1 font-sans" style={hintStyle}>({job.schedule.timezone})</span>}
                        </div>
                      </div>
                      <div>
                        <div className="font-medium mb-1" style={hintStyle}>Last Run</div>
                        <div style={{ color: 'var(--text-primary)' }}>
                          {job.last_run ? formatRelativeTime(job.last_run) : 'Never'}
                        </div>
                      </div>
                      <div>
                        <div className="font-medium mb-1" style={hintStyle}>Next Run</div>
                        <div style={{ color: 'var(--text-primary)' }}>
                          {job.enabled && job.next_run ? formatRelativeTime(job.next_run) : 'Disabled'}
                        </div>
                      </div>
                      {job.delivery?.channel && (
                        <div>
                          <div className="font-medium mb-1" style={hintStyle}>Delivery</div>
                          <div style={{ color: 'var(--text-primary)' }}>
                            {job.delivery.channel}{job.delivery.target ? ` (${job.delivery.target})` : ''}
                          </div>
                        </div>
                      )}
                      {(job.consecutive_failures ?? 0) > 0 && (
                        <div>
                          <div className="font-medium mb-1" style={hintStyle}>Failures</div>
                          <div className="flex items-center gap-1" style={{ color: '#f87171' }}>
                            <AlertTriangle size={12} /> {job.consecutive_failures} consecutive
                          </div>
                        </div>
                      )}
                    </div>

                    {/* Payload */}
                    {job.mode === 'adaptive' && job.payload?.instructions && (
                      <div>
                        <div className="text-xs font-medium mb-1" style={hintStyle}>Instructions</div>
                        <div className="p-2 rounded text-xs font-mono whitespace-pre-wrap" style={{ background: 'var(--bg-primary)', color: 'var(--text-secondary)' }}>
                          {job.payload.instructions}
                        </div>
                      </div>
                    )}
                    {job.mode === 'routine' && job.payload?.flow && (
                      <div>
                        <div className="text-xs font-medium mb-1" style={hintStyle}>Flow</div>
                        <div className="text-xs font-mono" style={{ color: 'var(--text-primary)' }}>
                          {job.payload.flow}
                          {job.payload.params && Object.keys(job.payload.params).length > 0 && (
                            <span style={hintStyle}> with {Object.keys(job.payload.params).length} param(s)</span>
                          )}
                        </div>
                      </div>
                    )}

                    {/* Last Error */}
                    {job.last_error && (
                      <div>
                        <div className="text-xs font-medium mb-1" style={{ color: '#f87171' }}>Last Error</div>
                        <div className="p-2 rounded text-xs font-mono whitespace-pre-wrap" style={{ background: 'rgba(239, 68, 68, 0.05)', color: '#f87171' }}>
                          {job.last_error}
                        </div>
                      </div>
                    )}

                    <div className="text-xs font-mono pt-1" style={{ color: 'var(--text-muted)', opacity: 0.5 }}>
                      ID: {job.id}
                    </div>
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </div>

      {/* Delete Confirmation */}
      {deleteConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4" style={{ background: 'rgba(0,0,0,0.7)' }}>
          <div className="rounded-xl w-full max-w-sm p-6 shadow-2xl"
            style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <h3 className="text-lg font-semibold mb-2" style={{ color: 'var(--text-primary)' }}>Delete Job</h3>
            <p className="text-sm mb-4" style={hintStyle}>
              Are you sure you want to delete <strong style={{ color: 'var(--text-primary)' }}>{deleteConfirm.name}</strong>? This cannot be undone.
            </p>
            <div className="flex justify-end gap-3">
              <button
                onClick={() => setDeleteConfirm(null)}
                className="px-4 py-2 rounded-lg text-sm font-medium"
                style={{ color: 'var(--text-secondary)', background: 'var(--bg-tertiary)', border: '1px solid var(--border-color)' }}
              >
                Cancel
              </button>
              <button
                onClick={() => handleDeleteJob(deleteConfirm.id)}
                className="flex items-center gap-2 px-4 py-2 rounded-lg text-white text-sm font-medium transition-all"
                style={{ background: '#ef4444' }}
              >
                <Trash2 size={14} /> Delete
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
