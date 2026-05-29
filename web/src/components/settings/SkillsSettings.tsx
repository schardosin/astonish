import { useState, useEffect, useCallback, useRef } from 'react'
import { Save, AlertCircle, Check, Plus, Trash2, ChevronRight, ChevronDown, Eye, Pencil, X, Loader2, CheckCircle, XCircle, FileText, FolderOpen } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'
import { teamFetch } from '../../api/teamContext'
import CodeMirror from '@uiw/react-codemirror'
import { markdown } from '@codemirror/lang-markdown'
import { languages } from '@codemirror/language-data'
import { javascript } from '@codemirror/lang-javascript'
import { python } from '@codemirror/lang-python'
import { search, searchKeymap, highlightSelectionMatches } from '@codemirror/search'
import { keymap, EditorView } from '@codemirror/view'

// --- Types ---

interface Skill {
  name: string
  description: string
  source: string
  scope?: string // "bundled", "org", "team"
  eligible: boolean
  editable: boolean
  require_bins?: string[]
  missing?: string[]
  has_directory?: boolean   // New: indicates the skill has auxiliary files
}

interface ActiveSkill {
  name: string
  content: string
  raw_file: string
  editable: boolean
  source: string
  scope?: string
  description?: string
  files?: Array<{ path: string; filename: string; size?: number; is_executable?: boolean }>
}

interface SkillsForm {
  enabled: boolean
  user_dir: string
  extra_dirs: string[]
  allowlist: string[]
}

interface SkillsSettingsProps {
  config: Record<string, any> | null
  onSaved?: () => void
  theme?: string
  /** Scope filter for API calls: "team", "org", or undefined (merged) */
  scope?: string
  /** Whether running in platform mode */
  isPlatform?: boolean
  /** Whether the user can manage skills in this context */
  canManage?: boolean
  /** Explicit team slug — overrides global active team for API calls */
  teamSlug?: string
}

// API helpers — use teamFetch for team header injection in platform mode
// teamSlug parameter overrides the global active team when provided
const fetchSkills = async (scope?: string, teamSlug?: string): Promise<{ skills: Skill[], is_team_admin: boolean, is_org_admin: boolean }> => {
  const params = scope ? `?scope=${scope}` : ''
  const res = await teamFetch(`/api/skills${params}`, undefined, teamSlug)
  if (!res.ok) throw new Error('Failed to fetch skills')
  return res.json()
}

const fetchSkillContent = async (name: string, scope?: string, teamSlug?: string): Promise<ActiveSkill> => {
  const params = scope ? `?scope=${scope}` : ''
  const [contentRes, filesRes] = await Promise.all([
    teamFetch(`/api/skills/${encodeURIComponent(name)}/content${params}`, undefined, teamSlug),
    teamFetch(`/api/skills/${encodeURIComponent(name)}/files${params}`, undefined, teamSlug)
  ])

  if (!contentRes.ok) throw new Error('Failed to fetch skill content')

  const data = await contentRes.json()

  // Attach files list if available
  if (filesRes.ok) {
    try {
      const filesData = await filesRes.json()
      if (filesData.files && Array.isArray(filesData.files)) {
        data.files = filesData.files
      }
    } catch {
      // ignore files fetch failure
    }
  }

  return data
}

