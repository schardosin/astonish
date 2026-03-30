import { useState, useEffect } from 'react'
import { Settings, Key, Server, ChevronRight, X, Check, AlertCircle, Loader2, Store, GitBranch, Download, MessageSquare, Globe, Radio, Database, Brain, GitFork, Wand2, Clock, Shield, User, KeyRound, Terminal, Box } from 'lucide-react'
import FlowStorePanel from './FlowStorePanel'
import { fetchFullConfig, fetchSettings, fetchMCPConfig, fetchWebCapableTools, saveSettings } from './settings/settingsApi'
import type { FullConfig, SettingsData, MCPConfigData, MCPServerConfig, WebCapableTools, StandardServer, UpdateInfo } from './settings/settingsApi'
import ChatSettings from './settings/ChatSettings'
import BrowserSettings from './settings/BrowserSettings'
import ChannelsSettings from './settings/ChannelsSettings'
import SessionsSettings from './settings/SessionsSettings'
import MemorySettings from './settings/MemorySettings'
import SubAgentsSettings from './settings/SubAgentsSettings'
import SkillsSettings from './settings/SkillsSettings'
import SchedulerSettings from './settings/SchedulerSettings'
import DaemonSettings from './settings/DaemonSettings'
import IdentitySettings from './settings/IdentitySettings'
import CredentialsSettings from './settings/CredentialsSettings'
import OpenCodeSettings from './settings/OpenCodeSettings'
import SandboxSettings from './settings/SandboxSettings'
import GeneralSettings from './settings/GeneralSettings'
import ProvidersSettings from './settings/ProvidersSettings'
import MCPServersSettings from './settings/MCPServersSettings'
import TapsSettings from './settings/TapsSettings'

interface SettingsPageProps {
  onClose: () => void
  activeSection?: string
  onSectionChange?: (section: string) => void
  onToolsRefresh?: () => void
  onSettingsSaved?: () => void
  updateAvailable?: UpdateInfo | null
  onUpdateClick?: (() => void) | null
  appVersion?: string
  theme?: string
}

// Section keys that use the full config API
const FULL_CONFIG_SECTIONS = ['chat', 'browser', 'channels', 'sessions', 'memory', 'sub_agents', 'skills', 'scheduler', 'daemon', 'sandbox', 'identity', 'open_code']

