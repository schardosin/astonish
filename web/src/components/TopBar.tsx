import { Moon, Sun, Settings, Cpu, Grid, MessageSquare, Rocket, ShieldCheck, ShieldAlert, Crosshair } from 'lucide-react'

interface SandboxStatus {
  sandboxEnabled: boolean
  incusAvailable: boolean
  baseTemplateExists: boolean
}

interface TopBarProps {
  theme: 'dark' | 'light'
  onToggleTheme: () => void
  onOpenSettings: () => void
  onOpenSandbox: () => void
  defaultProvider: string | null
  defaultModel: string | null
  currentView: string
  onNavigate?: (view: string) => void
  sandboxStatus?: SandboxStatus | null
}

export default function TopBar({ theme, onToggleTheme, onOpenSettings, onOpenSandbox, defaultProvider, defaultModel, currentView, onNavigate, sandboxStatus }: TopBarProps) {
  const navBackground = theme === 'dark' ? 'rgba(15, 23, 42, 0.92)' : 'rgba(255,255,255,0.86)'
  const navBorder = theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'var(--border-color)'

  return (
    <div
      className="h-14 flex items-center justify-between px-4"
      style={{
        background: navBackground,
        backdropFilter: 'blur(12px)',
        WebkitBackdropFilter: 'blur(12px)',
        borderBottom: `1px solid ${navBorder}`
      }}
    >
      <div className="flex items-center gap-2">
        <div className="flex items-center gap-2 pr-3 rounded-xl" style={{ color: 'var(--accent)' }}>
          <img src="/astonish-logo.svg" alt="Astonish" className="w-6 h-6" />
          <span className="text-base font-semibold" style={{ color: 'var(--text-primary)' }}>Astonish Studio</span>
        </div>
        
        <button 
          onClick={() => onNavigate && onNavigate('chat')}
          className={`hidden md:flex items-center gap-2 px-3 py-2 rounded-xl transition-all ${
            currentView === 'chat' 
              ? 'shadow-md' 
              : 'hover:bg-purple-500/10'
          }`}
          style={{ 
            background: currentView === 'chat' ? 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' : (theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'var(--bg-tertiary)'), 
            color: currentView === 'chat' ? '#fff' : 'var(--text-secondary)' 
          }}
        >
          <MessageSquare size={14} />
          <span className="text-xs font-medium">Chat</span>
        </button>

        <button 
          onClick={() => onNavigate && onNavigate('canvas')}
          className={`hidden md:flex items-center gap-2 px-3 py-2 rounded-xl transition-all ${
            currentView === 'canvas' 
              ? 'shadow-md' 
              : 'hover:bg-purple-500/10'
          }`}
          style={{ 
            background: currentView === 'canvas' ? 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' : (theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'var(--bg-tertiary)'), 
            color: currentView === 'canvas' ? '#fff' : 'var(--text-secondary)' 
          }}
        >
          <Grid size={14} />
          <span className="text-xs font-medium">Flows</span>
        </button>

        <button 
          onClick={() => onNavigate && onNavigate('fleet')}
          className={`hidden md:flex items-center gap-2 px-3 py-2 rounded-xl transition-all ${
            currentView === 'fleet' 
              ? 'shadow-md' 
              : 'hover:bg-purple-500/10'
          }`}
          style={{ 
            background: currentView === 'fleet' ? 'linear-gradient(135deg, #06b6d4 0%, #0891b2 100%)' : (theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'var(--bg-tertiary)'), 
            color: currentView === 'fleet' ? '#fff' : 'var(--text-secondary)' 
          }}
        >
          <Rocket size={14} />
          <span className="text-xs font-medium">Fleet</span>
        </button>

        <button 
          onClick={() => onNavigate && onNavigate('drill')}
          className={`hidden md:flex items-center gap-2 px-3 py-2 rounded-xl transition-all ${
            currentView === 'drill' 
              ? 'shadow-md' 
              : 'hover:bg-purple-500/10'
          }`}
          style={{ 
            background: currentView === 'drill' ? 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)' : (theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'var(--bg-tertiary)'), 
            color: currentView === 'drill' ? '#fff' : 'var(--text-secondary)' 
          }}
        >
          <Crosshair size={14} />
          <span className="text-xs font-medium">Drill</span>
        </button>
      </div>

      {(defaultProvider || defaultModel) && (
        <div
          className="flex items-center gap-2 px-3 py-1 rounded-xl shadow-sm"
          style={{ background: theme === 'dark' ? 'rgba(255,255,255,0.05)' : 'var(--bg-tertiary)' }}
        >
          <Cpu size={16} className="text-purple-400" />
          <div className="flex flex-col leading-tight">
            <span className="text-xs font-medium" style={{ color: 'var(--text-muted)' }}>
              {defaultProvider || 'Not configured'}
            </span>
            <span className="text-xs font-mono" style={{ color: 'var(--text-secondary)' }}>
              {defaultModel || 'No model set'}
            </span>
          </div>
        </div>
      )}

      <div className="flex items-center gap-2">
        {sandboxStatus && (() => {
          const isSecure = sandboxStatus.sandboxEnabled && sandboxStatus.incusAvailable && sandboxStatus.baseTemplateExists
          return (
            <button
              onClick={onOpenSandbox}
              className="p-2 rounded-full transition-colors hover:bg-purple-500/15"
              title={isSecure ? 'Sandbox: Secure — sessions run in isolated containers' : 'Sandbox: Disabled — sessions run on host (click to configure)'}
              style={{ border: `1px solid ${navBorder}`, color: isSecure ? '#22c55e' : '#f59e0b' }}
            >
              {isSecure ? <ShieldCheck size={18} /> : <ShieldAlert size={18} />}
            </button>
          )
        })()}

        <button
          onClick={onToggleTheme}
          className="p-2 rounded-full transition-colors hover:bg-purple-500/15"
          title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
          style={{ border: `1px solid ${navBorder}`, color: 'var(--text-secondary)' }}
        >
          {theme === 'dark' ? (
            <Sun size={18} className="text-yellow-400" />
          ) : (
            <Moon size={18} className="text-gray-500" />
          )}
        </button>

        <button
          onClick={onOpenSettings}
          className="p-2 rounded-full transition-colors hover:bg-purple-500/15"
          title="Settings"
          style={{ border: `1px solid ${navBorder}`, color: 'var(--text-secondary)' }}
        >
          <Settings size={18} />
        </button>
      </div>
    </div>
  )
}
