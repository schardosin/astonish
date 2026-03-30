import { useState, useEffect, useCallback } from 'react'
import {
  Crosshair, Loader, Clock, AlertCircle, Tag, ChevronLeft, Code,
} from 'lucide-react'
import {
  fetchDrill, fetchDrillYaml, saveDrillYaml,
} from '../../api/drillApi'
import type { DrillDetail } from '../../api/drillApi'
import { buildPath } from '../../hooks/useHashRouter'
import YamlDrawer from '../YamlDrawer'
import { StepCard } from './DrillCards'

// ─── Drill Detail ───

interface DrillDetailProps {
  suiteKey: string
  drillKey: string
  onNavigate: (path: string) => void
  theme: 'dark' | 'light'
}

export default function DrillDetailView({ suiteKey, drillKey, onNavigate, theme }: DrillDetailProps) {
  const [drill, setDrill] = useState<DrillDetail | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showYaml, setShowYaml] = useState(false)
  const [yamlContent, setYamlContent] = useState('')
  const [isSaving, setIsSaving] = useState(false)
  const [saveStatus, setSaveStatus] = useState<'saved' | 'error' | null>(null)

  const loadDrill = useCallback(async () => {
    try {
      const [drillData, yamlData] = await Promise.all([
        fetchDrill(suiteKey, drillKey),
        fetchDrillYaml(suiteKey, drillKey),
      ])
      setDrill(drillData)
      setYamlContent(yamlData)
    } catch (err: any) {
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
    } catch (err: any) {
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

  const nodes = Array.isArray(drill.nodes) ? drill.nodes as Record<string, any>[] : []

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
                {drill.tags.map((tag: string) => (
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
              {nodes.map((node: Record<string, any>, idx: number) => (
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
