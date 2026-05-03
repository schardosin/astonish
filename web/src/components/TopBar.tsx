import { useState, useRef, useEffect } from 'react'
import { Moon, Sun, Settings, Cpu, Grid, MessageSquare, Rocket, ShieldCheck, ShieldAlert, Crosshair, AppWindow, Users, BookOpen, FileText, ChevronDown, LogOut } from 'lucide-react'

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
  isPlatformMode?: boolean
  user?: { id: string, email: string, display_name: string, role: string } | null
  org?: { id: string, name: string, slug: string } | null
  activeTeam?: string | null
  teams?: { slug: string, name: string }[] | null
  onTeamChange?: (teamSlug: string) => void
  onLogout?: () => void
}

function useClickOutside(ref: any, handler: () => void) {
  useEffect(() => {
    const listener = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) handler()
    }
    document.addEventListener('mousedown', listener)
    return () => document.removeEventListener('mousedown', listener)
  }, [ref, handler])
}

export default function TopBar({ theme, onToggleTheme, onOpenSettings, onOpenSandbox, defaultProvider, defaultModel, currentView, onNavigate, sandboxStatus, isPlatformMode, user, org, activeTeam, teams, onTeamChange, onLogout }: TopBarProps) {
  const navBackground = theme === 'dark' ? 'rgba(15, 23, 42, 0.92)' : 'rgba(255,255,255,0.86)'
  const navBorder = theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'var(--border-color)'
  const inactiveBg = theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'var(--bg-tertiary)'

  const [teamOpen, setTeamOpen] = useState(false)
  const [userOpen, setUserOpen] = useState(false)
  const teamRef = useRef<HTMLDivElement>(null)
  const userRef = useRef<HTMLDivElement>(null)
  useClickOutside(teamRef, () => setTeamOpen(false))
  useClickOutside(userRef, () => setUserOpen(false))

  const nav = (view: string) => onNavigate && onNavigate(view)
  const activeTeamName = teams?.find(t => t.slug === activeTeam)?.name || activeTeam || 'No team'
  const initials = user?.display_name ? user.display_name.split(' ').map(w => w[0]).join('').slice(0, 2).toUpperCase() : '?'

  const navBtn = (view: string, label: string, Icon: any, gradient: string) => (
    <button
      onClick={() => nav(view)}
      className={`hidden md:flex items-center gap-2 px-3 py-2 rounded-xl transition-all ${currentView === view ? 'shadow-md' : 'hover:bg-purple-500/10'}`}
      style={{ background: currentView === view ? gradient : inactiveBg, color: currentView === view ? '#fff' : 'var(--text-secondary)' }}
    >
      <Icon size={14} />
      <span className="text-xs font-medium">{label}</span>
    </button>
  )

  const dropdownStyle = {
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border-color)',
    color: 'var(--text-primary)',
    boxShadow: '0 8px 24px rgba(0,0,0,0.25)',
  }

  return (
    <div
      className="h-14 flex items-center justify-between px-4"
      style={{ background: navBackground, backdropFilter: 'blur(12px)', WebkitBackdropFilter: 'blur(12px)', borderBottom: `1px solid ${navBorder}` }}
    >
      <div className="flex items-center gap-2">
        <div className="flex items-center gap-2 pr-3 rounded-xl" style={{ color: 'var(--accent)' }}>
          <img src="/astonish-logo.svg" alt="Astonish" className="w-6 h-6" />
          <span className="text-base font-semibold" style={{ color: 'var(--text-primary)' }}>Astonish Studio</span>
        </div>

        {navBtn('chat', 'Chat', MessageSquare, 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)')}
        {navBtn('canvas', 'Flows', Grid, 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)')}
        {navBtn('fleet', 'Fleet', Rocket, 'linear-gradient(135deg, #06b6d4 0%, #0891b2 100%)')}
        {navBtn('drill', 'Drill', Crosshair, 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)')}
        {navBtn('apps', 'Apps', AppWindow, 'linear-gradient(135deg, #10b981 0%, #059669 100%)')}

        {isPlatformMode && (
          <>
            {navBtn('team-mgmt', 'Team', Users, 'linear-gradient(135deg, #3b82f6 0%, #2563eb 100%)')}
            {navBtn('knowledge', 'Knowledge', BookOpen, 'linear-gradient(135deg, #10b981 0%, #059669 100%)')}
            {user?.role === 'admin' && navBtn('audit', 'Audit', FileText, 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)')}
          </>
        )}
      </div>

      {(defaultProvider || defaultModel) && (
        <div className="flex items-center gap-2 px-3 py-1 rounded-xl shadow-sm" style={{ background: theme === 'dark' ? 'rgba(255,255,255,0.05)' : 'var(--bg-tertiary)' }}>
          <Cpu size={16} className="text-purple-400" />
          <div className="flex flex-col leading-tight">
            <span className="text-xs font-medium" style={{ color: 'var(--text-muted)' }}>{defaultProvider || 'Not configured'}</span>
            <span className="text-xs font-mono" style={{ color: 'var(--text-secondary)' }}>{defaultModel || 'No model set'}</span>
          </div>
        </div>
      )}

      <div className="flex items-center gap-2">
        {isPlatformMode && teams && teams.length > 0 && (
          <div ref={teamRef} className="relative">
            <button
              onClick={() => setTeamOpen(!teamOpen)}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-medium transition-colors hover:opacity-80"
              style={{ background: 'var(--accent)', color: '#fff' }}
            >
              {activeTeamName}
              <ChevronDown size={12} />
            </button>
            {teamOpen && (
              <div className="absolute right-0 top-full mt-1 min-w-[180px] rounded-xl py-1 z-50" style={dropdownStyle}>
                {teams.map(t => (
                  <button
                    key={t.slug}
                    onClick={() => { onTeamChange && onTeamChange(t.slug); setTeamOpen(false) }}
                    className="w-full text-left px-3 py-2 text-xs transition-colors hover:opacity-80"
                    style={{ background: t.slug === activeTeam ? 'var(--bg-tertiary)' : 'transparent', color: 'var(--text-primary)' }}
                  >
                    {t.name}
                  </button>
                ))}
              </div>
            )}
          </div>
        )}

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
          {theme === 'dark' ? <Sun size={18} className="text-yellow-400" /> : <Moon size={18} className="text-gray-500" />}
        </button>

        <button
          onClick={onOpenSettings}
          className="p-2 rounded-full transition-colors hover:bg-purple-500/15"
          title="Settings"
          style={{ border: `1px solid ${navBorder}`, color: 'var(--text-secondary)' }}
        >
          <Settings size={18} />
        </button>

        {isPlatformMode && user && (
          <div ref={userRef} className="relative flex items-center gap-2 ml-1">
            <button onClick={() => setUserOpen(!userOpen)} className="flex items-center gap-2">
              <div
                className="w-8 h-8 rounded-full flex items-center justify-center text-xs font-bold"
                style={{ background: 'var(--accent)', color: '#fff' }}
              >
                {initials}
              </div>
              {org && <span className="text-xs hidden lg:inline" style={{ color: 'var(--text-muted)' }}>{org.name}</span>}
            </button>
            {userOpen && (
              <div className="absolute right-0 top-full mt-1 min-w-[220px] rounded-xl py-2 z-50" style={dropdownStyle}>
                <div className="px-3 py-2 border-b" style={{ borderColor: 'var(--border-color)' }}>
                  <div className="text-xs font-medium" style={{ color: 'var(--text-primary)' }}>{user.display_name}</div>
                  <div className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>{user.email}</div>
                  <span
                    className="inline-block mt-1.5 px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase"
                    style={{ background: 'var(--accent)', color: '#fff' }}
                  >
                    {user.role}
                  </span>
                </div>
                <button
                  onClick={() => { onLogout && onLogout(); setUserOpen(false) }}
                  className="w-full flex items-center gap-2 px-3 py-2 text-xs text-left transition-colors hover:opacity-80"
                  style={{ color: '#ef4444' }}
                >
                  <LogOut size={14} />
                  Logout
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
