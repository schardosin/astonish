import React from 'react'
import { ChevronRight } from 'lucide-react'
import type { SettingsMenuItem } from './settingsMenuItems'

interface MenuCategory {
  label?: string
  items: SettingsMenuItem[]
}

interface SettingsShellProps {
  /** Menu items, optionally grouped into categories with headers */
  categories: MenuCategory[]
  /** Currently active section id */
  activeSection: string
  /** Callback when a section is selected */
  onSectionChange: (section: string) => void
  /** Section title displayed in the content header */
  title?: string
  /** Content to render in the right panel */
  children: React.ReactNode
  /** Whether the active section needs no padding (e.g. MCP) */
  noPadding?: boolean
}

export default function SettingsShell({
  categories,
  activeSection,
  onSectionChange,
  title,
  children,
  noPadding = false,
}: SettingsShellProps) {
  // Find the active item's label for the header
  const activeLabel = title || categories
    .flatMap(c => c.items)
    .find(item => item.id === activeSection)?.label || ''

  return (
    <div className="flex h-full" style={{ background: 'var(--bg-primary)' }}>
      {/* Left Sidebar */}
      <div className="w-56 flex-shrink-0 border-r flex flex-col overflow-hidden" style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}>
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
                    onClick={() => onSectionChange(item.id)}
                    className="w-full flex items-center gap-3 px-3 py-2 rounded-lg transition-all"
                    style={{
                      background: isActive ? 'var(--accent-soft)' : 'transparent',
                      color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
                      border: `1px solid ${isActive ? 'rgba(95, 79, 178, 0.25)' : 'transparent'}`
                    }}
                  >
                    <Icon size={16} />
                    <span className="font-medium text-sm">{item.label}</span>
                    {isActive && <ChevronRight size={14} className="ml-auto" />}
                  </button>
                )
              })}
            </div>
          ))}
        </nav>
      </div>

      {/* Right Content */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Header */}
        <div className="px-6 py-3 border-b flex items-center" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
          <h3 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
            {activeLabel}
          </h3>
        </div>

        {/* Content */}
        <div className={noPadding ? 'flex-1 overflow-hidden' : 'flex-1 overflow-y-auto p-6'}>
          {children}
        </div>
      </div>
    </div>
  )
}
