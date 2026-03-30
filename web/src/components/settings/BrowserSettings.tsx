import { useState, useEffect } from 'react'
import { Save, AlertCircle, Check, Info } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'

interface BrowserForm {
  headless: boolean | null
  viewport_width: number
  viewport_height: number
  no_sandbox: boolean | null
  chrome_path: string
  user_data_dir: string
  navigation_timeout: number
  proxy: string
  remote_cdp_url: string
  fingerprint_seed: string
  fingerprint_platform: string
  handoff_bind_address: string
  handoff_port: number
}

interface BrowserSettingsProps {
  config: Record<string, any> | null
  onSaved?: () => void
}

export default function BrowserSettings({ config, onSaved }: BrowserSettingsProps) {
  const [form, setForm] = useState<BrowserForm>({
    headless: null,
    viewport_width: 1280,
    viewport_height: 720,
    no_sandbox: null,
    chrome_path: '',
    user_data_dir: '',
    navigation_timeout: 30,
    proxy: '',
    remote_cdp_url: '',
    fingerprint_seed: '',
    fingerprint_platform: '',
    handoff_bind_address: '',
    handoff_port: 9222
  })
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Derive engine type from config values — mirrors detectCurrentEngine() in Go
  const getEngineType = (cfg: BrowserForm): string => {
    if (cfg.remote_cdp_url) return 'remote'
    if (!cfg.chrome_path) return 'default'
    if (cfg.chrome_path.includes('cloakbrowser')) return 'cloakbrowser'
    return 'custom'
  }

  const [engineType, setEngineType] = useState('default')

  useEffect(() => {
    if (config) {
      const f: BrowserForm = {
        headless: config.headless ?? null,
        viewport_width: config.viewport_width || 1280,
        viewport_height: config.viewport_height || 720,
        no_sandbox: config.no_sandbox ?? null,
        chrome_path: config.chrome_path || '',
        user_data_dir: config.user_data_dir || '',
        navigation_timeout: config.navigation_timeout || 30,
        proxy: config.proxy || '',
        remote_cdp_url: config.remote_cdp_url || '',
        fingerprint_seed: config.fingerprint_seed || '',
        fingerprint_platform: config.fingerprint_platform || '',
        handoff_bind_address: config.handoff_bind_address || '',
        handoff_port: config.handoff_port || 9222
      }
      setForm(f)
      setEngineType(getEngineType(f))
    }
  }, [config])

  const handleEngineChange = (type: string) => {
    setEngineType(type)
    // Clear engine-specific fields when switching
    if (type === 'default') {
      setForm(f => ({ ...f, chrome_path: '', remote_cdp_url: '', fingerprint_seed: '', fingerprint_platform: '' }))
    } else if (type === 'cloakbrowser') {
      setForm(f => ({ ...f, remote_cdp_url: '', fingerprint_platform: f.fingerprint_platform || 'windows' }))
    } else if (type === 'custom') {
      setForm(f => ({ ...f, remote_cdp_url: '', fingerprint_seed: '', fingerprint_platform: '' }))
    } else if (type === 'remote') {
      setForm(f => ({ ...f, chrome_path: '', fingerprint_seed: '', fingerprint_platform: '' }))
    }
  }

  const handleSave = async () => {
    setSaving(true)
    setSaveSuccess(false)
    setError(null)
    try {
      await saveFullConfigSection('browser', form as unknown as Record<string, unknown>)
      setSaveSuccess(true)
      if (onSaved) onSaved()
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err: any) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const isHeadless = form.headless === true
  const isNoSandbox = form.no_sandbox === true

  return (
    <div className="max-w-xl space-y-6">
      {/* Engine Selection */}
      <div>
        <label className="block text-sm font-medium mb-2" style={labelStyle}>
          Browser Engine
        </label>
        <select
          value={engineType}
          onChange={(e) => handleEngineChange(e.target.value)}
          className={inputClass}
          style={inputStyle}
        >
          <option value="default">Default (Chromium, auto-downloaded by Astonish)</option>
          <option value="cloakbrowser">CloakBrowser (anti-detect Chromium with stealth patches)</option>
          <option value="custom">Custom Chrome/Chromium path</option>
          <option value="remote">Remote browser (connect via CDP)</option>
        </select>
        <p className="text-xs mt-1" style={hintStyle}>
          {engineType === 'default' && 'Astonish will automatically download and manage a Chromium binary.'}
          {engineType === 'cloakbrowser' && 'CloakBrowser provides advanced fingerprint spoofing at the binary level. Install via CLI: astonish config browser'}
          {engineType === 'custom' && 'Point to an existing Chrome or Chromium installation on your system.'}
          {engineType === 'remote' && 'Connect to a remote browser instance (Chrome, anti-detect browsers, Browserless, etc.) via Chrome DevTools Protocol.'}
        </p>
      </div>

      {/* Engine-specific fields */}
      {engineType === 'custom' && (
        <div>
          <label className="block text-sm font-medium mb-2" style={labelStyle}>
            Chrome Binary Path
          </label>
          <input
            type="text"
            value={form.chrome_path}
            onChange={(e) => setForm({ ...form, chrome_path: e.target.value })}
            placeholder="/usr/bin/google-chrome"
            className={inputClass + ' font-mono'}
            style={inputStyle}
          />
        </div>
      )}

      {engineType === 'cloakbrowser' && (
        <>
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Chrome Binary Path
            </label>
            <input
              type="text"
              value={form.chrome_path}
              onChange={(e) => setForm({ ...form, chrome_path: e.target.value })}
              placeholder="~/.cloakbrowser/chromium-.../chrome"
              className={inputClass + ' font-mono'}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Path to the CloakBrowser binary. Use the CLI to auto-install: <code>astonish config browser</code>
            </p>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Fingerprint Platform
              </label>
              <select
                value={form.fingerprint_platform || 'windows'}
                onChange={(e) => setForm({ ...form, fingerprint_platform: e.target.value })}
                className={inputClass}
                style={inputStyle}
              >
                <option value="windows">Windows (recommended)</option>
                <option value="macos">macOS</option>
                <option value="linux">Linux</option>
              </select>
            </div>
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Fingerprint Seed
              </label>
              <input
                type="text"
                value={form.fingerprint_seed}
                onChange={(e) => setForm({ ...form, fingerprint_seed: e.target.value })}
                placeholder="e.g. 42000"
                className={inputClass + ' font-mono'}
                style={inputStyle}
              />
              <p className="text-xs mt-1" style={hintStyle}>
                Unique seed for consistent fingerprint generation.
              </p>
            </div>
          </div>
        </>
      )}

      {engineType === 'remote' && (
        <div>
          <label className="block text-sm font-medium mb-2" style={labelStyle}>
            Remote CDP URL
          </label>
          <input
            type="text"
            value={form.remote_cdp_url}
            onChange={(e) => setForm({ ...form, remote_cdp_url: e.target.value })}
            placeholder="ws://192.168.1.100:9222/devtools/browser/..."
            className={inputClass + ' font-mono'}
            style={inputStyle}
          />
          <p className="text-xs mt-1" style={hintStyle}>
            WebSocket URL of the Chrome DevTools Protocol endpoint. Use the CLI for auto-discovery: <code>astonish config browser</code>
          </p>
        </div>
      )}

      {/* Viewport & Display */}
      <div className="pt-4 border-t" style={sectionBorderStyle}>
        <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
          Display
        </h4>
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <label className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                Headless Mode
              </label>
              <p className="text-xs mt-0.5" style={hintStyle}>
                Run browser without a visible window. Headed mode (with Xvfb) produces more realistic fingerprints.
              </p>
            </div>
            <button
              onClick={() => setForm({ ...form, headless: !isHeadless ? true : null })}
              className="relative w-11 h-6 rounded-full transition-colors"
              style={{
                background: isHeadless ? '#a855f7' : 'var(--bg-tertiary)',
                border: `1px solid ${isHeadless ? '#a855f7' : 'var(--border-color)'}`
              }}
            >
              <span
                className="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform bg-white"
                style={{ transform: isHeadless ? 'translateX(20px)' : 'translateX(0)' }}
              />
            </button>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Viewport Width
              </label>
              <input
                type="number"
                value={form.viewport_width}
                onChange={(e) => setForm({ ...form, viewport_width: parseInt(e.target.value) || 1280 })}
                min="320"
                max="3840"
                className={inputClass}
                style={inputStyle}
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Viewport Height
              </label>
              <input
                type="number"
                value={form.viewport_height}
                onChange={(e) => setForm({ ...form, viewport_height: parseInt(e.target.value) || 720 })}
                min="240"
                max="2160"
                className={inputClass}
                style={inputStyle}
              />
            </div>
          </div>
        </div>
      </div>

      {/* Network */}
      <div className="pt-4 border-t" style={sectionBorderStyle}>
        <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
          Network
        </h4>
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Proxy
            </label>
            <input
              type="text"
              value={form.proxy}
              onChange={(e) => setForm({ ...form, proxy: e.target.value })}
              placeholder="http://user:pass@host:port or socks5://host:port"
              className={inputClass + ' font-mono'}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Route browser traffic through an HTTP or SOCKS proxy.
            </p>
          </div>
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Navigation Timeout (seconds)
            </label>
            <input
              type="number"
              value={form.navigation_timeout}
              onChange={(e) => setForm({ ...form, navigation_timeout: parseInt(e.target.value) || 30 })}
              min="5"
              max="300"
              className={inputClass}
              style={inputStyle}
            />
          </div>
        </div>
      </div>

      {/* Advanced */}
      <div className="pt-4 border-t" style={sectionBorderStyle}>
        <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
          Advanced
        </h4>
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              User Data Directory
            </label>
            <input
              type="text"
              value={form.user_data_dir}
              onChange={(e) => setForm({ ...form, user_data_dir: e.target.value })}
              placeholder="~/.config/astonish/browser/ (default)"
              className={inputClass + ' font-mono'}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Persistent browser profile directory. Stores cookies, localStorage, etc.
            </p>
          </div>

          <div className="flex items-center justify-between">
            <div>
              <label className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                No Sandbox
              </label>
              <p className="text-xs mt-0.5" style={hintStyle}>
                Disable Chrome sandbox. Auto-enabled when running as root.
              </p>
            </div>
            <button
              onClick={() => setForm({ ...form, no_sandbox: !isNoSandbox ? true : null })}
              className="relative w-11 h-6 rounded-full transition-colors"
              style={{
                background: isNoSandbox ? '#a855f7' : 'var(--bg-tertiary)',
                border: `1px solid ${isNoSandbox ? '#a855f7' : 'var(--border-color)'}`
              }}
            >
              <span
                className="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform bg-white"
                style={{ transform: isNoSandbox ? 'translateX(20px)' : 'translateX(0)' }}
              />
            </button>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Handoff Bind Address
              </label>
              <input
                type="text"
                value={form.handoff_bind_address}
                onChange={(e) => setForm({ ...form, handoff_bind_address: e.target.value })}
                placeholder="0.0.0.0 (default)"
                className={inputClass + ' font-mono'}
                style={inputStyle}
              />
              <p className="text-xs mt-1" style={hintStyle}>
                CDP handoff proxy bind address for human-in-the-loop.
              </p>
            </div>
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Handoff Port
              </label>
              <input
                type="number"
                value={form.handoff_port}
                onChange={(e) => setForm({ ...form, handoff_port: parseInt(e.target.value) || 9222 })}
                min="1024"
                max="65535"
                className={inputClass}
                style={inputStyle}
              />
            </div>
          </div>
        </div>
      </div>

      {/* Info banner for CloakBrowser */}
      {engineType === 'cloakbrowser' && (
        <div className="flex items-start gap-2 p-3 rounded-lg text-sm"
          style={{ background: 'rgba(168, 85, 247, 0.1)', border: '1px solid rgba(168, 85, 247, 0.2)' }}>
          <Info size={16} className="mt-0.5 flex-shrink-0" style={{ color: '#a855f7' }} />
          <span style={hintStyle}>
            CloakBrowser dependency installation (Python, pip, Xvfb) is only available through the CLI.
            Run <code style={{ color: 'var(--text-primary)' }}>astonish config browser</code> for guided setup.
          </span>
        </div>
      )}

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
