import { useState, useEffect, useCallback } from 'react'
import { AppWindow, Trash2, Code, ArrowLeft, Clock, Sparkles } from 'lucide-react'
import { fetchApps, fetchApp, deleteApp, saveApp } from '../api/apps'
import type { AppListItem, VisualApp } from '../api/apps'
import AppPreview from './chat/AppPreview'
import CodeDrawer from './CodeDrawer'

interface AppsViewProps {
  theme: string
  onImproveApp?: (message: string, systemContext: string) => void
}

function EmptyState() {
  return (
    <div className="flex-1 flex items-center justify-center">
      <div className="text-center">
        <AppWindow size={48} className="mx-auto mb-4" style={{ color: 'rgba(16, 185, 129, 0.3)' }} />
        <h2 className="text-lg font-semibold mb-2" style={{ color: 'var(--text-primary)' }}>Visual Apps</h2>
        <p className="text-sm max-w-md" style={{ color: 'var(--text-muted)' }}>
          No apps saved yet. Generate a visual app in Chat by asking to build a
          dashboard, calculator, form, or any interactive UI — then click Save to
          keep it here.
        </p>
      </div>
    </div>
  )
}

function formatDate(dateStr: string) {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffMin = Math.floor(diffMs / 60000)
  if (diffMin < 1) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  const diffDay = Math.floor(diffHr / 24)
  if (diffDay < 7) return `${diffDay}d ago`
  return d.toLocaleDateString()
}

