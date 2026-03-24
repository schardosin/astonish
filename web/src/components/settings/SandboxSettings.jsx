import { useState, useEffect, useCallback } from 'react'
import { Save, AlertCircle, Check, Shield, ShieldOff, Loader2, Trash2, RefreshCw, Plus, Camera, ArrowUpCircle, Eye, ChevronDown, ChevronRight, Server, Box } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'
import {
  fetchSandboxDetails, fetchContainers, deleteContainer, pruneOrphans,
  fetchTemplates, fetchTemplateInfo, createTemplate, deleteTemplate,
  snapshotTemplate, promoteTemplate, refreshTemplates
} from '../../api/sandbox'

// --- Badge components ---

function StatusBadge({ status }) {
  const colors = {
    running: { bg: 'rgba(34, 197, 94, 0.15)', color: '#22c55e' },
    stopped: { bg: 'rgba(107, 114, 128, 0.15)', color: '#9ca3af' },
    missing: { bg: 'rgba(239, 68, 68, 0.15)', color: '#ef4444' },
  }
  const c = colors[status] || colors.stopped
  return (
    <span className="text-xs px-1.5 py-0.5 rounded inline-flex items-center gap-1"
      style={{ background: c.bg, color: c.color }}>
      <span className="w-1.5 h-1.5 rounded-full" style={{ background: c.color }} />
      {status}
    </span>
  )
}

function TemplateBadge({ name }) {
  const isBase = name === 'base'
  return (
    <span className="text-xs px-1.5 py-0.5 rounded"
      style={{
        background: isBase ? 'rgba(168, 85, 247, 0.15)' : 'rgba(59, 130, 246, 0.15)',
        color: isBase ? '#a855f7' : '#3b82f6'
      }}>
      @{name}
    </span>
  )
}

// --- Main component ---

