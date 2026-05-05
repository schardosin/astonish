import { useState, useRef, useEffect } from 'react'
import { Moon, Sun, Settings, Cpu, Grid, MessageSquare, Rocket, ShieldCheck, ShieldAlert, Crosshair, AppWindow, ChevronDown, LogOut, MoreHorizontal, Menu, X, KeyRound, FolderCog } from 'lucide-react'

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
  orgs?: { id: string, name: string, slug: string, role: string }[] | null
  onOrgSwitch?: (orgSlug: string) => void
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

// ---------------------------------------------------------------------------
// Nav item definitions (shared between inline buttons and drawers)
// ---------------------------------------------------------------------------

interface NavItem {
  view: string
  label: string
  Icon: any
  gradient: string
}

// Primary nav — always visible inline from md+ (most used)
const primaryNavItems: NavItem[] = [
  { view: 'chat', label: 'Chat', Icon: MessageSquare, gradient: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' },
  { view: 'canvas', label: 'Flows', Icon: Grid, gradient: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' },
]

// Secondary nav — visible inline from lg+ ; in "More" dropdown between md and lg
const secondaryNavItems: NavItem[] = [
  { view: 'fleet', label: 'Fleet', Icon: Rocket, gradient: 'linear-gradient(135deg, #06b6d4 0%, #0891b2 100%)' },
  { view: 'drill', label: 'Drill', Icon: Crosshair, gradient: 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)' },
  { view: 'apps', label: 'Apps', Icon: AppWindow, gradient: 'linear-gradient(135deg, #10b981 0%, #059669 100%)' },
  { view: 'credentials', label: 'Credentials', Icon: KeyRound, gradient: 'linear-gradient(135deg, #8b5cf6 0%, #6d28d9 100%)' },
]

// All core items combined (for mobile drawer)
const allCoreNavItems: NavItem[] = [...primaryNavItems, ...secondaryNavItems]

function getPlatformNavItems(): NavItem[] {
  const items: NavItem[] = []
  items.push({ view: 'team-mgmt', label: 'Management', Icon: FolderCog, gradient: 'linear-gradient(135deg, #3b82f6 0%, #2563eb 100%)' })
  return items
}

// Breakpoint thresholds (must match Tailwind classes used in the template)
const BP_MD = 768
const BP_LG = 1024
const BP_XL = 1312  // custom: aligns with min-[1312px] for platform nav inline

// Hook to track which breakpoint tier we're in
function useBreakpointTier(): 'sm' | 'md' | 'lg' | 'xl' {
  const [tier, setTier] = useState<'sm' | 'md' | 'lg' | 'xl'>(() => {
    if (typeof window === 'undefined') return 'xl'
    const w = window.innerWidth
    if (w < BP_MD) return 'sm'
    if (w < BP_LG) return 'md'
    if (w < BP_XL) return 'lg'
    return 'xl'
  })

  useEffect(() => {
    const update = () => {
      const w = window.innerWidth
      if (w < BP_MD) setTier('sm')
      else if (w < BP_LG) setTier('md')
      else if (w < BP_XL) setTier('lg')
      else setTier('xl')
    }
    window.addEventListener('resize', update)
    return () => window.removeEventListener('resize', update)
  }, [])

  return tier
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export default function TopBar({ theme, onToggleTheme, onOpenSettings, onOpenSandbox, defaultProvider, defaultModel, currentView, onNavigate, sandboxStatus, isPlatformMode, user, org, orgs, onOrgSwitch, activeTeam, teams, onTeamChange, onLogout }: TopBarProps) {
  const navBackground = theme === 'dark' ? 'rgba(15, 23, 42, 0.92)' : 'rgba(255,255,255,0.86)'
  const navBorder = theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'var(--border-color)'
  const inactiveBg = theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'var(--bg-tertiary)'

  const [teamOpen, setTeamOpen] = useState(false)
  const [userOpen, setUserOpen] = useState(false)
  const [moreOpen, setMoreOpen] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const teamRef = useRef<HTMLDivElement>(null)
  const userRef = useRef<HTMLDivElement>(null)
  const moreRef = useRef<HTMLDivElement>(null)
  useClickOutside(teamRef, () => setTeamOpen(false))
  useClickOutside(userRef, () => setUserOpen(false))
  useClickOutside(moreRef, () => setMoreOpen(false))

  const tier = useBreakpointTier()

  const nav = (view: string) => onNavigate && onNavigate(view)
  const activeTeamName = teams?.find(t => t.slug === activeTeam)?.name || activeTeam || 'No team'
  const initials = user?.display_name ? user.display_name.split(' ').map(w => w[0]).join('').slice(0, 2).toUpperCase() : '?'
  const platformItems = isPlatformMode ? getPlatformNavItems() : []

  // Build the "More" dropdown items based on current breakpoint tier:
  // - md (768–1024): secondary core items + platform items
  // - lg (1024–1280): platform items only (secondary are visible inline)
  // - xl+: nothing (More button is hidden via CSS)
  const moreItems: NavItem[] = tier === 'md'
    ? [...secondaryNavItems, ...platformItems]
    : platformItems

  const isMoreActive = moreItems.some(i => i.view === currentView)
  const showMore = moreItems.length > 0

  // Close mobile drawer on navigation
  const mobileNav = (view: string) => { nav(view); setMobileOpen(false) }

  // Close mobile drawer and More dropdown on window resize (breakpoint changes)
  useEffect(() => {
    const onResize = () => {
      if (window.innerWidth >= 768) setMobileOpen(false)
      // Close More dropdown on resize — contents differ per breakpoint tier
      setMoreOpen(false)
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  // -- Inline nav button (desktop) -------------------------------------------
  const navBtn = (item: NavItem, extraClass = 'hidden md:flex') => (
    <button
      key={item.view}
      onClick={() => nav(item.view)}
      className={`${extraClass} items-center gap-2 px-3 py-2 rounded-xl transition-all whitespace-nowrap ${currentView === item.view ? 'shadow-md' : 'hover:bg-purple-500/10'}`}
      style={{ background: currentView === item.view ? item.gradient : inactiveBg, color: currentView === item.view ? '#fff' : 'var(--text-secondary)' }}
    >
      <item.Icon size={14} />
      <span className="text-xs font-medium">{item.label}</span>
    </button>
  )

  // -- Dropdown nav item (inside "More" menu) ---------------------------------
  const navDropdownItem = (item: NavItem) => (
    <button
      key={item.view}
      onClick={() => { nav(item.view); setMoreOpen(false) }}
      className="w-full flex items-center gap-2.5 px-3 py-2 text-xs transition-colors hover:opacity-80"
      style={{
        background: currentView === item.view ? 'var(--bg-tertiary)' : 'transparent',
        color: currentView === item.view ? 'var(--accent)' : 'var(--text-primary)',
        fontWeight: currentView === item.view ? 600 : 400,
      }}
    >
      <item.Icon size={14} />
      {item.label}
    </button>
  )

  // -- Mobile drawer nav item -------------------------------------------------
  const mobileNavItem = (item: NavItem) => (
    <button
      key={item.view}
      onClick={() => mobileNav(item.view)}
      className="w-full flex items-center gap-3 px-4 py-3 rounded-xl transition-all"
      style={{
        background: currentView === item.view ? item.gradient : 'transparent',
        color: currentView === item.view ? '#fff' : 'var(--text-primary)',
      }}
    >
      <item.Icon size={16} />
      <span className="text-sm font-medium">{item.label}</span>
    </button>
  )

  const dropdownStyle = {
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border-color)',
    color: 'var(--text-primary)',
    boxShadow: '0 8px 24px rgba(0,0,0,0.25)',
  }

  return (
    <>
      {/* ================================================================== */}
      {/* Top Bar                                                            */}
      {/* ================================================================== */}
      <div
        className="h-14 flex items-center justify-between px-4 relative z-50"
        style={{ background: navBackground, backdropFilter: 'blur(12px)', WebkitBackdropFilter: 'blur(12px)', borderBottom: `1px solid ${navBorder}` }}
      >
        {/* ---- Left: Hamburger + Logo + Nav ---- */}
        <div className="flex items-center gap-2 min-w-0 shrink">
          {/* Hamburger — visible below md */}
          <button
            onClick={() => setMobileOpen(true)}
            className="flex md:hidden items-center justify-center p-2 rounded-lg transition-colors hover:bg-purple-500/10"
            style={{ color: 'var(--text-secondary)' }}
          >
            <Menu size={20} />
          </button>

          {/* Logo */}
          <div className="flex items-center gap-2 pr-2 rounded-xl shrink-0" style={{ color: 'var(--accent)' }}>
            <img src="/astonish-logo.svg" alt="Astonish" className="w-6 h-6" />
            <span className="text-base font-semibold hidden sm:inline whitespace-nowrap" style={{ color: 'var(--text-primary)' }}>Astonish Studio</span>
          </div>

          {/* Primary nav buttons (Chat, Flows) — always visible from md+ */}
          {primaryNavItems.map(item => navBtn(item, 'hidden md:flex'))}

          {/* Secondary nav buttons (Fleet, Drill, Apps) — visible from lg+ */}
          {secondaryNavItems.map(item => navBtn(item, 'hidden lg:flex'))}

          {/* Platform nav buttons — inline only at xl tier (1312px+) */}
          {isPlatformMode && tier === 'xl' && platformItems.map(item => navBtn(item, 'hidden md:flex'))}

          {/* "More" overflow dropdown — visible from md up, only when tier is not xl
              Contents adapt: at md–lg includes secondary+platform; at lg–xl includes platform only
              At xl+ tier, all items are inline so More is not needed */}
          {showMore && tier !== 'xl' && (
            <div ref={moreRef} className="relative hidden md:block">
              <button
                onClick={() => setMoreOpen(!moreOpen)}
                className={`flex items-center gap-1.5 px-3 py-2 rounded-xl transition-all whitespace-nowrap ${isMoreActive ? 'shadow-md' : 'hover:bg-purple-500/10'}`}
                style={{
                  background: isMoreActive ? 'linear-gradient(135deg, #6366f1 0%, #4f46e5 100%)' : inactiveBg,
                  color: isMoreActive ? '#fff' : 'var(--text-secondary)',
                }}
              >
                <MoreHorizontal size={14} />
                <span className="text-xs font-medium">More</span>
              </button>
              {moreOpen && (
                <div className="absolute left-0 top-full mt-1 min-w-[160px] rounded-xl py-1 z-[60]" style={dropdownStyle}>
                  {moreItems.map(item => navDropdownItem(item))}
                </div>
              )}
            </div>
          )}
        </div>

        {/* ---- Center: Model / Provider chip — hidden below 2xl (1536px) ---- */}
        {(defaultProvider || defaultModel) && (
          <div className="hidden 2xl:flex items-center gap-2 px-3 py-1 rounded-xl shadow-sm shrink-0 mx-2" style={{ background: theme === 'dark' ? 'rgba(255,255,255,0.05)' : 'var(--bg-tertiary)' }}>
            <Cpu size={16} className="text-purple-400" />
            <div className="flex flex-col leading-tight">
              <span className="text-xs font-medium" style={{ color: 'var(--text-muted)' }}>{defaultProvider || 'Not configured'}</span>
              <span className="text-xs font-mono" style={{ color: 'var(--text-secondary)' }}>{defaultModel || 'No model set'}</span>
            </div>
          </div>
        )}

        {/* ---- Right: Team selector, action buttons, user avatar ---- */}
        <div className="flex items-center gap-2 shrink-0">
          {/* Team selector — hidden below md (available in mobile drawer instead) */}
          {isPlatformMode && teams && teams.length > 1 && (
            <div ref={teamRef} className="relative hidden md:block">
              <button
                onClick={() => setTeamOpen(!teamOpen)}
                className="flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-medium transition-colors hover:opacity-80"
                style={{ background: 'var(--accent)', color: '#fff' }}
              >
                <span className="max-w-[100px] truncate">{activeTeamName}</span>
                <ChevronDown size={12} />
              </button>
              {teamOpen && (
                <div className="absolute right-0 top-full mt-1 min-w-[180px] rounded-xl py-1 z-[60]" style={dropdownStyle}>
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

          {/* Sandbox badge */}
          {sandboxStatus && (() => {
            const isSecure = sandboxStatus.sandboxEnabled && sandboxStatus.incusAvailable && sandboxStatus.baseTemplateExists
            return (
              <button
                onClick={onOpenSandbox}
                className="hidden sm:flex p-2 rounded-full transition-colors hover:bg-purple-500/15"
                title={isSecure ? 'Sandbox: Secure — sessions run in isolated containers' : 'Sandbox: Disabled — sessions run on host (click to configure)'}
                style={{ border: `1px solid ${navBorder}`, color: isSecure ? '#22c55e' : '#f59e0b' }}
              >
                {isSecure ? <ShieldCheck size={18} /> : <ShieldAlert size={18} />}
              </button>
            )
          })()}

          {/* Theme toggle */}
          <button
            onClick={onToggleTheme}
            className="hidden sm:flex p-2 rounded-full transition-colors hover:bg-purple-500/15"
            title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
            style={{ border: `1px solid ${navBorder}`, color: 'var(--text-secondary)' }}
          >
            {theme === 'dark' ? <Sun size={18} className="text-yellow-400" /> : <Moon size={18} className="text-gray-500" />}
          </button>

          {/* Settings */}
          <button
            onClick={onOpenSettings}
            className="p-2 rounded-full transition-colors hover:bg-purple-500/15"
            title="Settings"
            style={{ border: `1px solid ${navBorder}`, color: 'var(--text-secondary)' }}
          >
            <Settings size={18} />
          </button>

          {/* User avatar — hidden below md (available in mobile drawer) */}
          {isPlatformMode && user && (
            <div ref={userRef} className="relative hidden md:flex items-center gap-2 ml-1">
              <button onClick={() => setUserOpen(!userOpen)} className="flex items-center gap-2">
                <div
                  className="w-8 h-8 rounded-full flex items-center justify-center text-xs font-bold"
                  style={{ background: 'var(--accent)', color: '#fff' }}
                >
                  {initials}
                </div>
              </button>
              {userOpen && (
                <div className="absolute right-0 top-full mt-1 min-w-[220px] rounded-xl py-2 z-[60]" style={dropdownStyle}>
                  <div className="px-3 py-2 border-b" style={{ borderColor: 'var(--border-color)' }}>
                    <div className="text-xs font-medium" style={{ color: 'var(--text-primary)' }}>{user.display_name}</div>
                    <div className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>{user.email}</div>
                    <div className="flex items-center gap-2 mt-1.5">
                      <span
                        className="inline-block px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase"
                        style={{ background: 'var(--accent)', color: '#fff' }}
                      >
                        {user.role}
                      </span>
                      {org && (
                        <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
                          {org.name || org.slug}
                        </span>
                      )}
                    </div>
                  </div>
                  {/* Org switcher — only when user belongs to multiple orgs */}
                  {orgs && orgs.length > 1 && (
                    <div className="border-b py-1" style={{ borderColor: 'var(--border-color)' }}>
                      <div className="px-3 py-1 text-[10px] uppercase tracking-wider font-semibold" style={{ color: 'var(--text-muted)' }}>Organization</div>
                      {orgs.map(o => (
                        <button
                          key={o.slug}
                          onClick={() => { onOrgSwitch && onOrgSwitch(o.slug); setUserOpen(false) }}
                          className="w-full text-left px-3 py-1.5 text-xs transition-colors hover:opacity-80"
                          style={{ background: o.slug === org?.slug ? 'var(--bg-tertiary)' : 'transparent', color: 'var(--text-primary)' }}
                        >
                          <div className="flex items-center justify-between">
                            <span className={o.slug === org?.slug ? 'font-semibold' : 'font-medium'}>{o.name}</span>
                            <span className="text-[10px] px-1.5 py-0.5 rounded" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>{o.role}</span>
                          </div>
                        </button>
                      ))}
                    </div>
                  )}
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

      {/* ================================================================== */}
      {/* Mobile Slide-out Drawer (< md)                                     */}
      {/* ================================================================== */}
      {mobileOpen && (
        <div className="fixed inset-0 z-[60] md:hidden">
          {/* Backdrop */}
          <div
            className="absolute inset-0 bg-black/60 backdrop-blur-sm"
            onClick={() => setMobileOpen(false)}
          />

          {/* Drawer panel */}
          <div
            className="absolute inset-y-0 left-0 w-72 flex flex-col shadow-2xl"
            style={{ background: 'var(--bg-secondary)' }}
          >
            {/* Drawer header */}
            <div className="h-14 flex items-center justify-between px-4 shrink-0" style={{ borderBottom: `1px solid ${navBorder}` }}>
              <div className="flex items-center gap-2" style={{ color: 'var(--accent)' }}>
                <img src="/astonish-logo.svg" alt="Astonish" className="w-6 h-6" />
                <span className="text-base font-semibold" style={{ color: 'var(--text-primary)' }}>Astonish</span>
              </div>
              <button
                onClick={() => setMobileOpen(false)}
                className="p-2 rounded-lg transition-colors hover:bg-purple-500/10"
                style={{ color: 'var(--text-muted)' }}
              >
                <X size={18} />
              </button>
            </div>

            {/* Scrollable nav content */}
            <div className="flex-1 overflow-y-auto py-2 px-3 space-y-1">
              {/* Core nav */}
              {allCoreNavItems.map(item => mobileNavItem(item))}

              {/* Platform nav (if applicable) */}
              {isPlatformMode && platformItems.length > 0 && (
                <>
                  <div className="my-2 mx-2 border-t" style={{ borderColor: 'var(--border-color)' }} />
                  <div className="px-4 py-1">
                    <span className="text-[10px] font-semibold uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>Platform</span>
                  </div>
                  {platformItems.map(item => mobileNavItem(item))}
                </>
              )}
            </div>

            {/* Drawer footer: team selector, theme, settings, user */}
            <div className="shrink-0 border-t px-3 py-3 space-y-3" style={{ borderColor: 'var(--border-color)' }}>
              {/* Team selector */}
              {isPlatformMode && teams && teams.length > 1 && (
                <div>
                  <div className="px-1 mb-1.5">
                    <span className="text-[10px] font-semibold uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>Team</span>
                  </div>
                  <select
                    value={activeTeam || ''}
                    onChange={e => { onTeamChange && onTeamChange(e.target.value); setMobileOpen(false) }}
                    className="w-full px-3 py-2 rounded-xl text-xs outline-none"
                    style={{ background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }}
                  >
                    {teams.map(t => <option key={t.slug} value={t.slug}>{t.name}</option>)}
                  </select>
                </div>
              )}

              {/* Org selector (mobile) */}
              {isPlatformMode && orgs && orgs.length > 1 && (
                <div>
                  <div className="px-1 mb-1.5">
                    <span className="text-[10px] font-semibold uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>Organization</span>
                  </div>
                  <select
                    value={org?.slug || ''}
                    onChange={e => { onOrgSwitch && onOrgSwitch(e.target.value); setMobileOpen(false) }}
                    className="w-full px-3 py-2 rounded-xl text-xs outline-none"
                    style={{ background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }}
                  >
                    {orgs.map(o => <option key={o.slug} value={o.slug}>{o.name}</option>)}
                  </select>
                </div>
              )}

              {/* Quick actions row */}
              <div className="flex items-center gap-2">
                {sandboxStatus && (() => {
                  const isSecure = sandboxStatus.sandboxEnabled && sandboxStatus.incusAvailable && sandboxStatus.baseTemplateExists
                  return (
                    <button
                      onClick={() => { onOpenSandbox(); setMobileOpen(false) }}
                      className="p-2 rounded-full transition-colors hover:bg-purple-500/15"
                      title="Sandbox"
                      style={{ border: `1px solid ${navBorder}`, color: isSecure ? '#22c55e' : '#f59e0b' }}
                    >
                      {isSecure ? <ShieldCheck size={18} /> : <ShieldAlert size={18} />}
                    </button>
                  )
                })()}
                <button
                  onClick={onToggleTheme}
                  className="p-2 rounded-full transition-colors hover:bg-purple-500/15"
                  title={theme === 'dark' ? 'Light mode' : 'Dark mode'}
                  style={{ border: `1px solid ${navBorder}`, color: 'var(--text-secondary)' }}
                >
                  {theme === 'dark' ? <Sun size={18} className="text-yellow-400" /> : <Moon size={18} className="text-gray-500" />}
                </button>
                <button
                  onClick={() => { onOpenSettings(); setMobileOpen(false) }}
                  className="p-2 rounded-full transition-colors hover:bg-purple-500/15"
                  title="Settings"
                  style={{ border: `1px solid ${navBorder}`, color: 'var(--text-secondary)' }}
                >
                  <Settings size={18} />
                </button>
              </div>

              {/* User info + logout */}
              {isPlatformMode && user && (
                <div className="flex items-center justify-between pt-2 border-t" style={{ borderColor: 'var(--border-color)' }}>
                  <div className="flex items-center gap-2 min-w-0">
                    <div
                      className="w-8 h-8 rounded-full flex items-center justify-center text-xs font-bold shrink-0"
                      style={{ background: 'var(--accent)', color: '#fff' }}
                    >
                      {initials}
                    </div>
                    <div className="min-w-0">
                      <div className="text-xs font-medium truncate" style={{ color: 'var(--text-primary)' }}>{user.display_name}</div>
                      <div className="text-[10px] truncate" style={{ color: 'var(--text-muted)' }}>{user.email}</div>
                    </div>
                  </div>
                  <button
                    onClick={() => { onLogout && onLogout(); setMobileOpen(false) }}
                    className="p-2 rounded-lg transition-colors hover:bg-red-500/10 shrink-0"
                    title="Logout"
                    style={{ color: '#ef4444' }}
                  >
                    <LogOut size={16} />
                  </button>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  )
}
