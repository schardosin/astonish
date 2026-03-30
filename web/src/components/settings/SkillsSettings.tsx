import { useState, useEffect, useCallback } from 'react'
import { Save, AlertCircle, Check, Plus, Trash2, ChevronRight, Eye, Pencil, X, Loader2, CheckCircle, XCircle } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'
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
}

// API helpers
const fetchSkills = async (): Promise<{ skills: Skill[] }> => {
  const res = await fetch('/api/skills')
  if (!res.ok) throw new Error('Failed to fetch skills')
  return res.json()
}

const fetchSkillContent = async (name: string): Promise<ActiveSkill> => {
  const res = await fetch(`/api/skills/${encodeURIComponent(name)}/content`)
  if (!res.ok) throw new Error('Failed to fetch skill content')
  return res.json()
}

const saveSkillContent = async (name: string, rawFile: string): Promise<Record<string, any>> => {
  const res = await fetch(`/api/skills/${encodeURIComponent(name)}/content`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ raw_file: rawFile })
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to save skill')
  }
  return res.json()
}

const createSkill = async (name: string): Promise<Record<string, any>> => {
  const res = await fetch('/api/skills', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name })
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to create skill')
  }
  return res.json()
}

const deleteSkill = async (name: string): Promise<Record<string, any>> => {
  const res = await fetch(`/api/skills/${encodeURIComponent(name)}`, {
    method: 'DELETE'
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to delete skill')
  }
  return res.json()
}

export default function SkillsSettings({ config, onSaved, theme = 'dark' }: SkillsSettingsProps) {
  // Config form state
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

  // Editor state
  const [activeSkill, setActiveSkill] = useState<ActiveSkill | null>(null) // { name, content, rawFile, editable, source }
  const [editorContent, setEditorContent] = useState('')
  const [editorMode, setEditorMode] = useState<'view' | 'edit' | null>(null) // 'view' or 'edit'
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

  // Config save state
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (config) {
      setForm({
        enabled: config.enabled !== false,
        user_dir: config.user_dir || '',
        extra_dirs: config.extra_dirs || [],
        allowlist: config.allowlist || []
      })
      setExtraDirsText((config.extra_dirs || []).join(', '))
      setAllowlistText((config.allowlist || []).join(', '))
    }
  }, [config])

  const loadSkills = useCallback(async () => {
    setSkillsLoading(true)
    setSkillsError(null)
    try {
      const data = await fetchSkills()
      setSkillsList(data.skills || [])
    } catch (err: any) {
      setSkillsError(err.message)
    } finally {
      setSkillsLoading(false)
    }
  }, [])

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

  const handleOpenSkill = async (name: string, mode: 'view' | 'edit') => {
    setEditorLoading(true)
    setEditorError(null)
    setEditorSuccess(false)
    try {
      const data = await fetchSkillContent(name)
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
      await saveSkillContent(activeSkill.name, editorContent)
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
      await createSkill(newSkillName)
      setShowCreate(false)
      setNewSkillName('')
      await loadSkills()
      // Open the new skill in edit mode
      handleOpenSkill(newSkillName, 'edit')
    } catch (err: any) {
      setCreateError(err.message)
    } finally {
      setCreating(false)
    }
  }

  const handleDeleteSkill = async (name: string) => {
    try {
      await deleteSkill(name)
      setDeleteConfirm(null)
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

  const bundledSkills = skillsList.filter(s => s.source === 'bundled')
  const userSkills = skillsList.filter(s => s.source !== 'bundled')

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
                <span className="text-xs px-2 py-0.5 rounded" style={{
                  background: activeSkill?.source === 'bundled' ? 'rgba(168, 85, 247, 0.15)' : 'rgba(34, 197, 94, 0.15)',
                  color: activeSkill?.source === 'bundled' ? '#a855f7' : '#22c55e'
                }}>
                  {activeSkill?.source}
                </span>
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
    <div className="max-w-2xl space-y-6">
      {/* Master Toggle */}
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

      {form.enabled && (
        <>
          {/* Skills List */}
          <div className="pt-4 border-t" style={sectionBorderStyle}>
            <div className="flex items-center justify-between mb-3">
              <h4 className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                Installed Skills
                {!skillsLoading && (
                  <span className="ml-2 text-xs font-normal" style={hintStyle}>
                    {skillsList.filter(s => s.eligible).length} eligible, {skillsList.length} total
                  </span>
                )}
              </h4>
              <button
                onClick={() => { setShowCreate(true); setNewSkillName(''); setCreateError(null) }}
                className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95"
                style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)', color: '#fff' }}
              >
                <Plus size={14} /> New Skill
              </button>
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

            {/* Bundled Skills */}
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
                      onView={() => handleOpenSkill(skill.name, 'view')}
                    />
                  ))}
                </div>
              </div>
            )}

            {/* User / Extra / Project Skills */}
            {userSkills.length > 0 && (
              <div className="mb-4">
                <div className="text-xs font-medium mb-2 uppercase tracking-wider" style={hintStyle}>
                  Custom
                </div>
                <div className="space-y-1">
                  {userSkills.map(skill => (
                    <SkillRow
                      key={skill.name}
                      skill={skill}
                      onView={() => handleOpenSkill(skill.name, 'view')}
                      onEdit={() => handleOpenSkill(skill.name, 'edit')}
                      onDelete={() => setDeleteConfirm(skill.name)}
                    />
                  ))}
                </div>
              </div>
            )}

            {!skillsLoading && skillsList.length === 0 && (
              <p className="text-sm py-4" style={hintStyle}>
                No skills found. Click &quot;New Skill&quot; to create one, or install from ClawHub via CLI: <code>astonish skills install &lt;slug&gt;</code>
              </p>
            )}
          </div>

          {/* Configuration */}
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
              Are you sure you want to delete <strong style={{ color: 'var(--text-primary)' }}>{deleteConfirm}</strong>? This will remove the skill directory and all its files. This cannot be undone.
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

// Skill row component
interface SkillRowProps {
  skill: Skill
  onView?: () => void
  onEdit?: () => void
  onDelete?: () => void
}

function SkillRow({ skill, onView, onEdit, onDelete }: SkillRowProps) {
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
