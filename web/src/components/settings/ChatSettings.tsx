import { useState, useEffect } from 'react'
import { Save, AlertCircle, Check } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'

export default function ChatSettings({ config, onSaved }: { config: Record<string, any>; onSaved?: () => void }) {
  const [form, setForm] = useState({
    system_prompt: '',
    max_tool_calls: 0,
    max_tools: 0,
    auto_approve: false,
    workspace_dir: '',
    flow_save_dir: ''
  })
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (config) {
      setForm({
        system_prompt: config.system_prompt || '',
        max_tool_calls: config.max_tool_calls || 0,
        max_tools: config.max_tools || 0,
        auto_approve: config.auto_approve || false,
        workspace_dir: config.workspace_dir || '',
        flow_save_dir: config.flow_save_dir || ''
      })
    }
  }, [config])

  const handleSave = async () => {
    setSaving(true)
    setSaveSuccess(false)
    setError(null)
    try {
      await saveFullConfigSection('chat', form)
      setSaveSuccess(true)
      if (onSaved) onSaved()
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="max-w-xl space-y-6">
      {/* System Prompt */}
      <div>
        <label className="block text-sm font-medium mb-2" style={labelStyle}>
          System Prompt
        </label>
        <textarea
          value={form.system_prompt}
          onChange={(e) => setForm({ ...form, system_prompt: e.target.value })}
          placeholder="You are Astonish, an AI assistant with access to tools..."
          rows={5}
          className={inputClass}
          style={{ ...inputStyle, resize: 'vertical', minHeight: '100px' }}
        />
        <p className="text-xs mt-1" style={hintStyle}>
          Custom instructions appended to the built-in system prompt. Leave empty to use only the default
          (&quot;You are Astonish, an AI assistant with access to tools. You help users accomplish tasks by calling tools and reasoning through problems.&quot;).
        </p>
      </div>

      {/* Tool Limits */}
      <div className="pt-4 border-t" style={sectionBorderStyle}>
        <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
          Tool Limits
        </h4>
        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Max Tool Calls
            </label>
            <input
              type="number"
              value={form.max_tool_calls || ''}
              onChange={(e) => setForm({ ...form, max_tool_calls: parseInt(e.target.value) || 0 })}
              placeholder="100 (default)"
              min="0"
              className={inputClass}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Maximum tool calls per conversation turn. Default: 100. Set to 0 to use the default.
            </p>
          </div>
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Max Tools
            </label>
            <input
              type="number"
              value={form.max_tools || ''}
              onChange={(e) => setForm({ ...form, max_tools: parseInt(e.target.value) || 0 })}
              placeholder="128 (default)"
              min="0"
              className={inputClass}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Maximum tools exposed to the LLM. Default: 128. Set to 0 to use the default.
            </p>
          </div>
        </div>
      </div>

      {/* Auto Approve */}
      <div className="pt-4 border-t" style={sectionBorderStyle}>
        <div className="flex items-center justify-between">
          <div>
            <label className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
              Auto-Approve Tool Calls
            </label>
            <p className="text-xs mt-0.5" style={hintStyle}>
              Automatically approve all tool executions without prompting. Default: off.
            </p>
          </div>
          <button
            onClick={() => setForm({ ...form, auto_approve: !form.auto_approve })}
            className="relative w-11 h-6 rounded-full transition-colors"
            style={{
              background: form.auto_approve ? '#a855f7' : 'var(--bg-tertiary)',
              border: `1px solid ${form.auto_approve ? '#a855f7' : 'var(--border-color)'}`
            }}
          >
            <span
              className="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform bg-white"
              style={{ transform: form.auto_approve ? 'translateX(20px)' : 'translateX(0)' }}
            />
          </button>
        </div>
      </div>

      {/* Directories */}
      <div className="pt-4 border-t" style={sectionBorderStyle}>
        <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
          Directories
        </h4>
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Workspace Directory
            </label>
            <input
              type="text"
              value={form.workspace_dir}
              onChange={(e) => setForm({ ...form, workspace_dir: e.target.value })}
              placeholder="Current working directory (default)"
              className={inputClass + ' font-mono'}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Working directory for tool execution. Default: the directory where Astonish was started.
            </p>
          </div>
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Flow Save Directory
            </label>
            <input
              type="text"
              value={form.flow_save_dir}
              onChange={(e) => setForm({ ...form, flow_save_dir: e.target.value })}
              placeholder="~/.config/astonish/flows/ (default)"
              className={inputClass + ' font-mono'}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Directory where recorded flows are saved. Default: ~/.config/astonish/flows/
            </p>
          </div>
        </div>
      </div>

      {/* Save */}
      <div className="flex items-center gap-3">
        <button
          onClick={handleSave}
          disabled={saving}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-white font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
          style={saveButtonStyle}
        >
          <Save size={16} />
          {saving ? 'Saving...' : 'Save Changes'}
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
    </div>
  )
}
