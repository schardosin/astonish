import React, { useState, useCallback } from 'react'
import { Database, ArrowRight, Check, X, Loader2, AlertCircle, Eye, EyeOff } from 'lucide-react'
import {
  startMigration,
  subscribeMigrationProgress,
  type MigrationProgress,
  type MigrationSummary,
} from '../api/auth'

interface MigrationPageProps {
  defaultEmail?: string
  defaultName?: string
  onComplete: () => void
  onSkip: () => void
}

const CATEGORY_LABELS: Record<string, string> = {
  credentials: 'Credentials',
  sessions: 'Chat Sessions',
  apps: 'Apps',
  flows: 'Flows',
  scheduler: 'Scheduled Jobs',
  fleets: 'Fleet Templates & Plans',
  skills: 'Skills',
  memory: 'Memory & Knowledge',
}

const CATEGORY_ORDER = [
  'credentials',
  'sessions',
  'apps',
  'flows',
  'scheduler',
  'fleets',
  'skills',
  'memory',
]

export default function MigrationPage({
  defaultEmail = '',
  defaultName = '',
  onComplete,
  onSkip,
}: MigrationPageProps) {
  const [phase, setPhase] = useState<'setup' | 'migrating' | 'done' | 'error'>('setup')
  const [email, setEmail] = useState(defaultEmail)
  const [displayName, setDisplayName] = useState(defaultName)
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [progress, setProgress] = useState<Record<string, MigrationProgress>>({})
  const [summary, setSummary] = useState<MigrationSummary | null>(null)

  const handleMigrate = useCallback(async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')

    if (password !== confirmPassword) {
      setError('Passwords do not match')
      return
    }
    if (password.length < 8) {
      setError('Password must be at least 8 characters')
      return
    }

    setLoading(true)

    try {
      await startMigration(email, password, displayName)
      setPhase('migrating')

      // Subscribe to progress updates
      subscribeMigrationProgress(
        (p) => {
          setProgress((prev) => ({ ...prev, [p.category]: p }))
        },
        (s) => {
          setSummary(s)
          setPhase(s.success ? 'done' : 'error')
        },
        (err) => {
          setError(err.message)
          setPhase('error')
        }
      )
    } catch (err: any) {
      setError(err.message || 'Failed to start migration')
      setLoading(false)
    }
  }, [email, password, confirmPassword, displayName])

  // Setup phase: account creation form
  if (phase === 'setup') {
    return (
      <div className="min-h-screen flex items-center justify-center p-4" style={{ background: 'var(--bg-primary)' }}>
        <div className="w-full max-w-md" style={{ background: 'var(--bg-secondary)', borderRadius: '12px', border: '1px solid var(--border-primary)' }}>
          <div className="p-8">
            <div className="flex items-center gap-3 mb-2">
              <Database size={28} style={{ color: 'var(--accent-primary)' }} />
              <h1 className="text-xl font-bold" style={{ color: 'var(--text-primary)' }}>
                Migrate to Platform Mode
              </h1>
            </div>
            <p className="text-sm mb-6" style={{ color: 'var(--text-secondary)' }}>
              Existing data was found from your personal installation. Create your platform account
              to migrate your data to the database.
            </p>

            <form onSubmit={handleMigrate} className="space-y-4">
              <div>
                <label className="block text-sm font-medium mb-1" style={{ color: 'var(--text-primary)' }}>
                  Email
                </label>
                <input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  required
                  className="w-full px-3 py-2 rounded-lg text-sm"
                  style={{
                    background: 'var(--bg-primary)',
                    border: '1px solid var(--border-primary)',
                    color: 'var(--text-primary)',
                  }}
                />
              </div>

              <div>
                <label className="block text-sm font-medium mb-1" style={{ color: 'var(--text-primary)' }}>
                  Display Name
                </label>
                <input
                  type="text"
                  value={displayName}
                  onChange={(e) => setDisplayName(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg text-sm"
                  style={{
                    background: 'var(--bg-primary)',
                    border: '1px solid var(--border-primary)',
                    color: 'var(--text-primary)',
                  }}
                />
              </div>

              <div>
                <label className="block text-sm font-medium mb-1" style={{ color: 'var(--text-primary)' }}>
                  Password
                </label>
                <div className="relative">
                  <input
                    type={showPassword ? 'text' : 'password'}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    required
                    minLength={8}
                    className="w-full px-3 py-2 rounded-lg text-sm pr-10"
                    style={{
                      background: 'var(--bg-primary)',
                      border: '1px solid var(--border-primary)',
                      color: 'var(--text-primary)',
                    }}
                  />
                  <button
                    type="button"
                    onClick={() => setShowPassword(!showPassword)}
                    className="absolute right-2 top-1/2 -translate-y-1/2"
                    style={{ color: 'var(--text-tertiary)' }}
                  >
                    {showPassword ? <EyeOff size={16} /> : <Eye size={16} />}
                  </button>
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium mb-1" style={{ color: 'var(--text-primary)' }}>
                  Confirm Password
                </label>
                <input
                  type={showPassword ? 'text' : 'password'}
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  required
                  minLength={8}
                  className="w-full px-3 py-2 rounded-lg text-sm"
                  style={{
                    background: 'var(--bg-primary)',
                    border: '1px solid var(--border-primary)',
                    color: 'var(--text-primary)',
                  }}
                />
              </div>

              {error && (
                <div className="flex items-center gap-2 p-3 rounded-lg text-sm" style={{
                  background: 'var(--status-error-bg, rgba(239, 68, 68, 0.1))',
                  color: 'var(--status-error, #ef4444)',
                }}>
                  <AlertCircle size={16} />
                  {error}
                </div>
              )}

              <div className="flex gap-3 pt-2">
                <button
                  type="submit"
                  disabled={loading}
                  className="flex-1 flex items-center justify-center gap-2 px-4 py-2 rounded-lg text-sm font-medium"
                  style={{
                    background: 'var(--accent-primary)',
                    color: 'var(--text-on-accent, #fff)',
                    opacity: loading ? 0.7 : 1,
                  }}
                >
                  {loading ? <Loader2 size={16} className="animate-spin" /> : <ArrowRight size={16} />}
                  Migrate & Create Account
                </button>
              </div>

              <button
                type="button"
                onClick={onSkip}
                className="w-full text-center text-sm py-2"
                style={{ color: 'var(--text-tertiary)' }}
              >
                Skip — start with a clean state
              </button>
            </form>
          </div>
        </div>
      </div>
    )
  }

  // Migrating phase: progress display
  if (phase === 'migrating') {
    return (
      <div className="min-h-screen flex items-center justify-center p-4" style={{ background: 'var(--bg-primary)' }}>
        <div className="w-full max-w-md" style={{ background: 'var(--bg-secondary)', borderRadius: '12px', border: '1px solid var(--border-primary)' }}>
          <div className="p-8">
            <div className="flex items-center gap-3 mb-6">
              <Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent-primary)' }} />
              <h1 className="text-xl font-bold" style={{ color: 'var(--text-primary)' }}>
                Migrating Data...
              </h1>
            </div>

            <div className="space-y-3">
              {CATEGORY_ORDER.map((cat) => {
                const p = progress[cat]
                return (
                  <CategoryProgress
                    key={cat}
                    label={CATEGORY_LABELS[cat] || cat}
                    progress={p}
                  />
                )
              })}
            </div>
          </div>
        </div>
      </div>
    )
  }

  // Done phase: success or error
  return (
    <div className="min-h-screen flex items-center justify-center p-4" style={{ background: 'var(--bg-primary)' }}>
      <div className="w-full max-w-md" style={{ background: 'var(--bg-secondary)', borderRadius: '12px', border: '1px solid var(--border-primary)' }}>
        <div className="p-8">
          {summary?.success ? (
            <>
              <div className="flex items-center gap-3 mb-4">
                <Check size={28} style={{ color: 'var(--status-success, #22c55e)' }} />
                <h1 className="text-xl font-bold" style={{ color: 'var(--text-primary)' }}>
                  Migration Complete
                </h1>
              </div>
              <div className="space-y-2 mb-6">
                {CATEGORY_ORDER.map((cat) => {
                  const count = summary.categories[cat] ?? 0
                  if (count === 0) return null
                  return (
                    <div key={cat} className="flex justify-between text-sm" style={{ color: 'var(--text-secondary)' }}>
                      <span>{CATEGORY_LABELS[cat] || cat}</span>
                      <span className="font-medium" style={{ color: 'var(--text-primary)' }}>{count} items</span>
                    </div>
                  )
                })}
              </div>
            </>
          ) : (
            <>
              <div className="flex items-center gap-3 mb-4">
                <X size={28} style={{ color: 'var(--status-error, #ef4444)' }} />
                <h1 className="text-xl font-bold" style={{ color: 'var(--text-primary)' }}>
                  Migration Completed with Errors
                </h1>
              </div>
              {summary?.errors?.map((err, i) => (
                <div key={i} className="text-sm p-2 mb-2 rounded" style={{
                  background: 'var(--status-error-bg, rgba(239, 68, 68, 0.1))',
                  color: 'var(--status-error, #ef4444)',
                }}>
                  {err}
                </div>
              ))}
            </>
          )}

          <button
            onClick={onComplete}
            className="w-full flex items-center justify-center gap-2 px-4 py-2 rounded-lg text-sm font-medium"
            style={{
              background: 'var(--accent-primary)',
              color: 'var(--text-on-accent, #fff)',
            }}
          >
            <ArrowRight size={16} />
            Continue to Astonish
          </button>
        </div>
      </div>
    </div>
  )
}

