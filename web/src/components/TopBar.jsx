import { Moon, Sun, Settings, Cpu, Grid, Home, Sparkles } from 'lucide-react'

export default function TopBar({ theme, onToggleTheme, onOpenSettings, defaultProvider, defaultModel, currentView, onNavigate }) {
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
          onClick={() => onNavigate && onNavigate('home')}
          className={`hidden md:flex items-center gap-2 px-3 py-2 rounded-xl transition-all ${
            currentView === 'home' 
              ? '' 
              : 'hover:bg-purple-500/10'
          }`}
          style={{ 
            background: currentView === 'home' ? 'var(--accent)' : (theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'var(--bg-tertiary)'), 
            color: currentView === 'home' ? '#fff' : 'var(--text-secondary)' 
          }}
        >
          <Home size={14} />
          <span className="text-xs font-medium">Home</span>
        </button>

        <button 
          onClick={() => onNavigate && onNavigate('canvas')}
          className={`hidden md:flex items-center gap-2 px-3 py-2 rounded-xl transition-all ${
            currentView === 'canvas' 
              ? '' 
              : 'hover:bg-purple-500/10'
          }`}
          style={{ 
            background: currentView === 'canvas' ? 'var(--accent)' : (theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'var(--bg-tertiary)'), 
            color: currentView === 'canvas' ? '#fff' : 'var(--text-secondary)' 
          }}
        >
          <Grid size={14} />
          <span className="text-xs font-medium">Flows</span>
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