// eslint-disable-next-line @typescript-eslint/no-unused-vars
export default function AppsView({ theme, onImproveApp }: AppsViewProps) {
  const [apps, setApps] = useState<AppListItem[]>([])
  const [loading, setLoading] = useState(true)
  const [selectedApp, setSelectedApp] = useState<VisualApp | null>(null)
  const [showCode, setShowCode] = useState(false)
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null)
  const [codeContent, setCodeContent] = useState('')
  const [isSaving, setIsSaving] = useState(false)
  const [saveStatus, setSaveStatus] = useState<'saved' | 'error' | null>(null)

  const loadApps = useCallback(async () => {
    try {
      const data = await fetchApps()
      setApps(data.apps || [])
    } catch {
      // Ignore errors
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadApps()
  }, [loadApps])

  // Listen for apps-updated events (from chat save flow)
  useEffect(() => {
    const handler = () => loadApps()
    window.addEventListener('astonish:apps-updated', handler)
    return () => window.removeEventListener('astonish:apps-updated', handler)
  }, [loadApps])

  const handleOpenApp = useCallback(async (name: string) => {
    try {
      const app = await fetchApp(name)
      setSelectedApp(app)
      setCodeContent(app.code)
      setShowCode(false)
      setSaveStatus(null)
    } catch {
      // Ignore
    }
  }, [])

  const handleDeleteApp = useCallback(async (name: string) => {
    try {
      await deleteApp(name)
      setApps(prev => prev.filter(a => a.name !== name))
      if (selectedApp?.name === name) {
        setSelectedApp(null)
      }
    } catch {
      // Ignore
    }
    setDeleteConfirm(null)
  }, [selectedApp])

  const handleSaveCode = useCallback(async () => {
    if (!selectedApp) return
    setIsSaving(true)
    setSaveStatus(null)
    try {
      await saveApp(selectedApp.name, {
        description: selectedApp.description,
        code: codeContent,
        version: selectedApp.version,
        sessionId: selectedApp.sessionId,
      })
      setSelectedApp(prev => prev ? { ...prev, code: codeContent } : prev)
      setSaveStatus('saved')
      setTimeout(() => setSaveStatus(null), 3000)
    } catch {
      setSaveStatus('error')
    } finally {
      setIsSaving(false)
    }
  }, [selectedApp, codeContent])

  const handleImproveWithAI = useCallback(() => {
    if (!selectedApp || !onImproveApp) return
    const systemContext = [
      '## Active App Refinement\n',
      `The user wants to improve a saved visual app called "${selectedApp.name}" (version ${selectedApp.version}).`,
      'Apply the user\'s requested changes to the CURRENT source code below and output the COMPLETE updated component.',
      'You MUST output the full component inside an ```astonish-app code fence — do NOT output a diff or partial snippet.',
      'Preserve all existing functionality unless the user explicitly asks to remove it.',
      '',
      '### Current Source Code\n',
      '```jsx',
      selectedApp.code,
      '```',
      '',
      'First, show the current app by outputting it unchanged inside an ```astonish-app fence, then ask the user what they would like to change.',
    ].join('\n')
    onImproveApp('I want to improve this app.', systemContext)
  }, [selectedApp, onImproveApp])

  // Runner view
  if (selectedApp) {
    return (
      <div className="flex-1 flex flex-col overflow-hidden" style={{ background: 'var(--bg-primary)' }}>
        {/* Toolbar */}
        <div
          className="flex items-center gap-3 px-4 py-2 shrink-0"
          style={{ borderBottom: '1px solid var(--border-color)', background: 'var(--bg-secondary)' }}
        >
          <button
            onClick={() => setSelectedApp(null)}
            className="flex items-center gap-1 px-2 py-1 rounded text-xs transition-colors cursor-pointer"
            style={{ color: 'var(--text-secondary)' }}
          >
            <ArrowLeft size={14} />
            Back
          </button>

          <div className="flex-1 flex items-center gap-2 min-w-0">
            <AppWindow size={16} style={{ color: '#10b981', flexShrink: 0 }} />
            <span className="text-sm font-medium truncate" style={{ color: 'var(--text-primary)' }}>
              {selectedApp.name}
            </span>
            {selectedApp.version > 1 && (
              <span className="text-[10px] px-1.5 py-0.5 rounded-full shrink-0"
                style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
                v{selectedApp.version}
              </span>
            )}
          </div>

          {onImproveApp && (
            <button
              onClick={handleImproveWithAI}
              className="flex items-center gap-1 px-2 py-1 rounded text-xs transition-colors cursor-pointer"
              style={{ color: 'var(--text-muted)' }}
              title="Improve this app with AI in Chat"
            >
              <Sparkles size={14} />
              Improve with AI
            </button>
          )}

          <button
            onClick={() => setShowCode(!showCode)}
            className="flex items-center gap-1 px-2 py-1 rounded text-xs transition-colors cursor-pointer"
            style={{
              background: showCode ? 'rgba(6, 182, 212, 0.15)' : 'transparent',
              color: showCode ? '#22d3ee' : 'var(--text-muted)',
            }}
            title={showCode ? 'Hide code' : 'View code'}
          >
            <Code size={14} />
            {showCode ? 'Hide Code' : 'View Code'}
          </button>

          <button
            onClick={() => setDeleteConfirm(selectedApp.name)}
            className="flex items-center gap-1 px-2 py-1 rounded text-xs transition-colors cursor-pointer"
            style={{ color: 'var(--text-muted)' }}
            title="Delete this app"
          >
            <Trash2 size={14} />
          </button>
        </div>

        {/* Delete confirmation */}
        {deleteConfirm && (
          <div className="flex items-center gap-2 px-4 py-2 text-xs"
            style={{ background: 'rgba(239, 68, 68, 0.1)', borderBottom: '1px solid rgba(239, 68, 68, 0.2)' }}>
            <span style={{ color: '#ef4444' }}>Delete this app permanently?</span>
            <button
              onClick={() => handleDeleteApp(deleteConfirm)}
              className="px-2 py-0.5 rounded text-white cursor-pointer"
              style={{ background: '#ef4444' }}
            >
              Delete
            </button>
            <button
              onClick={() => setDeleteConfirm(null)}
              className="px-2 py-0.5 rounded cursor-pointer"
              style={{ color: 'var(--text-muted)', background: 'var(--bg-tertiary)' }}
            >
              Cancel
            </button>
          </div>
        )}

        {/* Content area */}
        <div className="flex-1 flex flex-col overflow-hidden">
          {/* Preview iframe */}
          <div className={`${showCode ? 'h-1/2' : 'flex-1'} overflow-hidden p-4`}>
            <AppPreview code={selectedApp.code} maxHeight={9999} appName={selectedApp.name} />
          </div>

          {/* Bottom code drawer */}
          {showCode && (
            <div className="h-1/2" style={{ borderTop: '1px solid var(--border-color)' }}>
              <CodeDrawer
                code={codeContent}
                onChange={(val) => setCodeContent(val)}
                onClose={() => setShowCode(false)}
                theme={theme === 'dark' ? 'dark' : 'light'}
                title={selectedApp.name}
                subtitle="JSX / React component"
                onSave={handleSaveCode}
                isSaving={isSaving}
                saveStatus={saveStatus}
              />
            </div>
          )}
        </div>
      </div>
    )
  }

  // List view
  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <span className="text-sm" style={{ color: 'var(--text-muted)' }}>Loading apps...</span>
      </div>
    )
  }

  if (apps.length === 0) {
    return <EmptyState />
  }

  return (
    <div className="flex-1 overflow-auto p-6" style={{ background: 'var(--bg-primary)' }}>
      <div className="max-w-5xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <AppWindow size={20} style={{ color: '#10b981' }} />
          <h1 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>Visual Apps</h1>
          <span className="text-xs px-2 py-0.5 rounded-full" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
            {apps.length}
          </span>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {apps.map(app => (
            <div
              key={app.name}
              className="group rounded-xl overflow-hidden cursor-pointer transition-all hover:shadow-lg"
              style={{
                border: '1px solid var(--border-color)',
                background: 'var(--bg-secondary)',
              }}
              onClick={() => handleOpenApp(app.name)}
            >
              <div className="p-4">
                <div className="flex items-start justify-between mb-2">
                  <div className="flex items-center gap-2 min-w-0">
                    <AppWindow size={16} style={{ color: '#10b981', flexShrink: 0 }} />
                    <span className="text-sm font-medium truncate" style={{ color: 'var(--text-primary)' }}>
                      {app.name}
                    </span>
                  </div>
                  {app.version > 1 && (
                    <span className="text-[10px] px-1.5 py-0.5 rounded-full shrink-0 ml-2"
                      style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
                      v{app.version}
                    </span>
                  )}
                </div>

                {app.description && app.description !== app.name && (
                  <p className="text-xs mb-3 line-clamp-2" style={{ color: 'var(--text-muted)' }}>
                    {app.description}
                  </p>
                )}

                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-1 text-[10px]" style={{ color: 'var(--text-muted)' }}>
                    <Clock size={10} />
                    <span>{formatDate(app.updatedAt)}</span>
                  </div>
                  <div className="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                    <button
                      onClick={(e) => {
                        e.stopPropagation()
                        setDeleteConfirm(app.name)
                      }}
                      className="p-1 rounded transition-colors cursor-pointer"
                      style={{ color: 'var(--text-muted)' }}
                      title="Delete app"
                    >
                      <Trash2 size={12} />
                    </button>
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>

        {/* Inline delete confirmation for list view */}
        {deleteConfirm && !selectedApp && (
          <div className="fixed inset-0 flex items-center justify-center z-50" style={{ background: 'rgba(0,0,0,0.5)' }}>
            <div className="rounded-xl p-6 max-w-sm w-full mx-4"
              style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
              <h3 className="text-sm font-semibold mb-2" style={{ color: 'var(--text-primary)' }}>Delete App</h3>
              <p className="text-xs mb-4" style={{ color: 'var(--text-muted)' }}>
                Are you sure you want to delete <strong>{deleteConfirm}</strong>? This cannot be undone.
              </p>
              <div className="flex gap-2 justify-end">
                <button
                  onClick={() => setDeleteConfirm(null)}
                  className="px-3 py-1.5 rounded text-xs cursor-pointer"
                  style={{ color: 'var(--text-secondary)', background: 'var(--bg-tertiary)' }}
                >
                  Cancel
                </button>
                <button
                  onClick={() => handleDeleteApp(deleteConfirm)}
                  className="px-3 py-1.5 rounded text-xs text-white cursor-pointer"
                  style={{ background: '#ef4444' }}
                >
                  Delete
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