// CategoryProgress shows the migration status of a single data category.
function CategoryProgress({ label, progress: p }: { label: string; progress?: MigrationProgress }) {
  const status = p?.status || 'pending'
  const current = p?.current ?? 0
  const total = p?.total ?? 0
  const pct = total > 0 ? Math.round((current / total) * 100) : 0

  return (
    <div className="flex items-center gap-3">
      <div className="w-5 flex-shrink-0">
        {status === 'done' && <Check size={16} style={{ color: 'var(--status-success, #22c55e)' }} />}
        {status === 'error' && <X size={16} style={{ color: 'var(--status-error, #ef4444)' }} />}
        {status === 'skipped' && <span className="text-xs" style={{ color: 'var(--text-tertiary)' }}>--</span>}
        {(status === 'migrating' || status === 'counting') && (
          <Loader2 size={16} className="animate-spin" style={{ color: 'var(--accent-primary)' }} />
        )}
        {status === 'pending' && <div className="w-3 h-3 rounded-full" style={{ background: 'var(--border-primary)' }} />}
      </div>

      <div className="flex-1 min-w-0">
        <div className="flex justify-between items-center text-sm">
          <span style={{ color: status === 'pending' ? 'var(--text-tertiary)' : 'var(--text-primary)' }}>
            {label}
          </span>
          <span className="text-xs" style={{ color: 'var(--text-tertiary)' }}>
            {status === 'migrating' && total > 0 ? `${current}/${total}` : ''}
            {status === 'done' && total > 0 ? `${total}` : ''}
            {status === 'skipped' ? 'skipped' : ''}
            {status === 'error' ? p?.error || 'failed' : ''}
          </span>
        </div>

        {status === 'migrating' && total > 0 && (
          <div className="mt-1 h-1 rounded-full overflow-hidden" style={{ background: 'var(--border-primary)' }}>
            <div
              className="h-full rounded-full transition-all duration-300"
              style={{ width: `${pct}%`, background: 'var(--accent-primary)' }}
            />
          </div>
        )}
      </div>
    </div>
  )
}