export default function SettingsPage({ onClose, activeSection = 'general', onSectionChange, onToolsRefresh, onSettingsSaved, updateAvailable = null, onUpdateClick = null, appVersion = 'dev', theme = 'dark' }: SettingsPageProps) {
  const [settings, setSettings] = useState<SettingsData | null>(null)
  const [mcpConfig, setMcpConfig] = useState<MCPConfigData | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Form state
  const [generalForm, setGeneralForm] = useState({ 
    default_provider: '', 
    default_model: '',
    web_search_tool: '',
    web_extract_tool: '',
    timezone: ''
  })
  const [providerForms, setProviderForms] = useState<Record<string, Record<string, string>>>({})
  const [mcpServers, setMcpServers] = useState<Record<string, MCPServerConfig>>({})
  const [mcpServerNames, setMcpServerNames] = useState<Record<string, string>>({})
  const [mcpServerArgs, setMcpServerArgs] = useState<Record<string, string>>({})
  const [mcpHasChanges, setMcpHasChanges] = useState(false)

  // Web-capable tools state
  const [webCapableTools, setWebCapableTools] = useState<WebCapableTools>({ webSearch: [], webExtract: [] })

  // Standard servers state
  const [standardServers, setStandardServers] = useState<StandardServer[]>([])

  // Full config state for new settings sections
  const [fullConfig, setFullConfig] = useState<FullConfig | null>(null)
  const [fullConfigLoading, setFullConfigLoading] = useState(false)

  useEffect(() => {
    loadData()
  }, [])

  // Load full config when a new settings section is opened
  useEffect(() => {
    if (FULL_CONFIG_SECTIONS.includes(activeSection) && !fullConfig) {
      setFullConfigLoading(true)
      fetchFullConfig()
        .then(data => setFullConfig(data))
        .catch((err: any) => setError(err.message))
        .finally(() => setFullConfigLoading(false))
    }
  }, [activeSection, fullConfig])

  const loadData = async () => {
    setLoading(true)
    try {
      const [settingsData, mcpData, webTools, stdServers] = await Promise.all([
        fetchSettings(),
        fetchMCPConfig(),
        fetchWebCapableTools().catch(() => ({ webSearch: [], webExtract: [] })),
        fetch('/api/standard-servers').then(r => r.ok ? r.json() : { servers: [] }).catch(() => ({ servers: [] }))
      ])
      setSettings(settingsData)
      setMcpConfig(mcpData)
      setWebCapableTools(webTools)
      setStandardServers(stdServers.servers || [])
      setGeneralForm({
        default_provider: settingsData.general.default_provider || '',
        default_model: settingsData.general.default_model || '',
        web_search_tool: settingsData.general.web_search_tool || '',
        web_extract_tool: settingsData.general.web_extract_tool || '',
        timezone: settingsData.general.timezone || ''
      })
      // Initialize provider forms
      const pForms: Record<string, Record<string, string>> = {}
      settingsData.providers.forEach(p => {
        pForms[p.name] = { ...p.fields }
      })
      setProviderForms(pForms)
      const servers = mcpData.mcpServers || {}
      setMcpServers(servers)
      // Initialize editable names and args
      const names: Record<string, string> = {}
      const args: Record<string, string> = {}
      Object.entries(servers).forEach(([name, server]) => {
        names[name] = name
        args[name] = (server.args || []).join(', ')
      })
      setMcpServerNames(names)
      setMcpServerArgs(args)
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  const handleSaveGeneral = async () => {
    setSaving(true)
    setSaveSuccess(false)
    try {
      await saveSettings({ general: generalForm })
      setSaveSuccess(true)
      if (onSettingsSaved) onSettingsSaved()
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err: any) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const menuItems = [
    { id: 'general', label: 'General', icon: Settings },
    { id: 'providers', label: 'Providers', icon: Key },
    { id: 'credentials', label: 'Credentials', icon: KeyRound },
    { id: 'chat', label: 'Chat', icon: MessageSquare },
    { id: 'browser', label: 'Browser', icon: Globe },
    { id: 'channels', label: 'Channels', icon: Radio },
    { id: 'mcp', label: 'MCP Servers', icon: Server },
    { id: 'sessions', label: 'Sessions', icon: Database },
    { id: 'memory', label: 'Memory', icon: Brain },
    { id: 'sub_agents', label: 'Sub-Agents', icon: GitFork },
    { id: 'open_code', label: 'OpenCode', icon: Terminal },
    { id: 'skills', label: 'Skills', icon: Wand2 },
    { id: 'scheduler', label: 'Scheduler', icon: Clock },
    { id: 'daemon', label: 'Daemon', icon: Shield },
    { id: 'sandbox', label: 'Sandbox', icon: Box },
    { id: 'identity', label: 'Agent Identity', icon: User },
    { id: 'taps', label: 'Repositories', icon: GitBranch },
    { id: 'flows', label: 'Flow Store', icon: Store },
  ]

  if (loading) {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center" style={{ background: 'rgba(0,0,0,0.8)' }}>
        <div className="text-white">Loading settings...</div>
      </div>
    )
  }

  return (
    <div className="fixed inset-0 z-50 flex" style={{ background: 'var(--bg-primary)' }}>
      {/* Left Sidebar */}
      <div className="w-64 border-r flex flex-col" style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}>
        <div className="p-4 border-b flex items-center justify-between" style={{ borderColor: 'var(--border-color)' }}>
          <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>Settings</h2>
          <button
            onClick={onClose}
            className="p-1.5 rounded-lg transition-colors"
            style={{ color: 'var(--text-muted)', border: '1px solid var(--border-color)', background: 'var(--bg-tertiary)' }}
          >
            <X size={18} />
          </button>
        </div>

        <nav className="flex-1 p-2 space-y-1 overflow-y-auto">
          {menuItems.map(item => {
            const Icon = item.icon
            const isActive = activeSection === item.id
            return (
              <button
                key={item.id}
                onClick={() => onSectionChange ? onSectionChange(item.id) : null}
                className="w-full flex items-center gap-3 px-3 py-2 rounded-lg transition-all"
                style={{
                  background: isActive ? 'var(--accent-soft)' : 'transparent',
                  color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
                  border: `1px solid ${isActive ? 'rgba(95, 79, 178, 0.25)' : 'transparent'}`
                }}
              >
                <Icon size={18} />
                <span className="font-medium text-sm">{item.label}</span>
                {isActive && <ChevronRight size={16} className="ml-auto" />}
              </button>
            )
          })}
        </nav>

        {/* Update Available Indicator */}
        {updateAvailable && onUpdateClick && (
          <div className="p-3 border-t" style={{ borderColor: 'var(--border-color)' }}>
            <button
              onClick={onUpdateClick}
              className="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-all hover:scale-[1.02]"
              style={{ 
                background: 'linear-gradient(135deg, rgba(168, 85, 247, 0.2) 0%, rgba(124, 58, 237, 0.2) 100%)',
                border: '1px solid rgba(168, 85, 247, 0.3)'
              }}
            >
              <Download size={18} style={{ color: '#a855f7' }} />
              <div className="flex-1 text-left">
                <div className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>Update Available</div>
                <div className="text-xs" style={{ color: 'var(--text-muted)' }}>{updateAvailable.version}</div>
              </div>
            </button>
          </div>
        )}

        {/* Version Info */}
        <div className="p-3 border-t text-xs space-y-1" style={{ borderColor: 'var(--border-color)', color: 'var(--text-muted)' }}>
          <div className="opacity-60">App Version: {appVersion}</div>
          <div className="opacity-60">UI Version: {__UI_VERSION__}</div>
        </div>
      </div>

      {/* Right Content */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Header */}
        <div className="p-4 border-b flex items-center justify-between" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
          <h3 className="text-xl font-semibold" style={{ color: 'var(--text-primary)' }}>
            {menuItems.find(m => m.id === activeSection)?.label}
          </h3>
          {saveSuccess && (
            <div className="flex items-center gap-2 text-green-400 text-sm">
              <Check size={16} />
              Saved successfully!
            </div>
          )}
          {error && (
            <div className="flex items-center gap-2 text-sm" style={{ color: 'var(--danger)' }}>
              <AlertCircle size={16} />
              {error}
            </div>
          )}
        </div>

        {/* Content */}
        <div className={activeSection === 'mcp' ? 'flex-1 overflow-hidden' : 'flex-1 overflow-y-auto p-6'}>
          {activeSection === 'general' && (
            <GeneralSettings
              settings={settings}
              generalForm={generalForm}
              setGeneralForm={setGeneralForm}
              webCapableTools={webCapableTools}
              standardServers={standardServers}
              saving={saving}
              onSave={handleSaveGeneral}
              onSectionChange={onSectionChange}
            />
          )}

          {activeSection === 'providers' && (
            <ProvidersSettings
              settings={settings}
              providerForms={providerForms}
              setProviderForms={setProviderForms}
              generalForm={generalForm}
              setGeneralForm={setGeneralForm}
              saving={saving}
              setSaving={setSaving}
              setSaveSuccess={setSaveSuccess}
              error={error}
              setError={setError}
              loadData={loadData}
              onSettingsSaved={onSettingsSaved}
            />
          )}

          {activeSection === 'mcp' && (
            <MCPServersSettings
              mcpServers={mcpServers}
              setMcpServers={setMcpServers}
              mcpServerNames={mcpServerNames}
              setMcpServerNames={setMcpServerNames}
              mcpServerArgs={mcpServerArgs}
              setMcpServerArgs={setMcpServerArgs}
              setMcpHasChanges={setMcpHasChanges}
              standardServers={standardServers}
              saving={saving}
              setSaving={setSaving}
              setSaveSuccess={setSaveSuccess}
              setError={setError}
              onToolsRefresh={onToolsRefresh}
              loadData={loadData}
              setGeneralForm={setGeneralForm}
              theme={theme}
            />
          )}

          {activeSection === 'taps' && (
            <TapsSettings />
          )}

          {activeSection === 'flows' && (
            <FlowStorePanel />
          )}

          {/* New settings sections */}
          {FULL_CONFIG_SECTIONS.includes(activeSection) && fullConfigLoading && (
            <div className="flex items-center justify-center py-12">
              <Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} />
              <span className="ml-2 text-sm" style={{ color: 'var(--text-muted)' }}>Loading settings...</span>
            </div>
          )}

          {activeSection === 'chat' && fullConfig && (
            <ChatSettings config={fullConfig.chat} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'browser' && fullConfig && (
            <BrowserSettings config={fullConfig.browser} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'channels' && fullConfig && (
            <ChannelsSettings config={fullConfig.channels} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'sessions' && fullConfig && (
            <SessionsSettings config={fullConfig.sessions} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'memory' && fullConfig && (
            <MemorySettings config={fullConfig.memory} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'sub_agents' && fullConfig && (
            <SubAgentsSettings config={fullConfig.sub_agents} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'skills' && fullConfig && (
            <SkillsSettings config={fullConfig.skills} onSaved={() => setFullConfig(null)} theme={theme} />
          )}

          {activeSection === 'scheduler' && fullConfig && (
            <SchedulerSettings config={fullConfig.scheduler} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'daemon' && fullConfig && (
            <DaemonSettings config={fullConfig.daemon} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'sandbox' && fullConfig && (
            <SandboxSettings config={fullConfig.sandbox} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'identity' && fullConfig && (
            <IdentitySettings config={fullConfig.agent_identity} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'open_code' && fullConfig && (
            <OpenCodeSettings config={fullConfig.open_code} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'credentials' && (
            <CredentialsSettings />
          )}
        </div>
      </div>
    </div>
  )
}
