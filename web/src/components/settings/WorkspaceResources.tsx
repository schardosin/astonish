import { useState } from 'react'
import SettingsShell from './SettingsShell'
import SettingsContent from './SettingsContent'
import { RESOURCE_ITEMS } from './settingsMenuItems'
import { useSettingsData } from '../../hooks/useSettingsData'

interface WorkspaceResourcesProps {
  activeSection?: string
  onSectionChange?: (section: string) => void
  theme?: string
}

/**
 * Workspace Resources panel — renders team-scoped settings sections
 * (Credentials, Skills, Memory, Scheduler, Repositories, Flow Store).
 * Used inside the Workspace view's "Resources" tab.
 */
export default function WorkspaceResources({
  activeSection: externalSection,
  onSectionChange: externalOnChange,
  theme = 'dark',
}: WorkspaceResourcesProps) {
  const [internalSection, setInternalSection] = useState(RESOURCE_ITEMS[0].id)
  const activeSection = externalSection || internalSection
  const onSectionChange = externalOnChange || setInternalSection

  const data = useSettingsData(activeSection)

  const categories = [{ items: RESOURCE_ITEMS }]

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
        onSectionChange={onSectionChange}
        theme={theme}
      />
    </SettingsShell>
  )
}