const saveSkillContent = async (name: string, rawFile: string, scope?: string, teamSlug?: string): Promise<Record<string, any>> => {
  const params = scope ? `?scope=${scope}` : ''
  const res = await teamFetch(`/api/skills/${encodeURIComponent(name)}/content${params}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ raw_file: rawFile })
  }, teamSlug)
  if (!res.ok) {
    const data = await res.json().catch(() => null)
    throw new Error(data?.error || 'Failed to save skill')
  }
  return res.json()
}

const createSkillApi = async (name: string, scope?: string, teamSlug?: string): Promise<Record<string, any>> => {
  const res = await teamFetch('/api/skills', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, scope: scope || '' })
  }, teamSlug)
  if (!res.ok) {
    const data = await res.json().catch(() => null)
    throw new Error(data?.error || 'Failed to create skill')
  }
  return res.json()
}

const deleteSkillApi = async (name: string, scope?: string, teamSlug?: string): Promise<Record<string, any>> => {
  const params = scope ? `?scope=${scope}` : ''
  const res = await teamFetch(`/api/skills/${encodeURIComponent(name)}${params}`, {
    method: 'DELETE'
  }, teamSlug)
  if (!res.ok) {
    const data = await res.json().catch(() => null)
    throw new Error(data?.error || 'Failed to delete skill')
  }
  return res.json()
}

const deleteSkillFileApi = async (name: string, path: string, filename: string, scope?: string, teamSlug?: string): Promise<void> => {
  const params = scope ? `?scope=${scope}` : ''
  const res = await teamFetch(
    `/api/skills/${encodeURIComponent(name)}/file${params}&path=${encodeURIComponent(path)}&filename=${encodeURIComponent(filename)}`,
    { method: 'DELETE' },
    teamSlug
  )
  if (!res.ok) {
    const data = await res.json().catch(() => null)
    throw new Error(data?.error || 'Failed to delete file')
  }
}

// Scope badge colors
function ScopeBadge({ scope }: { scope: string }) {
  const colors: Record<string, { bg: string, fg: string }> = {
    team: { bg: 'rgba(34, 197, 94, 0.15)', fg: '#22c55e' },
    org: { bg: 'rgba(59, 130, 246, 0.15)', fg: '#3b82f6' },
    bundled: { bg: 'rgba(168, 85, 247, 0.15)', fg: '#a855f7' },
    custom: { bg: 'rgba(34, 197, 94, 0.15)', fg: '#22c55e' },
  }
  const c = colors[scope] || colors.custom
  return (
    <span className="text-[10px] px-1.5 py-0.5 rounded" style={{ background: c.bg, color: c.fg }}>
      {scope}
    </span>
  )
}

export default function SkillsSettings({ config, onSaved, theme = 'dark', scope, isPlatform = false, canManage, teamSlug }: SkillsSettingsProps) {
  // Config form state (only used in personal mode or org admin settings)
  const [form, setForm] = useState<SkillsForm>({
    enabled: true,
    user_dir: '',
    extra_dirs: [],
    allowlist: []
  })
  const [extraDirsText, setExtraDirsText] = useState('')
  const [allowlistText, setAllowlistText] = useState('')

  // Skills list state
  const [skillsList, setSkillsList] = useState<Skill[]>([])
  const [skillsLoading, setSkillsLoading] = useState(false)
  const [skillsError, setSkillsError] = useState<string | null>(null)
  const [isTeamAdmin, setIsTeamAdmin] = useState(false)
  const [isOrgAdmin, setIsOrgAdmin] = useState(false)

  // Editor state
  const [activeSkill, setActiveSkill] = useState<ActiveSkill | null>(null)
  const [editorContent, setEditorContent] = useState('')
  const [editorMode, setEditorMode] = useState<'view' | 'edit' | null>(null)
  const [editorLoading, setEditorLoading] = useState(false)
  const [editorSaving, setEditorSaving] = useState(false)
  const [editorError, setEditorError] = useState<string | null>(null)
  const [editorSuccess, setEditorSuccess] = useState(false)

  // Multi-file editor state
  const [currentFilePath, setCurrentFilePath] = useState('')      // '' for SKILL.md
  const [currentFilename, setCurrentFilename] = useState('SKILL.md')

  // Sidebar add-file form
  const [showAddFile, setShowAddFile] = useState(false)
  const [newFileDir, setNewFileDir] = useState<'scripts' | 'references' | 'templates' | ''>('scripts')
  const [newFileName, setNewFileName] = useState('')
  const [addFileError, setAddFileError] = useState<string | null>(null)

  // Inline delete confirmation for auxiliary files
  const [pendingDeleteFile, setPendingDeleteFile] = useState<{ path: string; filename: string } | null>(null)

  // Ref for the Add File popover (for click-outside dismissal)
  const addPopoverRef = useRef<HTMLDivElement>(null)

  // Choose CodeMirror language extension based on current file
  const getLanguageExtension = (filename: string) => {
    const lower = filename.toLowerCase()
    if (lower.endsWith('.sh') || lower.endsWith('.bash')) return []
    if (lower.endsWith('.py')) return python()
    if (lower.endsWith('.js') || lower.endsWith('.ts') || lower.endsWith('.jsx') || lower.endsWith('.tsx')) return javascript()
    if (lower.endsWith('.json')) return javascript()
    if (lower.endsWith('.md')) return markdown({ codeLanguages: languages })
    return markdown({ codeLanguages: languages })
  }

  // Create skill modal
  const [showCreate, setShowCreate] = useState(false)
  const [newSkillName, setNewSkillName] = useState('')
  const [createError, setCreateError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  // Delete confirm
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null)
  const [deleteScope, setDeleteScope] = useState<string | undefined>(undefined)

  // Config save state
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Determine if config section should be shown
  // Only in personal mode (no scope filter), or no scope provided
  const showConfig = !isPlatform && !scope

  // Effective management permission: from props, or derived from API response
  const effectiveCanManage = canManage !== undefined
    ? canManage
    : scope === 'team' ? isTeamAdmin : scope === 'org' ? isOrgAdmin : true

  useEffect(() => {
    if (config && showConfig) {
      setForm({
        enabled: config.enabled !== false,
        user_dir: config.user_dir || '',
        extra_dirs: config.extra_dirs || [],
        allowlist: config.allowlist || []
      })
      setExtraDirsText((config.extra_dirs || []).join(', '))
      setAllowlistText((config.allowlist || []).join(', '))
    }
  }, [config, showConfig])

  const loadSkills = useCallback(async () => {
    setSkillsLoading(true)
    setSkillsError(null)
    try {
      const data = await fetchSkills(scope, teamSlug)
      setSkillsList(data.skills || [])
      setIsTeamAdmin(data.is_team_admin ?? false)
      setIsOrgAdmin(data.is_org_admin ?? false)
    } catch (err: any) {
      setSkillsError(err.message)
    } finally {
      setSkillsLoading(false)
    }
  }, [scope, teamSlug])

  useEffect(() => {
    loadSkills()
  }, [loadSkills])

  // Close Add File popover on outside click or Escape
  useEffect(() => {
    if (!showAddFile) return

    const handleClickOutside = (e: MouseEvent) => {
      if (addPopoverRef.current && !addPopoverRef.current.contains(e.target as Node)) {
        // Also ignore clicks on the "+ Add" button itself (the toggle handles it)
        setShowAddFile(false)
        setAddFileError(null)
      }
    }

    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setShowAddFile(false)
        setAddFileError(null)
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    document.addEventListener('keydown', handleEscape)

    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
      document.removeEventListener('keydown', handleEscape)
    }
  }, [showAddFile])

  const handleSaveConfig = async () => {
    setSaving(true)
    setSaveSuccess(false)
    setError(null)
    try {
      const saveData = {
        ...form,
        extra_dirs: extraDirsText.split(',').map(s => s.trim()).filter(Boolean),
        allowlist: allowlistText.split(',').map(s => s.trim()).filter(Boolean)
      }
      await saveFullConfigSection('skills', saveData as unknown as Record<string, unknown>)
      setSaveSuccess(true)
      if (onSaved) onSaved()
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err: any) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const handleOpenSkill = async (name: string, mode: 'view' | 'edit', skillScope?: string) => {
    setEditorLoading(true)
    setEditorError(null)
    setEditorSuccess(false)
    setCurrentFilePath('')
    setCurrentFilename('SKILL.md')
    setShowAddFile(false)
    setPendingDeleteFile(null)
    setAddFileError(null)
    try {
      const data = await fetchSkillContent(name, skillScope || scope, teamSlug)
      setActiveSkill(data)
      setEditorContent(data.raw_file)
      setEditorMode(mode)
    } catch (err: any) {
      setEditorError(err.message)
    } finally {
      setEditorLoading(false)
    }
  }

  // Switch to editing a different file within the current skill (MVP multi-file support)
  const switchToFile = async (path: string, filename: string) => {
    if (!activeSkill) return

    setEditorLoading(true)
    setEditorError(null)

    try {
      const params = activeSkill.scope ? `?scope=${activeSkill.scope}` : ''
      const res = await teamFetch(
        `/api/skills/${encodeURIComponent(activeSkill.name)}/file${params}&path=${encodeURIComponent(path)}&filename=${encodeURIComponent(filename)}`,
        undefined,
        teamSlug
      )
      if (!res.ok) throw new Error('Failed to load file')

      const fileData = await res.json()
      setCurrentFilePath(path)
      setCurrentFilename(filename)
      setEditorContent(fileData.content || '')
    } catch (err: any) {
      setEditorError(err.message)
    } finally {
      setEditorLoading(false)
    }
  }

  // Add a new auxiliary file from the sidebar form
  const handleAddFile = async () => {
    if (!activeSkill || !newFileName.trim()) return
    setAddFileError(null)
    const p = newFileDir
    const fn = newFileName.trim()

    try {
      const params = activeSkill.scope ? `?scope=${activeSkill.scope}` : ''
      await teamFetch(
        `/api/skills/${encodeURIComponent(activeSkill.name)}/file${params}&path=${encodeURIComponent(p)}&filename=${encodeURIComponent(fn)}`,
        { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ content: '' }) },
        teamSlug
      )
      // Refresh
      const refreshed = await fetchSkillContent(activeSkill.name, activeSkill.scope || scope, teamSlug)
      setActiveSkill(refreshed)
      await switchToFile(p, fn)
      // Reset form + any pending delete
      setNewFileName('')
      setShowAddFile(false)
      setAddFileError(null)
      setPendingDeleteFile(null)
    } catch (e: any) {
      setAddFileError(e.message)
    }
  }

  // Delete an auxiliary file (called after inline confirmation)
  const handleDeleteFile = async (path: string, filename: string) => {
    if (!activeSkill) return
    try {
      await deleteSkillFileApi(activeSkill.name, path, filename, activeSkill.scope || scope, teamSlug)
      setPendingDeleteFile(null)

      // Refresh file list
      const refreshed = await fetchSkillContent(activeSkill.name, activeSkill.scope || scope, teamSlug)
      setActiveSkill(refreshed)

      // If we just deleted the file we were viewing, switch back to SKILL.md
      if (currentFilePath === path && currentFilename === filename) {
        setCurrentFilePath('')
        setCurrentFilename('SKILL.md')
        setEditorContent(activeSkill.raw_file || '')
      }
    } catch (e: any) {
      setEditorError(e.message)
      setPendingDeleteFile(null)
    }
  }

  const handleSaveSkill = async () => {
    if (!activeSkill) return
    setEditorSaving(true)
    setEditorError(null)
    setEditorSuccess(false)
    try {
      if (currentFilename === 'SKILL.md' && currentFilePath === '') {
        // Saving the main SKILL.md
        await saveSkillContent(activeSkill.name, editorContent, activeSkill.scope || scope, teamSlug)
      } else {
        // Saving an auxiliary file
        const params = activeSkill.scope ? `?scope=${activeSkill.scope}` : ''
        const res = await teamFetch(
          `/api/skills/${encodeURIComponent(activeSkill.name)}/file${params}&path=${encodeURIComponent(currentFilePath)}&filename=${encodeURIComponent(currentFilename)}`,
          {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              content: editorContent,
              is_executable: false // TODO: detect from file extension or UI
            })
          },
          teamSlug
        )
        if (!res.ok) {
          const errData = await res.json().catch(() => ({}))
          throw new Error(errData.error || 'Failed to save file')
        }
      }
      setEditorSuccess(true)
      setTimeout(() => setEditorSuccess(false), 2000)
      loadSkills()
      // Refresh the file list
      const refreshed = await fetchSkillContent(activeSkill.name, activeSkill.scope || scope, teamSlug)
      setActiveSkill(refreshed)
    } catch (err: any) {
      setEditorError(err.message)
    } finally {
      setEditorSaving(false)
    }
  }

  const handleCreateSkill = async () => {
    setCreating(true)
    setCreateError(null)
    try {
      await createSkillApi(newSkillName, scope, teamSlug)
      setShowCreate(false)
      setNewSkillName('')
      await loadSkills()
      handleOpenSkill(newSkillName, 'edit', scope)
    } catch (err: any) {
      setCreateError(err.message)
    } finally {
      setCreating(false)
    }
  }

  const handleDeleteSkill = async (name: string) => {
    try {
      await deleteSkillApi(name, deleteScope || scope, teamSlug)
      setDeleteConfirm(null)
      setDeleteScope(undefined)
      if (activeSkill?.name === name) {
        setActiveSkill(null)
        setEditorMode(null)
      }
      loadSkills()
    } catch (err: any) {
      setSkillsError(err.message)
    }
  }

  const closeEditor = () => {
    setActiveSkill(null)
    setEditorMode(null)
    setEditorContent('')
    setEditorError(null)
    setEditorSuccess(false)
    setCurrentFilePath('')
    setCurrentFilename('SKILL.md')
    setShowAddFile(false)
    setPendingDeleteFile(null)
    setAddFileError(null)
  }

  // Group skills by scope for platform mode display
  const teamSkills = skillsList.filter(s => s.scope === 'team')
  const orgSkills = skillsList.filter(s => s.scope === 'org')
  const bundledSkills = skillsList.filter(s => s.source === 'bundled')
  const customSkills = skillsList.filter(s => s.source !== 'bundled')

  // Full-screen editor mode
  if (editorMode) {
    const fileGroups = ['scripts', 'references', 'templates', ''] as const

    const renderFileItem = (f: { path: string; filename: string; size?: number; is_executable?: boolean }, isActive: boolean) => {
      const isPendingDelete = pendingDeleteFile && pendingDeleteFile.path === f.path && pendingDeleteFile.filename === f.filename
      if (isPendingDelete) {
        return (
          <div key={`${f.path}/${f.filename}`} className="ml-6 flex items-center gap-2 px-2 py-0.5 rounded bg-red-900/30 text-red-300 text-xs">
            <span>Delete {f.filename}?</span>
            <button onClick={() => handleDeleteFile(f.path, f.filename)} className="px-2 py-0.5 rounded bg-red-600 hover:bg-red-500 text-white text-[10px]">Yes</button>
            <button onClick={() => setPendingDeleteFile(null)} className="px-2 py-0.5 rounded hover:bg-gray-700 text-[10px]">Cancel</button>
          </div>
        )
      }
      return (
        <div
          key={`${f.path}/${f.filename}`}
          onClick={() => switchToFile(f.path, f.filename)}
          className={`group flex items-center justify-between px-2 py-1 rounded cursor-pointer text-sm ${isActive ? 'bg-purple-600 text-white' : 'hover:bg-gray-800/60'}`}
        >
          <div className="flex items-center gap-1.5 min-w-0">
            <FileText size={14} className="flex-shrink-0" />
            <span className="truncate">{f.filename}</span>
            {f.is_executable && <span className="text-[10px] opacity-70">⚡</span>}
          </div>
          <div className="flex items-center gap-1 text-[10px] opacity-60">
            {f.size && <span>{Math.round(f.size / 1024)}k</span>}
            {activeSkill?.editable && (
              <button
                onClick={(e) => { e.stopPropagation(); setPendingDeleteFile({ path: f.path, filename: f.filename }) }}
                className="opacity-0 group-hover:opacity-100 p-0.5 hover:text-red-400"
                title="Delete file"
              >
                <Trash2 size={12} />
              </button>
            )}
          </div>
        </div>
      )
    }

    return (
      <div className="flex flex-col h-full" style={{ margin: '-24px', height: 'calc(100% + 48px)' }}>
        {/* Editor header */}
        <div className="flex items-center justify-between px-4 py-2.5 border-b flex-shrink-0" style={sectionBorderStyle}>
          <div className="flex items-center gap-3 min-w-0">
            <button
              onClick={closeEditor}
              className="p-1.5 rounded-lg transition-colors hover:bg-gray-600/30 flex-shrink-0"
              style={{ color: 'var(--text-muted)' }}
            >
              <X size={18} />
            </button>
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                  {activeSkill?.name}
                </span>
                {activeSkill?.scope && <ScopeBadge scope={activeSkill.scope} />}
                {!activeSkill?.scope && (
                  <span className="text-xs px-2 py-0.5 rounded flex-shrink-0" style={{
                    background: activeSkill?.source === 'bundled' ? 'rgba(168, 85, 247, 0.15)' : 'rgba(34, 197, 94, 0.15)',
                    color: activeSkill?.source === 'bundled' ? '#a855f7' : '#22c55e'
                  }}>
                    {activeSkill?.source}
                  </span>
                )}
                {editorMode === 'view' && (
                  <span className="text-xs px-2 py-0.5 rounded flex-shrink-0" style={{ background: 'rgba(100,100,100,0.2)', color: 'var(--text-muted)' }}>
                    read-only
                  </span>
                )}
              </div>
              {/* Breadcrumb for current file */}
              <div className="text-xs mt-0.5 flex items-center gap-1" style={hintStyle}>
                <span>{activeSkill?.name}</span>
                <span>/</span>
                {currentFilePath && <span>{currentFilePath}/</span>}
                <span className="font-medium" style={{ color: 'var(--text-primary)' }}>{currentFilename}</span>
                {currentFilename === 'SKILL.md' && <span className="opacity-50">(main)</span>}
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2 flex-shrink-0">
            {editorSuccess && (
              <span className="flex items-center gap-1 text-green-400 text-sm">
                <Check size={14} /> Saved
              </span>
            )}
            {editorError && (
              <span className="flex items-center gap-1 text-sm" style={{ color: 'var(--danger)' }}>
                <AlertCircle size={14} /> {editorError}
              </span>
            )}
            {editorMode === 'view' && activeSkill?.editable && (
              <button
                onClick={() => setEditorMode('edit')}
                className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium transition-all"
                style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)', border: '1px solid var(--border-color)' }}
              >
                <Pencil size={14} /> Edit
              </button>
            )}
            {editorMode === 'edit' && (
              <button
                onClick={handleSaveSkill}
                disabled={editorSaving}
                className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-white text-sm font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
                style={saveButtonStyle}
              >
                <Save size={14} />
                {editorSaving ? 'Saving...' : 'Save'}
              </button>
            )}
          </div>
        </div>

        {/* Sidebar + Editor split */}
        <div className="flex flex-1 overflow-hidden">
           {/* LEFT FILE SIDEBAR */}
           <div className="w-56 border-r flex flex-col shrink-0" style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}>
             {/* Sidebar header (relative so the Add popover can be absolutely positioned) */}
             <div className="px-3 py-2 text-xs font-semibold border-b flex items-center justify-between flex-shrink-0 relative" style={{ borderColor: 'var(--border-color)', color: 'var(--text-secondary)' }}>
               <div className="flex items-center gap-1.5">
                 <FolderOpen size={14} />
                 <span>Files</span>
               </div>
               {activeSkill?.editable && (
                 <button
                   onClick={() => { setShowAddFile(!showAddFile); setAddFileError(null) }}
                   className="text-[11px] px-2 py-0.5 rounded bg-emerald-700 hover:bg-emerald-600 text-white flex items-center gap-1"
                 >
                   <Plus size={12} /> Add
                 </button>
               )}

               {/* Floating Add File popover (not constrained by sidebar width/height) */}
               {showAddFile && activeSkill?.editable && (
                 <div
                   ref={addPopoverRef}
                   className="absolute top-full left-0 mt-1 z-50 w-72 p-3 rounded-lg shadow-xl border text-xs"
                   style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}
                 >
                   <div className="flex items-center justify-between mb-2">
                     <div className="font-medium" style={{ color: 'var(--text-secondary)' }}>Add new file</div>
                     <button
                       onClick={() => { setShowAddFile(false); setAddFileError(null) }}
                       className="p-1 -mr-1 rounded hover:bg-gray-700"
                       style={{ color: 'var(--text-muted)' }}
                     >
                       <X size={14} />
                     </button>
                   </div>

                   {/* Directory quick picks */}
                   <div className="flex gap-1 mb-2">
                     {(['scripts', 'references', 'templates', ''] as const).map(d => (
                       <button
                         key={d}
                         onClick={() => setNewFileDir(d)}
                         className={`px-2 py-0.5 rounded text-[10px] ${newFileDir === d ? 'bg-purple-600 text-white' : 'bg-gray-700 hover:bg-gray-600'}`}
                       >
                         {d || 'root'}
                       </button>
                     ))}
                   </div>

                   <div className="flex gap-1">
                     <input
                       type="text"
                       value={newFileName}
                       onChange={(e) => setNewFileName(e.target.value)}
                       onKeyDown={(e) => { if (e.key === 'Enter' && newFileName.trim()) handleAddFile() }}
                       placeholder="filename.sh"
                       className="flex-1 px-2 py-1 text-xs rounded bg-gray-900 border border-gray-700 focus:outline-none focus:border-purple-500"
                       style={{ color: 'var(--text-primary)' }}
                       autoFocus
                     />
                     <button
                       onClick={handleAddFile}
                       disabled={!newFileName.trim()}
                       className="px-3 rounded bg-emerald-700 hover:bg-emerald-600 disabled:opacity-50 text-white"
                     >
                       Add
                     </button>
                   </div>
                   {addFileError && <div className="text-red-400 text-[10px] mt-1.5">{addFileError}</div>}
                 </div>
               )}
             </div>

             {/* File tree */}
             <div className="flex-1 overflow-y-auto p-1 text-sm space-y-0.5" style={{ color: 'var(--text-primary)' }}>
               {/* SKILL.md - always first */}
               <div
                 onClick={() => {
                   setCurrentFilePath('')
                   setCurrentFilename('SKILL.md')
                   setEditorContent(activeSkill?.raw_file || '')
                 }}
                 className={`group flex items-center gap-1.5 px-2 py-1 rounded cursor-pointer ${currentFilename === 'SKILL.md' ? 'bg-purple-600 text-white' : 'hover:bg-gray-800/60'}`}
               >
                 <FileText size={14} />
                 <span className="font-medium">SKILL.md</span>
                 <span className="ml-auto text-[10px] opacity-60">main</span>
               </div>

               {/* Grouped auxiliary files */}
               {fileGroups.map(group => {
                 const gFiles = (activeSkill?.files || []).filter(f => (f.path || '') === group)
                 if (gFiles.length === 0) return null
                 return (
                   <div key={group} className="mt-1">
                     {group && (
                       <div className="flex items-center gap-1 px-2 py-0.5 text-[11px] text-gray-400 font-medium">
                         <ChevronDown size={12} /> {group}/
                       </div>
                     )}
                     <div className={group ? 'ml-3' : ''}>
                       {gFiles.map(f => renderFileItem(f, currentFilePath === f.path && currentFilename === f.filename))}
                     </div>
                   </div>
                 )
               })}

               {/* Empty state */}
               {(!activeSkill?.files || activeSkill.files.length === 0) && (
                 <div className="px-2 py-3 text-[11px] text-gray-400 italic">
                   No extra files yet.<br />Use "+ Add" to create scripts/, references/, etc.
                 </div>
               )}
             </div>
           </div>

          {/* MAIN EDITOR AREA */}
          <div className="flex-1 flex flex-col overflow-hidden">
            {editorLoading ? (
              <div className="flex items-center justify-center h-full">
                <Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} />
              </div>
            ) : (
              <CodeMirror
                value={editorContent}
                onChange={editorMode === 'edit' ? (value: string) => setEditorContent(value) : undefined}
                readOnly={editorMode === 'view'}
                height="100%"
                className="h-full"
                extensions={[
                  getLanguageExtension(currentFilename),
                  search({ scrollToMatch: (range) => EditorView.scrollIntoView(range, { y: 'center', yMargin: 100 }) }),
                  highlightSelectionMatches(),
                  keymap.of(searchKeymap),
                ]}
                theme={theme === 'dark' ? 'dark' : 'light'}
                basicSetup={{
                  lineNumbers: true,
                  highlightActiveLineGutter: true,
                  highlightActiveLine: true,
                  foldGutter: true,
                }}
              />
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="w-full space-y-6">
      {/* Master Toggle — only in personal mode settings */}
      {showConfig && (
        <div className="flex items-center justify-between">
          <div>
            <label className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
              Enable Skills
            </label>
            <p className="text-xs mt-0.5" style={hintStyle}>
              Load custom skills that extend the agent with specialized behaviors and tools. Default: enabled.
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
      )}

      {(showConfig ? form.enabled : true) && (
        <>
          {/* Skills List */}
          <div className={showConfig ? 'pt-4 border-t' : ''} style={showConfig ? sectionBorderStyle : undefined}>
            <div className="flex items-center justify-between mb-3">
              <h4 className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                {scope === 'team' ? 'Team Skills' : scope === 'org' ? 'Organization Skills' : isPlatform ? 'Skills' : 'Installed Skills'}
                {!skillsLoading && (
                  <span className="ml-2 text-xs font-normal" style={hintStyle}>
                    {skillsList.filter(s => s.eligible).length} eligible, {skillsList.length} total
                  </span>
                )}
              </h4>
              {effectiveCanManage && (
                <button
                  onClick={() => { setShowCreate(true); setNewSkillName(''); setCreateError(null) }}
                  className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95"
                  style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)', color: '#fff' }}
                >
                  <Plus size={14} /> New Skill
                </button>
              )}
            </div>

            {skillsLoading && (
              <div className="flex items-center gap-2 py-4">
                <Loader2 size={16} className="animate-spin" style={{ color: 'var(--accent)' }} />
                <span className="text-sm" style={hintStyle}>Loading skills...</span>
              </div>
            )}

            {skillsError && (
              <div className="flex items-center gap-2 p-3 rounded-lg text-sm mb-3"
                style={{ background: 'rgba(239, 68, 68, 0.1)', color: 'var(--danger)' }}>
                <AlertCircle size={14} /> {skillsError}
              </div>
            )}

            {/* Platform mode: scoped sections */}
            {isPlatform && !scope && (
              <>
                {/* Team Skills */}
                {teamSkills.length > 0 && (
                  <SkillSection
                    label="Team"
                    skills={teamSkills}
                    canManage={isTeamAdmin}
                    onView={(s) => handleOpenSkill(s.name, 'view', 'team')}
                    onEdit={(s) => handleOpenSkill(s.name, 'edit', 'team')}
                    onDelete={(s) => { setDeleteConfirm(s.name); setDeleteScope('team') }}
                  />
                )}

                {/* Org Skills */}
                {orgSkills.length > 0 && (
                  <SkillSection
                    label="Organization (inherited)"
                    skills={orgSkills}
                    canManage={isOrgAdmin}
                    onView={(s) => handleOpenSkill(s.name, 'view', 'org')}
                    onEdit={(s) => handleOpenSkill(s.name, 'edit', 'org')}
                    onDelete={(s) => { setDeleteConfirm(s.name); setDeleteScope('org') }}
                  />
                )}

                {/* Bundled Skills */}
                {bundledSkills.length > 0 && (
                  <SkillSection
                    label="Bundled (read-only)"
                    skills={bundledSkills}
                    canManage={false}
                    onView={(s) => handleOpenSkill(s.name, 'view', undefined)}
                  />
                )}
              </>
            )}

            {/* Scoped or personal mode: Bundled / Custom grouping */}
            {(isPlatform ? !!scope : true) && (
              <>
                {bundledSkills.length > 0 && (
                  <div className="mb-4">
                    <div className="text-xs font-medium mb-2 uppercase tracking-wider" style={hintStyle}>
                      Bundled
                    </div>
                    <div className="space-y-1">
                      {bundledSkills.map(skill => (
                        <SkillRow
                          key={skill.name}
                          skill={skill}
                          onView={() => handleOpenSkill(skill.name, 'view', isPlatform ? undefined : undefined)}
                        />
                      ))}
                    </div>
                  </div>
                )}

                {customSkills.length > 0 && (
                  <div className="mb-4">
                    <div className="text-xs font-medium mb-2 uppercase tracking-wider" style={hintStyle}>
                      Custom
                    </div>
                    <div className="space-y-1">
                      {customSkills.map(skill => (
                        <SkillRow
                          key={skill.name}
                          skill={skill}
                          onView={() => handleOpenSkill(skill.name, 'view', scope)}
                          onEdit={effectiveCanManage && skill.editable ? () => handleOpenSkill(skill.name, 'edit', scope) : undefined}
                          onDelete={effectiveCanManage && skill.editable ? () => { setDeleteConfirm(skill.name); setDeleteScope(scope) } : undefined}
                        />
                      ))}
                    </div>
                  </div>
                )}
              </>
            )}

            {!skillsLoading && skillsList.length === 0 && (
              <p className="text-sm py-4" style={hintStyle}>
                {scope === 'team'
                  ? 'No team skills yet. Click "New Skill" to create one.'
                  : scope === 'org'
                    ? 'No organization skills yet. Click "New Skill" to create one.'
                    : 'No skills found. Click "New Skill" to create one, or install from ClawHub via CLI: astonish skills install <slug>'
                }
              </p>
            )}
          </div>

          {/* Configuration — personal mode only */}
          {showConfig && (
            <>
              <div className="pt-4 border-t" style={sectionBorderStyle}>
                <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
                  Configuration
                </h4>
                <div className="space-y-4">
                  <div>
                    <label className="block text-sm font-medium mb-2" style={labelStyle}>
                      User Skills Directory
                    </label>
                    <input
                      type="text"
                      value={form.user_dir}
                      onChange={(e) => setForm({ ...form, user_dir: e.target.value })}
                      placeholder="~/.config/astonish/skills/ (default)"
                      className={inputClass + ' font-mono'}
                      style={inputStyle}
                    />
                    <p className="text-xs mt-1" style={hintStyle}>
                      Directory for user-defined skill files. Default: ~/.config/astonish/skills/
                    </p>
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-2" style={labelStyle}>
                      Extra Directories
                    </label>
                    <input
                      type="text"
                      value={extraDirsText}
                      onChange={(e) => setExtraDirsText(e.target.value)}
                      placeholder="No extra directories"
                      className={inputClass + ' font-mono'}
                      style={inputStyle}
                    />
                    <p className="text-xs mt-1" style={hintStyle}>
                      Comma-separated additional directories to search for skills.
                    </p>
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-2" style={labelStyle}>
                      Allowlist
                    </label>
                    <input
                      type="text"
                      value={allowlistText}
                      onChange={(e) => setAllowlistText(e.target.value)}
                      placeholder="All eligible skills (default)"
                      className={inputClass + ' font-mono'}
                      style={inputStyle}
                    />
                    <p className="text-xs mt-1" style={hintStyle}>
                      Comma-separated skill names to allow. Leave empty to load all eligible skills.
                    </p>
                  </div>
                </div>
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
                  {saving ? 'Saving...' : 'Save Configuration'}
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
            </>
          )}
        </>
      )}

      {/* Create Skill Modal */}
      {showCreate && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4" style={{ background: 'rgba(0,0,0,0.7)' }}>
          <div className="rounded-xl w-full max-w-md p-6 shadow-2xl"
            style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>Create New Skill</h3>
              <button onClick={() => setShowCreate(false)} className="p-1.5 rounded-lg hover:bg-gray-600/30" style={{ color: 'var(--text-muted)' }}>
                <X size={18} />
              </button>
            </div>
            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium mb-2" style={labelStyle}>Skill Name</label>
                <input
                  type="text"
                  value={newSkillName}
                  onChange={(e) => setNewSkillName(e.target.value.toLowerCase().replace(/[^a-z0-9-_]/g, ''))}
                  placeholder="my-custom-skill"
                  className={inputClass + ' font-mono'}
                  style={inputStyle}
                  autoFocus
                  onKeyDown={(e) => { if (e.key === 'Enter' && newSkillName.trim()) handleCreateSkill() }}
                />
                <p className="text-xs mt-1" style={hintStyle}>
                  Lowercase letters, numbers, hyphens, and underscores only.
                </p>
              </div>
              {createError && (
                <div className="flex items-center gap-2 p-2 rounded-lg text-sm"
                  style={{ background: 'rgba(239, 68, 68, 0.1)', color: 'var(--danger)' }}>
                  <AlertCircle size={14} /> {createError}
                </div>
              )}
              <div className="flex justify-end gap-3">
                <button
                  onClick={() => setShowCreate(false)}
                  className="px-4 py-2 rounded-lg text-sm font-medium"
                  style={{ color: 'var(--text-secondary)', background: 'var(--bg-tertiary)', border: '1px solid var(--border-color)' }}
                >
                  Cancel
                </button>
                <button
                  onClick={handleCreateSkill}
                  disabled={creating || !newSkillName.trim()}
                  className="flex items-center gap-2 px-4 py-2 rounded-lg text-white text-sm font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
                  style={saveButtonStyle}
                >
                  {creating ? <Loader2 size={14} className="animate-spin" /> : <Plus size={14} />}
                  {creating ? 'Creating...' : 'Create'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Delete Confirmation Modal */}
      {deleteConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4" style={{ background: 'rgba(0,0,0,0.7)' }}>
          <div className="rounded-xl w-full max-w-sm p-6 shadow-2xl"
            style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <h3 className="text-lg font-semibold mb-2" style={{ color: 'var(--text-primary)' }}>Delete Skill</h3>
            <p className="text-sm mb-4" style={hintStyle}>
              Are you sure you want to delete <strong style={{ color: 'var(--text-primary)' }}>{deleteConfirm}</strong>? This cannot be undone.
            </p>
            <div className="flex justify-end gap-3">
              <button
                onClick={() => { setDeleteConfirm(null); setDeleteScope(undefined) }}
                className="px-4 py-2 rounded-lg text-sm font-medium"
                style={{ color: 'var(--text-secondary)', background: 'var(--bg-tertiary)', border: '1px solid var(--border-color)' }}
              >
                Cancel
              </button>
              <button
                onClick={() => handleDeleteSkill(deleteConfirm)}
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

// --- Sub-components ---

// SkillSection renders a labeled group of skills
function SkillSection({ label, skills, canManage, onView, onEdit, onDelete }: {
  label: string
  skills: Skill[]
  canManage: boolean
  onView?: (s: Skill) => void
  onEdit?: (s: Skill) => void
  onDelete?: (s: Skill) => void
}) {
  return (
    <div className="mb-4">
      <div className="text-xs font-medium mb-2 uppercase tracking-wider" style={hintStyle}>
        {label}
      </div>
      <div className="space-y-1">
        {skills.map(skill => (
          <SkillRow
            key={skill.name}
            skill={skill}
            showScope
            onView={onView ? () => onView(skill) : undefined}
            onEdit={canManage && skill.editable && onEdit ? () => onEdit(skill) : undefined}
            onDelete={canManage && skill.editable && onDelete ? () => onDelete(skill) : undefined}
          />
        ))}
      </div>
    </div>
  )
}

// Skill row component
interface SkillRowProps {
  skill: Skill
  showScope?: boolean
  onView?: () => void
  onEdit?: () => void
  onDelete?: () => void
}

function SkillRow({ skill, showScope, onView, onEdit, onDelete }: SkillRowProps) {
  return (
    <div
      className="flex items-center gap-3 px-3 py-2 rounded-lg transition-colors group"
      style={{ background: 'var(--bg-secondary)', border: '1px solid transparent' }}
      onMouseEnter={(e) => { (e.currentTarget as HTMLDivElement).style.borderColor = 'var(--border-color)' }}
      onMouseLeave={(e) => { (e.currentTarget as HTMLDivElement).style.borderColor = 'transparent' }}
    >
      {/* Status icon */}
      <div className="flex-shrink-0">
        {skill.eligible ? (
          <CheckCircle size={16} style={{ color: '#22c55e' }} />
        ) : (
          <XCircle size={16} style={{ color: '#ef4444' }} />
        )}
      </div>

      {/* Name and description */}
      <div className="flex-1 min-w-0">
         <div className="flex items-center gap-2">
           <span className="text-sm font-medium truncate" style={{ color: 'var(--text-primary)' }}>
             {skill.name}
           </span>
           {showScope && skill.scope && <ScopeBadge scope={skill.scope} />}
           {skill.has_directory && (
             <span className="text-[10px] px-1.5 py-0.5 rounded bg-blue-900/40 text-blue-300" title="This skill contains multiple files (scripts, references, etc.)">
               multi-file
             </span>
           )}
           {skill.require_bins?.length && skill.require_bins.length > 0 && !skill.eligible && (
             <span className="text-xs px-1.5 py-0.5 rounded flex-shrink-0"
               style={{ background: 'rgba(239, 68, 68, 0.1)', color: '#f87171' }}>
               missing: {(skill.missing || []).join(', ')}
             </span>
           )}
         </div>
        <div className="text-xs truncate" style={{ color: 'var(--text-muted)' }}>
          {skill.description}
        </div>
      </div>

      {/* Actions */}
      <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0">
        {onView && (
          <button
            onClick={onView}
            className="p-1.5 rounded-lg transition-colors hover:bg-gray-600/30"
            style={{ color: 'var(--text-muted)' }}
            title="View"
          >
            <Eye size={14} />
          </button>
        )}
        {onEdit && (
          <button
            onClick={onEdit}
            className="p-1.5 rounded-lg transition-colors hover:bg-gray-600/30"
            style={{ color: 'var(--text-muted)' }}
            title="Edit"
          >
            <Pencil size={14} />
          </button>
        )}
        {onDelete && (
          <button
            onClick={onDelete}
            className="p-1.5 rounded-lg transition-colors hover:bg-red-600/20"
            style={{ color: 'var(--text-muted)' }}
            title="Delete"
          >
            <Trash2 size={14} />
          </button>
        )}
        {!onEdit && !onDelete && (
          <ChevronRight size={14} style={{ color: 'var(--text-muted)' }} />
        )}
      </div>
    </div>
  )
}
