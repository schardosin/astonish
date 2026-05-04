import { useState } from 'react'
import SettingsShell from './SettingsShell'
import SettingsContent from './SettingsContent'
import { ADMIN_ITEMS } from './settingsMenuItems'
import { useSettingsData } from '../../hooks/useSettingsData'

interface WorkspaceAdminProps {
  activeSection?: string
  onSectionChange?: (section: string) => void
  onToolsRefresh?: () => void
  onSettingsSaved?: () => void
  theme?: string
}

/**
 * Workspace Administration panel — renders system-level settings sections
 * (General, Providers, MCP, Channels, Sessions, Sub-Agents, OpenCode, Browser, Daemon, Sandbox).
 * Used inside the Workspace view's "Administration" tab. Admin/owner only.
 */
export default function WorkspaceAdmin({
  activeSection: externalSection,
  onSectionChange: externalOnChange,
  onToolsRefresh,
  onSettingsSaved,
  theme = 'dark',
}: WorkspaceAdminProps) {
  const [internalSection, setInternalSection] = useState(ADMIN_ITEMS[0].id)
  const activeSection = externalSection || internalSection
  const onSectionChange = externalOnChange || setInternalSection

  const data = useSettingsData(activeSection)

  const categories = [{ items: ADMIN_ITEMS }]

  return (
    <SettingsShell
      categories={categories}
      activeSection={activeSection}
      onSectionChange={onSectionChange}
      noPadding={activeSection === 'mcp'}
    >
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
    </SettingsShell>
  )
}
