import { useState, useEffect, useCallback } from 'react'
import { Save, AlertCircle, Check, Plus, Trash2, ChevronRight, Eye, Pencil, X, Loader2, CheckCircle, XCircle } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'
import { teamFetch } from '../../api/teamContext'
import CodeMirror from '@uiw/react-codemirror'
import { markdown } from '@codemirror/lang-markdown'
import { languages } from '@codemirror/language-data'
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
}

interface ActiveSkill {
  name: string
  content: string
  raw_file: string
  editable: boolean
  source: string
  scope?: string
  description?: string
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
  const res = await teamFetch(`/api/skills/${encodeURIComponent(name)}/content${params}`, undefined, teamSlug)
  if (!res.ok) throw new Error('Failed to fetch skill content')
  return res.json()
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

  const handleSaveSkill = async () => {
    if (!activeSkill) return
    setEditorSaving(true)
    setEditorError(null)
    setEditorSuccess(false)
    try {
      await saveSkillContent(activeSkill.name, editorContent, activeSkill.scope || scope, teamSlug)
      setEditorSuccess(true)
      setTimeout(() => setEditorSuccess(false), 2000)
      loadSkills()
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
  }

  // Group skills by scope for platform mode display
  const teamSkills = skillsList.filter(s => s.scope === 'team')
  const orgSkills = skillsList.filter(s => s.scope === 'org')
  const bundledSkills = skillsList.filter(s => s.source === 'bundled')
  const customSkills = skillsList.filter(s => s.source !== 'bundled')

  // Full-screen editor mode
  if (editorMode) {
    return (
      <div className="flex flex-col h-full" style={{ margin: '-24px', height: 'calc(100% + 48px)' }}>
        {/* Editor header */}
        <div className="flex items-center justify-between px-6 py-3 border-b flex-shrink-0" style={sectionBorderStyle}>
          <div className="flex items-center gap-3">
            <button
              onClick={closeEditor}
              className="p-1.5 rounded-lg transition-colors hover:bg-gray-600/30"
              style={{ color: 'var(--text-muted)' }}
            >
              <X size={18} />
            </button>
            <div>
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                  {activeSkill?.name}
                </span>
                {activeSkill?.scope && <ScopeBadge scope={activeSkill.scope} />}
                {!activeSkill?.scope && (
                  <span className="text-xs px-2 py-0.5 rounded" style={{
                    background: activeSkill?.source === 'bundled' ? 'rgba(168, 85, 247, 0.15)' : 'rgba(34, 197, 94, 0.15)',
                    color: activeSkill?.source === 'bundled' ? '#a855f7' : '#22c55e'
                  }}>
                    {activeSkill?.source}
                  </span>
                )}
                {editorMode === 'view' && (
                  <span className="text-xs px-2 py-0.5 rounded" style={{ background: 'rgba(100,100,100,0.2)', color: 'var(--text-muted)' }}>
                    read-only
                  </span>
                )}
              </div>
              <div className="text-xs" style={hintStyle}>{activeSkill?.description}</div>
            </div>
          </div>
          <div className="flex items-center gap-2">
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
        {/* Editor body */}
        <div className="flex-1 overflow-hidden">
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
                markdown({ codeLanguages: languages }),
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
                {scope === 'team' ? 'Team Skills' : scope === 'org' ? 'Organization Skills' : 'Installed Skills'}
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
                    label="Organization"
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
                    label="Bundled"
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
