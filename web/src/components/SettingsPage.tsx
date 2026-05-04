import { ChevronRight, X, Download } from 'lucide-react'
import SettingsContent from './settings/SettingsContent'
import { PREFERENCE_ITEMS, RESOURCE_ITEMS, ADMIN_ITEMS } from './settings/settingsMenuItems'
import type { SettingsMenuItem } from './settings/settingsMenuItems'
import { useSettingsData } from '../hooks/useSettingsData'
import type { UpdateInfo } from './settings/settingsApi'

declare const __UI_VERSION__: string

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
  /** Whether running in platform (multi-tenant) mode */
  isPlatformMode?: boolean
  /** User role — used to show/hide Administration section in platform mode */
  userRole?: string
}

interface MenuCategory {
  label?: string
  items: SettingsMenuItem[]
}

export default function SettingsPage({
  onClose,
  activeSection: externalActiveSection,
  onSectionChange,
  onToolsRefresh,
  onSettingsSaved,
  updateAvailable = null,
  onUpdateClick = null,
  appVersion = 'dev',
  theme = 'dark',
  isPlatformMode = false,
  userRole = '',
}: SettingsPageProps) {
  const isAdmin = userRole === 'admin' || userRole === 'owner'

  // In platform mode: show Preferences + (if admin) Administration
  // In personal mode: show all settings with category headers
  const categories: MenuCategory[] = isPlatformMode
    ? isAdmin
      ? [
          { label: 'Preferences', items: PREFERENCE_ITEMS },
          { label: 'Administration', items: ADMIN_ITEMS },
        ]
      : [{ items: PREFERENCE_ITEMS }]
    : [
        { label: 'Preferences', items: PREFERENCE_ITEMS },
        { label: 'Resources', items: RESOURCE_ITEMS },
        { label: 'System', items: ADMIN_ITEMS },
      ]

  const allItems = categories.flatMap(c => c.items)
  const defaultSection = isPlatformMode ? 'chat' : 'general'
  const activeSection = externalActiveSection && allItems.some(i => i.id === externalActiveSection)
    ? externalActiveSection
    : defaultSection

  const data = useSettingsData(activeSection)

  const activeLabel = allItems.find(item => item.id === activeSection)?.label || ''

  if (data.loading) {
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
        {/* Header with close button */}
        <div className="p-4 border-b flex items-center justify-between" style={{ borderColor: 'var(--border-color)' }}>
          <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
            {isPlatformMode ? 'Preferences' : 'Settings'}
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 rounded-lg transition-colors"
            style={{ color: 'var(--text-muted)', border: '1px solid var(--border-color)', background: 'var(--bg-tertiary)' }}
          >
            <X size={18} />
          </button>
        </div>

        {/* Menu items with category headers */}
        <nav className="flex-1 p-2 space-y-0.5 overflow-y-auto">
          {categories.map((category, catIdx) => (
            <div key={catIdx}>
              {category.label && (
                <div className="px-3 pt-4 pb-1.5 text-[11px] font-semibold uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>
                  {category.label}
                </div>
              )}
              {category.items.map(item => {
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
            </div>
          ))}
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
            {activeLabel}
          </h3>
        </div>

        {/* Content */}
        <div className={activeSection === 'mcp' ? 'flex-1 overflow-hidden' : 'flex-1 overflow-y-auto p-6'}>
          <SettingsContent
            activeSection={activeSection}
            settings={data.settings}
            mcpConfig={data.mcpConfig}
            webCapableTools={data.webCapableTools}
            standardServers={data.standardServers}
            loadData={data.loadData}
            fullConfig={data.fullConfig}
            fullConfigLoading={data.fullConfigLoading}
            invalidateFullConfig={data.invalidateFullConfig}
            onToolsRefresh={onToolsRefresh}
            onSettingsSaved={onSettingsSaved}
            onSectionChange={onSectionChange}
            theme={theme}
          />
        </div>
      </div>
    </div>
  )
}
