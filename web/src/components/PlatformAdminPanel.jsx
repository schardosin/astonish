import { useState, lazy, Suspense } from 'react'
import { Building2, Users, Crown, Shield, Globe, Loader2 } from 'lucide-react'
import { gradientAmber } from './platformAdmin/shared'

const OrgsTab = lazy(() => import('./platformAdmin/OrgsTab'))
const UsersTab = lazy(() => import('./platformAdmin/UsersTab'))
const AuthTab = lazy(() => import('./platformAdmin/AuthTab'))
const ChannelsTab = lazy(() => import('./platformAdmin/ChannelsTab'))

function TabFallback() {
  return (
    <div className="flex items-center justify-center py-12">
      <Loader2 size={24} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
    </div>
  )
}

export default function PlatformAdminPanel({ theme, activeTab: externalTab, onTabChange: externalOnTabChange = undefined }) {
  const [internalTab, setInternalTab] = useState('orgs')
  const activeTab = externalTab || internalTab
  const onTabChange = externalOnTabChange || setInternalTab

  return (
    <div className="flex flex-col h-full" style={{ background: 'var(--bg-primary)', color: 'var(--text-primary)' }}>
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b" style={{ borderColor: 'var(--border-color)' }}>
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-xl" style={gradientAmber}>
            <Crown size={20} className="text-white" />
          </div>
          <div>
            <h1 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>Platform Administration</h1>
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>Manage organizations and users across the platform</p>
          </div>
        </div>

        {/* Tabs in header area */}
        <div className="flex items-center gap-1">
          <button
            onClick={() => onTabChange('orgs')}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
            style={{
              background: activeTab === 'orgs' ? 'var(--accent-soft)' : 'transparent',
              color: activeTab === 'orgs' ? 'var(--accent)' : 'var(--text-muted)',
            }}
          >
            <Building2 size={13} /> Organizations
          </button>
          <button
            onClick={() => onTabChange('users')}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
            style={{
              background: activeTab === 'users' ? 'var(--accent-soft)' : 'transparent',
              color: activeTab === 'users' ? 'var(--accent)' : 'var(--text-muted)',
            }}
          >
            <Users size={13} /> Users
          </button>
          <button
            onClick={() => onTabChange('auth')}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
            style={{
              background: activeTab === 'auth' ? 'var(--accent-soft)' : 'transparent',
              color: activeTab === 'auth' ? 'var(--accent)' : 'var(--text-muted)',
            }}
          >
            <Shield size={13} /> Authentication
          </button>
          <button
            onClick={() => onTabChange('channels')}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
            style={{
              background: activeTab === 'channels' ? 'var(--accent-soft)' : 'transparent',
              color: activeTab === 'channels' ? 'var(--accent)' : 'var(--text-muted)',
            }}
          >
            <Globe size={13} /> Channels
          </button>
        </div>
      </div>

      {/* Content */}
      <Suspense fallback={<TabFallback />}>
        {activeTab === 'orgs' && <OrgsTab />}
        {activeTab === 'users' && <UsersTab />}
        {activeTab === 'auth' && <AuthTab />}
        {activeTab === 'channels' && <ChannelsTab />}
      </Suspense>
    </div>
  )
}