export default function SandboxSettings({ config, onSaved }) {
  // Config form (enable/disable)
  const [form, setForm] = useState({ enabled: true, memory: '2GB', cpu: 2, processes: 500 })
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [saveError, setSaveError] = useState(null)

  // Details / status
  const [details, setDetails] = useState(null)
  const [detailsLoading, setDetailsLoading] = useState(true)

  // Containers
  const [containerData, setContainerData] = useState(null)
  const [containersLoading, setContainersLoading] = useState(false)
  const [deleteConfirm, setDeleteConfirm] = useState(null)
  const [pruneConfirm, setPruneConfirm] = useState(false)
  const [actionLoading, setActionLoading] = useState(null)
  const [actionError, setActionError] = useState(null)

  // Templates
  const [templateData, setTemplateData] = useState(null)
  const [templatesLoading, setTemplatesLoading] = useState(false)
  const [expandedTemplate, setExpandedTemplate] = useState(null)
  const [templateDetail, setTemplateDetail] = useState(null)
  const [templateDetailLoading, setTemplateDetailLoading] = useState(false)
  const [templateDeleteConfirm, setTemplateDeleteConfirm] = useState(null)
  const [promoteConfirm, setPromoteConfirm] = useState(null)
  const [showCreateForm, setShowCreateForm] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createDesc, setCreateDesc] = useState('')
  const [templateActionLoading, setTemplateActionLoading] = useState(null)
  const [templateError, setTemplateError] = useState(null)

  // Init form from config
  useEffect(() => {
    if (config) setForm({
      enabled: config.enabled ?? true,
      memory: config.memory || '2GB',
      cpu: config.cpu || 2,
      processes: config.processes || 500
    })
  }, [config])

  // Load details on mount
  const loadDetails = useCallback(() => {
    setDetailsLoading(true)
    fetchSandboxDetails()
      .then(setDetails)
      .catch(() => setDetails(null))
      .finally(() => setDetailsLoading(false))
  }, [])

  useEffect(() => { loadDetails() }, [loadDetails])

  // Load containers
  const loadContainers = useCallback(() => {
    setContainersLoading(true)
    setActionError(null)
    fetchContainers()
      .then(setContainerData)
      .catch(() => setContainerData(null))
      .finally(() => setContainersLoading(false))
  }, [])

  // Load templates
  const loadTemplates = useCallback(() => {
    setTemplatesLoading(true)
    setTemplateError(null)
    fetchTemplates()
      .then(setTemplateData)
      .catch(() => setTemplateData(null))
      .finally(() => setTemplatesLoading(false))
  }, [])

  // Auto-load containers and templates when details show Incus is available
  useEffect(() => {
    if (details?.incusAvailable) {
      loadContainers()
      loadTemplates()
    }
  }, [details, loadContainers, loadTemplates])

  // Save config
  const handleSave = async () => {
    setSaving(true)
    setSaveSuccess(false)
    setSaveError(null)
    try {
      await saveFullConfigSection('sandbox', form)
      setSaveSuccess(true)
      if (onSaved) onSaved()
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err) {
      setSaveError(err.message)
    } finally {
      setSaving(false)
    }
  }

  // Container actions
  const handleDeleteContainer = async (sessionId) => {
    setActionLoading(sessionId)
    setActionError(null)
    try {
      await deleteContainer(sessionId)
      setDeleteConfirm(null)
      loadContainers()
      loadDetails()
    } catch (err) {
      setActionError(err.message)
    } finally {
      setActionLoading(null)
    }
  }

  const handlePrune = async () => {
    setActionLoading('prune')
    setActionError(null)
    try {
      await pruneOrphans()
      setPruneConfirm(false)
      loadContainers()
      loadDetails()
    } catch (err) {
      setActionError(err.message)
    } finally {
      setActionLoading(null)
    }
  }

  // Template actions
  const handleCreateTemplate = async () => {
    if (!createName.trim()) return
    setTemplateActionLoading('create')
    setTemplateError(null)
    try {
      await createTemplate(createName.trim(), createDesc.trim())
      setShowCreateForm(false)
      setCreateName('')
      setCreateDesc('')
      loadTemplates()
      loadDetails()
    } catch (err) {
      setTemplateError(err.message)
    } finally {
      setTemplateActionLoading(null)
    }
  }

  const handleDeleteTemplate = async (name) => {
    setTemplateActionLoading(name)
    setTemplateError(null)
    try {
      await deleteTemplate(name)
      setTemplateDeleteConfirm(null)
      if (expandedTemplate === name) setExpandedTemplate(null)
      loadTemplates()
      loadDetails()
    } catch (err) {
      setTemplateError(err.message)
    } finally {
      setTemplateActionLoading(null)
    }
  }

  const handleSnapshot = async (name) => {
    setTemplateActionLoading(`snapshot-${name}`)
    setTemplateError(null)
    try {
      await snapshotTemplate(name)
      loadTemplates()
      if (expandedTemplate === name) handleExpandTemplate(name)
    } catch (err) {
      setTemplateError(err.message)
    } finally {
      setTemplateActionLoading(null)
    }
  }

  const handlePromote = async (name) => {
    setTemplateActionLoading(`promote-${name}`)
    setTemplateError(null)
    try {
      await promoteTemplate(name)
      setPromoteConfirm(null)
      loadTemplates()
      loadDetails()
    } catch (err) {
      setTemplateError(err.message)
    } finally {
      setTemplateActionLoading(null)
    }
  }

  const handleRefresh = async () => {
    setTemplateActionLoading('refresh')
    setTemplateError(null)
    try {
      await refreshTemplates()
      loadTemplates()
    } catch (err) {
      setTemplateError(err.message)
    } finally {
      setTemplateActionLoading(null)
    }
  }

  const handleExpandTemplate = (name) => {
    if (expandedTemplate === name) {
      setExpandedTemplate(null)
      setTemplateDetail(null)
      return
    }
    setExpandedTemplate(name)
    setTemplateDetailLoading(true)
    fetchTemplateInfo(name)
      .then(setTemplateDetail)
      .catch(() => setTemplateDetail(null))
      .finally(() => setTemplateDetailLoading(false))
  }

  // --- Render ---

  const renderStatus = () => {
    if (detailsLoading) {
      return (
        <div className="flex items-center gap-2 text-xs" style={{ color: 'var(--text-muted)' }}>
          <Loader2 size={14} className="animate-spin" /> Checking runtime...
        </div>
      )
    }
    if (!details) return null

    if (details.platform === 'unsupported') {
      return (
        <div className="flex items-center gap-2 px-3 py-2 rounded-lg text-xs"
          style={{ background: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.3)' }}>
          <ShieldOff size={14} style={{ color: '#ef4444' }} />
          <span style={{ color: '#ef4444' }}>
            Platform not supported{details.reason ? ` \u2014 ${details.reason}` : ''}
          </span>
        </div>
      )
    }

    if (!details.incusAvailable) {
      return (
        <div className="flex items-center gap-2 px-3 py-2 rounded-lg text-xs"
          style={{ background: 'rgba(234, 179, 8, 0.1)', border: '1px solid rgba(234, 179, 8, 0.3)' }}>
          <Shield size={14} style={{ color: '#eab308' }} />
          <span style={{ color: '#eab308' }}>
            Incus not available \u2014 install with <code className="font-mono">sudo apt install incus</code>
          </span>
        </div>
      )
    }

    if (!details.baseTemplateExists) {
      return (
        <div className="flex items-center gap-2 px-3 py-2 rounded-lg text-xs"
          style={{ background: 'rgba(234, 179, 8, 0.1)', border: '1px solid rgba(234, 179, 8, 0.3)' }}>
          <Shield size={14} style={{ color: '#eab308' }} />
          <span style={{ color: '#eab308' }}>
            Base template not initialized \u2014 run the Setup Wizard to create it
          </span>
        </div>
      )
    }

    return (
      <div className="px-3 py-2 rounded-lg text-xs space-y-1"
        style={{ background: 'rgba(34, 197, 94, 0.05)', border: '1px solid rgba(34, 197, 94, 0.2)' }}>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Shield size={14} style={{ color: '#22c55e' }} />
            <span style={{ color: '#22c55e' }}>Runtime ready</span>
          </div>
          <button onClick={loadDetails} className="p-1 rounded hover:bg-white/10 transition-colors"
            title="Refresh status">
            <RefreshCw size={12} style={{ color: 'var(--text-muted)' }} />
          </button>
        </div>
        <div className="flex flex-wrap gap-x-4 gap-y-0.5 pl-5" style={{ color: 'var(--text-muted)' }}>
          {details.incus_version && <span>Incus {details.incus_version}</span>}
          {details.storage_backend && <span>Storage: {details.storage_backend}</span>}
          <span>Overlay: {details.overlay_ready ? 'ready' : 'not configured'}</span>
          <span>{details.template_count} template{details.template_count !== 1 ? 's' : ''}</span>
          <span>{details.container_count} container{details.container_count !== 1 ? 's' : ''}</span>
          {details.orphan_count > 0 && (
            <span style={{ color: '#f59e0b' }}>{details.orphan_count} orphan{details.orphan_count !== 1 ? 's' : ''}</span>
          )}
        </div>
      </div>
    )
  }

  const renderContainers = () => {
    if (!details?.incusAvailable) return null

    const containers = containerData?.containers || []
    const orphans = containerData?.orphans || []

    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <h4 className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
              Session Containers
            </h4>
            {containers.length > 0 && (
              <span className="text-xs px-1.5 py-0.5 rounded"
                style={{ background: 'rgba(107, 114, 128, 0.15)', color: 'var(--text-muted)' }}>
                {containers.length}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            {orphans.length > 0 && (
              <button
                onClick={() => setPruneConfirm(true)}
                className="flex items-center gap-1 px-2 py-1 rounded text-xs transition-colors"
                style={{ background: 'rgba(239, 68, 68, 0.1)', color: '#f87171', border: '1px solid rgba(239, 68, 68, 0.2)' }}
              >
                <Trash2 size={12} />
                Prune {orphans.length} orphan{orphans.length !== 1 ? 's' : ''}
              </button>
            )}
            <button onClick={loadContainers} className="p-1.5 rounded transition-colors hover:bg-white/10"
              title="Refresh" disabled={containersLoading}>
              <RefreshCw size={14} className={containersLoading ? 'animate-spin' : ''}
                style={{ color: 'var(--text-muted)' }} />
            </button>
          </div>
        </div>

        {/* Prune confirmation */}
        {pruneConfirm && (
          <div className="p-3 rounded-lg text-sm"
            style={{ background: 'rgba(239, 68, 68, 0.05)', border: '1px solid rgba(239, 68, 68, 0.2)' }}>
            <p className="font-medium mb-2" style={{ color: 'var(--text-primary)' }}>
              Prune {orphans.length} orphaned container{orphans.length !== 1 ? 's' : ''}?
            </p>
            <p className="text-xs mb-3" style={{ color: 'var(--text-muted)' }}>
              This will destroy containers whose sessions no longer exist. This cannot be undone.
            </p>
            <div className="flex gap-2">
              <button onClick={handlePrune}
                disabled={actionLoading === 'prune'}
                className="flex items-center gap-1 px-3 py-1.5 rounded text-xs font-medium text-white"
                style={{ background: '#ef4444' }}>
                {actionLoading === 'prune' ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
                Prune
              </button>
              <button onClick={() => setPruneConfirm(false)}
                className="px-3 py-1.5 rounded text-xs"
                style={{ color: 'var(--text-secondary)', border: '1px solid var(--border-color)' }}>
                Cancel
              </button>
            </div>
          </div>
        )}

        {actionError && (
          <div className="flex items-center gap-1 text-xs" style={{ color: '#ef4444' }}>
            <AlertCircle size={12} /> {actionError}
          </div>
        )}

        {/* Container list */}
        {containers.length === 0 && !containersLoading ? (
          <div className="flex flex-col items-center justify-center py-6 rounded-lg border border-dashed"
            style={{ borderColor: 'var(--border-color)' }}>
            <Server size={32} className="mb-2" style={{ color: 'var(--text-muted)', opacity: 0.3 }} />
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>No session containers</p>
          </div>
        ) : (
          <div className="space-y-1">
            {containers.map(c => (
              <div key={c.session_id}
                className="flex items-center justify-between px-3 py-2 rounded-lg group"
                style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
                <div className="flex-1 min-w-0 mr-3">
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-xs truncate" style={{ color: 'var(--text-primary)' }}>
                      {c.name}
                    </span>
                    <StatusBadge status={c.status} />
                    <TemplateBadge name={c.template || 'base'} />
                  </div>
                  <div className="flex items-center gap-2 mt-0.5">
                    <span className="text-xs font-mono truncate" style={{ color: 'var(--text-muted)' }}>
                      {c.session_id.length > 12 ? c.session_id.slice(0, 12) + '...' : c.session_id}
                    </span>
                    <span className="text-xs" style={{ color: 'var(--text-muted)' }}>{c.created}</span>
                  </div>
                </div>
                {deleteConfirm === c.session_id ? (
                  <div className="flex items-center gap-1 flex-shrink-0">
                    <button onClick={() => handleDeleteContainer(c.session_id)}
                      disabled={actionLoading === c.session_id}
                      className="px-2 py-1 rounded text-xs font-medium text-white"
                      style={{ background: '#ef4444' }}>
                      {actionLoading === c.session_id ? <Loader2 size={12} className="animate-spin" /> : 'Delete'}
                    </button>
                    <button onClick={() => setDeleteConfirm(null)}
                      className="px-2 py-1 rounded text-xs"
                      style={{ color: 'var(--text-muted)' }}>
                      Cancel
                    </button>
                  </div>
                ) : (
                  <button onClick={() => setDeleteConfirm(c.session_id)}
                    className="p-1.5 rounded opacity-0 group-hover:opacity-100 hover:bg-red-500/20 transition-all flex-shrink-0"
                    title="Delete container">
                    <Trash2 size={14} className="text-red-400" />
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    )
  }

  const renderTemplates = () => {
    if (!details?.incusAvailable) return null

    const templates = templateData?.templates || []

    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <h4 className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
              Templates
            </h4>
            {templates.length > 0 && (
              <span className="text-xs px-1.5 py-0.5 rounded"
                style={{ background: 'rgba(107, 114, 128, 0.15)', color: 'var(--text-muted)' }}>
                {templates.length}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button onClick={() => setShowCreateForm(!showCreateForm)}
              className="flex items-center gap-1 px-2 py-1 rounded text-xs font-medium text-white transition-all hover:scale-[1.02]"
              style={saveButtonStyle}>
              <Plus size={12} /> Create
            </button>
            <button onClick={handleRefresh}
              disabled={templateActionLoading === 'refresh'}
              className="flex items-center gap-1 px-2 py-1 rounded text-xs transition-colors"
              style={{ color: 'var(--text-secondary)', border: '1px solid var(--border-color)' }}
              title="Refresh binary in all templates">
              {templateActionLoading === 'refresh'
                ? <Loader2 size={12} className="animate-spin" />
                : <RefreshCw size={12} />}
              Refresh Binary
            </button>
            <button onClick={loadTemplates} className="p-1.5 rounded transition-colors hover:bg-white/10"
              title="Reload list" disabled={templatesLoading}>
              <RefreshCw size={14} className={templatesLoading ? 'animate-spin' : ''}
                style={{ color: 'var(--text-muted)' }} />
            </button>
          </div>
        </div>

        {/* Create form */}
        {showCreateForm && (
          <div className="p-3 rounded-lg space-y-2"
            style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <input type="text" value={createName}
              onChange={e => setCreateName(e.target.value)}
              placeholder="Template name"
              className="w-full px-3 py-1.5 rounded text-sm"
              style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border-color)', color: 'var(--text-primary)' }}
            />
            <input type="text" value={createDesc}
              onChange={e => setCreateDesc(e.target.value)}
              placeholder="Description (optional)"
              className="w-full px-3 py-1.5 rounded text-sm"
              style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border-color)', color: 'var(--text-primary)' }}
            />
            <div className="flex gap-2">
              <button onClick={handleCreateTemplate}
                disabled={!createName.trim() || templateActionLoading === 'create'}
                className="flex items-center gap-1 px-3 py-1.5 rounded text-xs font-medium text-white disabled:opacity-50"
                style={saveButtonStyle}>
                {templateActionLoading === 'create' ? <Loader2 size={12} className="animate-spin" /> : <Plus size={12} />}
                Create from @base
              </button>
              <button onClick={() => { setShowCreateForm(false); setCreateName(''); setCreateDesc('') }}
                className="px-3 py-1.5 rounded text-xs"
                style={{ color: 'var(--text-secondary)', border: '1px solid var(--border-color)' }}>
                Cancel
              </button>
            </div>
          </div>
        )}

        {templateError && (
          <div className="flex items-center gap-1 text-xs" style={{ color: '#ef4444' }}>
            <AlertCircle size={12} /> {templateError}
          </div>
        )}

        {/* Promote confirmation */}
        {promoteConfirm && (
          <div className="p-3 rounded-lg text-sm"
            style={{ background: 'rgba(234, 179, 8, 0.05)', border: '1px solid rgba(234, 179, 8, 0.2)' }}>
            <p className="font-medium mb-2" style={{ color: 'var(--text-primary)' }}>
              Promote &quot;{promoteConfirm}&quot; to @base?
            </p>
            <p className="text-xs mb-3" style={{ color: 'var(--text-muted)' }}>
              This will replace the current @base template. All new sessions will use this template. This cannot be undone.
            </p>
            <div className="flex gap-2">
              <button onClick={() => handlePromote(promoteConfirm)}
                disabled={templateActionLoading === `promote-${promoteConfirm}`}
                className="flex items-center gap-1 px-3 py-1.5 rounded text-xs font-medium text-white"
                style={{ background: '#f59e0b' }}>
                {templateActionLoading === `promote-${promoteConfirm}`
                  ? <Loader2 size={12} className="animate-spin" />
                  : <ArrowUpCircle size={12} />}
                Promote
              </button>
              <button onClick={() => setPromoteConfirm(null)}
                className="px-3 py-1.5 rounded text-xs"
                style={{ color: 'var(--text-secondary)', border: '1px solid var(--border-color)' }}>
                Cancel
              </button>
            </div>
          </div>
        )}

        {/* Template delete confirmation */}
        {templateDeleteConfirm && (
          <div className="p-3 rounded-lg text-sm"
            style={{ background: 'rgba(239, 68, 68, 0.05)', border: '1px solid rgba(239, 68, 68, 0.2)' }}>
            <p className="font-medium mb-2" style={{ color: 'var(--text-primary)' }}>
              Delete template &quot;{templateDeleteConfirm}&quot;?
            </p>
            <p className="text-xs mb-3" style={{ color: 'var(--text-muted)' }}>
              This will remove the template container and all its data. This cannot be undone.
            </p>
            <div className="flex gap-2">
              <button onClick={() => handleDeleteTemplate(templateDeleteConfirm)}
                disabled={templateActionLoading === templateDeleteConfirm}
                className="flex items-center gap-1 px-3 py-1.5 rounded text-xs font-medium text-white"
                style={{ background: '#ef4444' }}>
                {templateActionLoading === templateDeleteConfirm
                  ? <Loader2 size={12} className="animate-spin" />
                  : <Trash2 size={12} />}
                Delete
              </button>
              <button onClick={() => setTemplateDeleteConfirm(null)}
                className="px-3 py-1.5 rounded text-xs"
                style={{ color: 'var(--text-secondary)', border: '1px solid var(--border-color)' }}>
                Cancel
              </button>
            </div>
          </div>
        )}

        {/* Template list */}
        {templates.length === 0 && !templatesLoading ? (
          <div className="flex flex-col items-center justify-center py-6 rounded-lg border border-dashed"
            style={{ borderColor: 'var(--border-color)' }}>
            <Box size={32} className="mb-2" style={{ color: 'var(--text-muted)', opacity: 0.3 }} />
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
              No templates \u2014 run the Setup Wizard to create the base template
            </p>
          </div>
        ) : (
          <div className="space-y-2">
            {templates.map(t => {
              const isBase = t.name === 'base'
              const isExpanded = expandedTemplate === t.name
              return (
                <div key={t.name} className="rounded-lg border transition-all"
                  style={{
                    background: 'var(--bg-secondary)',
                    borderColor: isExpanded ? 'rgba(168, 85, 247, 0.4)' : 'var(--border-color)'
                  }}>
                  {/* Header */}
                  <div className="px-3 py-2 flex items-center gap-3">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                          @{t.name}
                        </span>
                        {isBase && (
                          <span className="text-xs px-1.5 py-0.5 rounded"
                            style={{ background: 'rgba(168, 85, 247, 0.15)', color: '#a855f7' }}>
                            default
                          </span>
                        )}
                        {t.fleet_plans?.length > 0 && t.fleet_plans.map(p => (
                          <span key={p} className="text-xs px-1.5 py-0.5 rounded"
                            style={{ background: 'rgba(6, 182, 212, 0.15)', color: '#06b6d4' }}>
                            {p}
                          </span>
                        ))}
                      </div>
                      <div className="flex items-center gap-3 mt-0.5 text-xs" style={{ color: 'var(--text-muted)' }}>
                        {t.description && <span className="truncate max-w-[200px]">{t.description}</span>}
                        <span>Created: {t.created}</span>
                        {t.last_snapshot && <span>Snapshot: {t.last_snapshot}</span>}
                      </div>
                    </div>
                    {/* Actions */}
                    <div className="flex items-center gap-1 flex-shrink-0">
                      <button onClick={() => handleExpandTemplate(t.name)}
                        className="p-1.5 rounded transition-colors hover:bg-white/10"
                        title="Details">
                        {isExpanded ? <ChevronDown size={14} style={{ color: '#a855f7' }} /> : <Eye size={14} style={{ color: 'var(--text-muted)' }} />}
                      </button>
                      <button onClick={() => handleSnapshot(t.name)}
                        disabled={!!templateActionLoading}
                        className="p-1.5 rounded transition-colors hover:bg-white/10"
                        title="Snapshot">
                        {templateActionLoading === `snapshot-${t.name}`
                          ? <Loader2 size={14} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
                          : <Camera size={14} style={{ color: 'var(--text-muted)' }} />}
                      </button>
                      {!isBase && (
                        <>
                          <button onClick={() => setPromoteConfirm(t.name)}
                            disabled={!!templateActionLoading}
                            className="p-1.5 rounded transition-colors hover:bg-white/10"
                            title="Promote to @base">
                            <ArrowUpCircle size={14} style={{ color: 'var(--text-muted)' }} />
                          </button>
                          <button onClick={() => setTemplateDeleteConfirm(t.name)}
                            disabled={!!templateActionLoading}
                            className="p-1.5 rounded transition-colors hover:bg-red-500/20"
                            title="Delete template">
                            <Trash2 size={14} className="text-red-400" />
                          </button>
                        </>
                      )}
                    </div>
                  </div>
                  {/* Expanded detail */}
                  {isExpanded && (
                    <div className="px-3 pb-3 pt-1 border-t text-xs space-y-1"
                      style={{ borderColor: 'var(--border-color)' }}>
                      {templateDetailLoading ? (
                        <div className="flex items-center gap-2 py-2" style={{ color: 'var(--text-muted)' }}>
                          <Loader2 size={12} className="animate-spin" /> Loading details...
                        </div>
                      ) : templateDetail ? (
                        <div className="grid grid-cols-2 gap-x-4 gap-y-1" style={{ color: 'var(--text-muted)' }}>
                          <span>Container:</span>
                          <span className="font-mono">{templateDetail.container_name}</span>
                          <span>Status:</span>
                          <span>{templateDetail.container_status}</span>
                          <span>Snapshot:</span>
                          <span>{templateDetail.snapshot_ready ? 'Ready (cloneable)' : 'Not ready'}</span>
                          {templateDetail.based_on && (
                            <>
                              <span>Based on:</span>
                              <span>@{templateDetail.based_on}</span>
                            </>
                          )}
                          {templateDetail.binary_hash && (
                            <>
                              <span>Binary hash:</span>
                              <span className="font-mono">{templateDetail.binary_hash}</span>
                            </>
                          )}
                        </div>
                      ) : (
                        <span style={{ color: 'var(--text-muted)' }}>Failed to load details</span>
                      )}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="max-w-2xl space-y-6">
      {/* Status */}
      {renderStatus()}

      {/* Enabled Toggle */}
      <div className="flex items-center justify-between">
        <div>
          <label className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
            Sandbox Isolation
          </label>
          <p className="text-xs mt-0.5" style={hintStyle}>
            When enabled, all tool execution runs inside isolated containers. When disabled, tools execute directly on the host.
          </p>
        </div>
        <button
          onClick={() => setForm({ ...form, enabled: !form.enabled })}
          className="relative w-11 h-6 rounded-full transition-colors"
          style={{
            background: form.enabled ? '#a855f7' : 'var(--bg-tertiary)',
            border: `1px solid ${form.enabled ? '#a855f7' : 'var(--border-color)'}`
          }}
        >
          <span
            className="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform bg-white"
            style={{ transform: form.enabled ? 'translateX(20px)' : 'translateX(0)' }}
          />
        </button>
      </div>

      {!form.enabled && (
        <div className="flex items-start gap-2 p-3 rounded-lg text-xs"
          style={{ background: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.3)' }}>
          <AlertCircle size={14} className="mt-0.5 flex-shrink-0" style={{ color: '#ef4444' }} />
          <span style={{ color: '#ef4444' }}>
            Sandbox is disabled. AI tools will execute directly on your host system with full access to files, network, and system resources.
          </span>
        </div>
      )}

      {/* Resource Limits */}
      {form.enabled && (
        <div className="pt-4 border-t" style={sectionBorderStyle}>
          <div className="mb-4">
            <h4 className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
              Resource Limits
            </h4>
            <p className="text-xs mt-0.5" style={hintStyle}>
              Limits applied to each session container. Changes apply to new containers only.
            </p>
          </div>
          <div className="grid grid-cols-3 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Memory
              </label>
              <input
                type="text"
                value={form.memory}
                onChange={(e) => setForm({ ...form, memory: e.target.value })}
                placeholder="2GB"
                className={inputClass}
                style={inputStyle}
              />
              <p className="text-xs mt-1" style={hintStyle}>e.g. 2GB, 4GB, 512MB</p>
            </div>
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                CPU Cores
              </label>
              <input
                type="number"
                value={form.cpu}
                onChange={(e) => setForm({ ...form, cpu: parseInt(e.target.value) || 0 })}
                min="1"
                max="64"
                className={inputClass}
                style={inputStyle}
              />
              <p className="text-xs mt-1" style={hintStyle}>Default: 2</p>
            </div>
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Max Processes
              </label>
              <input
                type="number"
                value={form.processes}
                onChange={(e) => setForm({ ...form, processes: parseInt(e.target.value) || 0 })}
                min="50"
                max="10000"
                className={inputClass}
                style={inputStyle}
              />
              <p className="text-xs mt-1" style={hintStyle}>Default: 500</p>
            </div>
          </div>
        </div>
      )}

      {/* Save */}
      <div className="flex items-center gap-3">
        <button onClick={handleSave} disabled={saving}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-white font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
          style={saveButtonStyle}>
          <Save size={16} />
          {saving ? 'Saving...' : 'Save Changes'}
        </button>
        {saveSuccess && (
          <span className="flex items-center gap-1 text-green-400 text-sm">
            <Check size={16} /> Saved
          </span>
        )}
        {saveError && (
          <span className="flex items-center gap-1 text-sm" style={{ color: 'var(--danger)' }}>
            <AlertCircle size={16} /> {saveError}
          </span>
        )}
      </div>

      {/* Session Containers section */}
      {details?.incusAvailable && (
        <div className="pt-4 border-t" style={{ borderColor: 'var(--border-color)' }}>
          {renderContainers()}
        </div>
      )}

      {/* Templates section */}
      {details?.incusAvailable && (
        <div className="pt-4 border-t" style={{ borderColor: 'var(--border-color)' }}>
          {renderTemplates()}
        </div>
      )}
    </div>
  )
}
