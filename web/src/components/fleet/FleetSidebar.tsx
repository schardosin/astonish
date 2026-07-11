import { useState } from 'react'
import {
  Search, ChevronDown, ChevronRight, Loader,
  Trash2, Radio, FileText, Eye,
  Copy, Plus, Settings2,
} from 'lucide-react'
import { cloneFleet, cloneSetupProfile, deleteFleet, deleteSetupProfile, fetchFleetPlanYaml, saveFleet, saveFleetPlanYaml } from '../../api/fleetChat'
import type { FleetPlanSummary, FleetDefinition, SetupProfileSummary } from '../../api/fleetChat'
import type { FleetChatSession, SidebarItem, SelectedItem } from './fleetUtils'
import { formatTimeAgo } from './fleetUtils'
import FleetTemplateDialog from './FleetTemplateDialog'
import type { FleetTemplateDialogMode } from './FleetTemplateDialog'
import SetupProfileDialog from './SetupProfileDialog'
import type { SetupProfileDialogMode } from './SetupProfileDialog'

interface FleetSidebarProps {
  plans: FleetPlanSummary[]
  sessions: FleetChatSession[]
  templates: FleetDefinition[]
  setupProfiles: SetupProfileSummary[]
  selectedItem: SelectedItem | null
  onSelect: (item: SelectedItem) => void
  onDeleteSession: (id: string) => void
  onSearch: (query: string) => void
  searchQuery: string
  isLoading: boolean
  theme: string
  onRefresh?: () => Promise<void> | void
}

interface TemplateDialogState {
  mode: FleetTemplateDialogMode
  source?: FleetDefinition
}

interface SetupProfileDialogState {
  mode: SetupProfileDialogMode
  source?: SetupProfileSummary
}

export default function FleetSidebar({
  plans, sessions, templates, setupProfiles, selectedItem, onSelect, onDeleteSession, onSearch, searchQuery,
  isLoading, theme, onRefresh,
}: FleetSidebarProps) {
  const [collapsedSections, setCollapsedSections] = useState<Record<string, boolean>>({})
  const [dialog, setDialog] = useState<TemplateDialogState | null>(null)
  const [profileDialog, setProfileDialog] = useState<SetupProfileDialogState | null>(null)
  const [dialogSubmitting, setDialogSubmitting] = useState(false)
  const [dialogError, setDialogError] = useState<string | null>(null)
  const [profileDialogSubmitting, setProfileDialogSubmitting] = useState(false)
  const [profileDialogError, setProfileDialogError] = useState<string | null>(null)

  const toggleSection = (key: string) => {
    setCollapsedSections(prev => ({ ...prev, [key]: !prev[key] }))
  }

  const renderSection = (title: string, icon: React.ReactNode, items: SidebarItem[], type: string, badgeColor: string | null) => {
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
              const selectType = type.startsWith('template') ? 'template' : type.startsWith('setup') ? 'setup-profile' : type
              const isSelected = selectedItem?.type === selectType && selectedItem?.key === item.key
              return (
                <button
                  key={`${type}-${item.key}`}
                  onClick={() => onSelect({ type: selectType, key: item.key })}
                  className={`group w-full text-left px-4 py-2.5 transition-colors ${
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
                    {item.onDelete && (
                      <button
                        onClick={(e: React.MouseEvent) => { e.stopPropagation(); item.onDelete!() }}
                        className="p-1 rounded opacity-0 group-hover:opacity-100 hover:bg-red-500/20 transition-all shrink-0"
                        title="Delete"
                      >
                        <Trash2 size={12} className="text-red-400" />
                      </button>
                    )}
                    {item.onClone && (
                      <button
                        onClick={(e: React.MouseEvent) => { e.stopPropagation(); item.onClone!() }}
                        className="p-1 rounded opacity-0 group-hover:opacity-100 hover:bg-cyan-500/20 transition-all shrink-0"
                        title="Clone template"
                      >
                        <Copy size={12} className="text-cyan-400" />
                      </button>
                    )}
                    {item.onExportYaml && (
                      <button
                        onClick={(e: React.MouseEvent) => { e.stopPropagation(); item.onExportYaml!() }}
                        className="p-1 rounded opacity-0 group-hover:opacity-100 hover:bg-white/10 transition-all shrink-0"
                        title="Export YAML"
                      >
                        <FileText size={12} className="text-cyan-400" />
                      </button>
                    )}
                    {item.onImportYaml && (
                      <button
                        onClick={(e: React.MouseEvent) => { e.stopPropagation(); item.onImportYaml!() }}
                        className="p-1 rounded opacity-0 group-hover:opacity-100 hover:bg-white/10 transition-all shrink-0"
                        title="Import YAML"
                      >
                        <Plus size={12} className="text-cyan-400" />
                      </button>
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

  const openCloneDialog = (t: FleetDefinition) => {
    setDialogError(null)
    setDialog({ mode: 'clone', source: t })
  }

  const openCreateDialog = () => {
    setDialogError(null)
    setDialog({ mode: 'create' })
  }

  const openCreateProfileDialog = () => {
    setProfileDialogError(null)
    setProfileDialog({ mode: 'create' })
  }

  const openCloneProfileDialog = (p: SetupProfileSummary) => {
    setProfileDialogError(null)
    setProfileDialog({ mode: 'clone', source: p })
  }

  const handleProfileDialogSubmit = async ({ key, name }: { key: string; name: string }) => {
    if (!profileDialog) return
    setProfileDialogSubmitting(true)
    setProfileDialogError(null)
    try {
      if (profileDialog.mode === 'clone' && profileDialog.source) {
        const result = await cloneSetupProfile(profileDialog.source.key, key, name)
        setProfileDialog(null)
        await onRefresh?.()
        onSelect({ type: 'setup-profile', key: result.key })
      } else {
        const result = await cloneSetupProfile('generic', key, name)
        setProfileDialog(null)
        await onRefresh?.()
        onSelect({ type: 'setup-profile', key: result.key })
      }
    } catch (err) {
      setProfileDialogError(err instanceof Error ? err.message : String(err))
    } finally {
      setProfileDialogSubmitting(false)
    }
  }

  const handleDialogSubmit = async ({ key, name }: { key: string; name: string }) => {
    if (!dialog) return
    setDialogSubmitting(true)
    setDialogError(null)
    try {
      if (dialog.mode === 'clone' && dialog.source) {
        const result = await cloneFleet(dialog.source.key, key, name)
        setDialog(null)
        await onRefresh?.()
        onSelect({ type: 'template', key: result.key })
      } else {
        await saveFleet(key, {
          name,
          description: '',
          setup_profile: 'generic',
          agents: {
            agent: {
              name: 'Agent',
              identity: 'You are a helpful agent.',
              behaviors: 'Follow the user instructions carefully.',
              tools: true,
            },
          },
          communication: {
            flow: [{ role: 'agent', talks_to: ['customer'], entry_point: true }],
          },
          settings: { max_turns_per_agent: 20, max_parallel_agents: 1 },
        })
        setDialog(null)
        await onRefresh?.()
        window.location.hash = `/fleet/template/${encodeURIComponent(key)}/agents`
        onSelect({ type: 'template', key })
      }
    } catch (err) {
      setDialogError(err instanceof Error ? err.message : String(err))
    } finally {
      setDialogSubmitting(false)
    }
  }

  const planItems: SidebarItem[] = plans.map(p => ({
    key: p.key,
    name: p.name,
    subtitle: `${p.created_from || 'custom'} | ${p.agent_names?.join(', ') || ''}`,
    activated: false,
    badge: p.channel_type !== 'chat' ? p.channel_type : null,
    onExportYaml: async () => {
      try {
        const yaml = await fetchFleetPlanYaml(p.key)
        const blob = new Blob([yaml], { type: 'text/yaml' })
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = `${p.key}.yaml`
        a.click()
        URL.revokeObjectURL(url)
      } catch (err) {
        alert('Export failed: ' + (err instanceof Error ? err.message : String(err)))
      }
    },
    onImportYaml: async () => {
      const yaml = window.prompt(`Paste YAML for plan "${p.name}":`)
      if (!yaml) return
      try {
        await saveFleetPlanYaml(p.key, yaml)
        await onRefresh?.()
      } catch (err) {
        alert('Import failed: ' + (err instanceof Error ? err.message : String(err)))
      }
    },
  }))

  const sessionItems: SidebarItem[] = sessions.map(s => ({
    key: s.id,
    name: s.title || `Session ${s.id.slice(0, 8)}`,
    subtitle: `${s.id.slice(0, 8)} | ${s.issueNumber ? `#${s.issueNumber}` : ''} ${formatTimeAgo(s.updatedAt)}`.trim(),
    badge: null,
    onDelete: () => onDeleteSession(s.id),
  }))

  const bundledTemplates = templates.filter(t => t.source === 'bundled')
  const customTemplates = templates.filter(t => t.source !== 'bundled')

  const bundledItems: SidebarItem[] = bundledTemplates.map(t => ({
    key: t.key,
    name: t.name,
    subtitle: `${t.key} · ${t.agent_count} agents`,
    badge: 'Bundled',
    onClone: () => openCloneDialog(t),
  }))

  const customItems: SidebarItem[] = customTemplates.map(t => ({
    key: t.key,
    name: t.name,
    subtitle: `${t.key} · ${t.agent_count} agents`,
    badge: null,
    onClone: () => openCloneDialog(t),
    onDelete: async () => {
      if (!window.confirm(`Delete template "${t.name}"?`)) return
      try {
        await deleteFleet(t.key)
        await onRefresh?.()
      } catch (err) {
        alert('Delete failed: ' + (err instanceof Error ? err.message : String(err)))
      }
    },
  }))

  const bundledSetupProfiles = setupProfiles.filter(p => p.source === 'bundled')
  const customSetupProfiles = setupProfiles.filter(p => p.source !== 'bundled')

  const bundledSetupItems: SidebarItem[] = bundledSetupProfiles.map(p => ({
    key: p.key,
    name: p.name,
    subtitle: `${p.step_count} steps · ${p.domain || p.key}`,
    badge: 'Bundled',
    onClone: () => openCloneProfileDialog(p),
  }))

  const customSetupItems: SidebarItem[] = customSetupProfiles.map(p => ({
    key: p.key,
    name: p.name,
    subtitle: `${p.step_count} steps · ${p.domain || p.key}`,
    badge: null,
    onClone: () => openCloneProfileDialog(p),
    onDelete: async () => {
      if (!window.confirm(`Delete setup profile "${p.name}"?`)) return
      try {
        await deleteSetupProfile(p.key)
        await onRefresh?.()
      } catch (err) {
        alert('Delete failed: ' + (err instanceof Error ? err.message : String(err)))
      }
    },
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
      <div className="px-3 py-3" style={{ borderBottom: '1px solid var(--border-color)' }}>
        <button
          onClick={openCreateDialog}
          className="w-full mb-2 flex items-center justify-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg bg-cyan-600 hover:bg-cyan-500 text-white transition-colors"
        >
          <Plus size={12} /> New Template
        </button>
        <button
          onClick={openCreateProfileDialog}
          className="w-full mb-2 flex items-center justify-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg border transition-colors hover:bg-cyan-500/10"
          style={{ borderColor: 'var(--border-color)', color: 'var(--text-secondary)' }}
        >
          <Plus size={12} className="text-cyan-400" /> New Setup Profile
        </button>
        <div className="relative">
          <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2" style={{ color: 'var(--text-muted)' }} />
          <input
            type="text"
            value={searchQuery}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => onSearch(e.target.value)}
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

      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <div className="flex items-center justify-center py-12">
            <Loader size={18} className="animate-spin text-cyan-400" />
          </div>
        ) : (
          <>
            {renderSection('Fleet Plans', <FileText size={12} />, planItems, 'plan', 'rgba(6, 182, 212, 0.6)')}
            {renderSection('Active Sessions', <Radio size={12} />, sessionItems, 'session', null)}
            {renderSection('Bundled Setup Profiles', <Settings2 size={12} />, bundledSetupItems, 'setup-profile-bundled', 'rgba(6, 182, 212, 0.5)')}
            {renderSection('Your Setup Profiles', <Settings2 size={12} />, customSetupItems, 'setup-profile-custom', null)}
            {renderSection('Astonish Templates', <Eye size={12} />, bundledItems, 'template-bundled', 'rgba(6, 182, 212, 0.7)')}
            {renderSection('Your Templates', <Eye size={12} />, customItems, 'template-custom', null)}
          </>
        )}
      </div>

      <FleetTemplateDialog
        isOpen={dialog !== null}
        mode={dialog?.mode || 'create'}
        sourceName={dialog?.source?.name}
        sourceKey={dialog?.source?.key}
        submitting={dialogSubmitting}
        error={dialogError}
        onClose={() => { if (!dialogSubmitting) { setDialog(null); setDialogError(null) } }}
        onSubmit={handleDialogSubmit}
      />

      <SetupProfileDialog
        isOpen={profileDialog !== null}
        mode={profileDialog?.mode || 'create'}
        sourceName={profileDialog?.source?.name}
        sourceKey={profileDialog?.source?.key}
        submitting={profileDialogSubmitting}
        error={profileDialogError}
        onClose={() => { if (!profileDialogSubmitting) { setProfileDialog(null); setProfileDialogError(null) } }}
        onSubmit={handleProfileDialogSubmit}
      />
    </div>
  )
}
