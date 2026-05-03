import React, { useState, useEffect } from 'react'
import { LogIn, UserPlus, Loader2, AlertCircle, Eye, EyeOff } from 'lucide-react'
import { getSetupStatus, type SetupStatus } from '../api/auth'

interface LoginPageProps {
  onLogin: (email: string, password: string) => Promise<void>
  onRegister: (email: string, password: string, displayName: string) => Promise<void>
}

export default function LoginPage({ onLogin, onRegister }: LoginPageProps) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [setupStatus, setSetupStatus] = useState<SetupStatus | null>(null)

  useEffect(() => {
    getSetupStatus().then(status => {
      setSetupStatus(status)
      // If not initialized, default to register mode
      if (!status.initialized) {
        setMode('register')
      }
    }).catch(() => {
      // Failed to check — show login by default
    })
  }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)

    try {
      if (mode === 'register') {
        await onRegister(email, password, displayName)
      } else {
        await onLogin(email, password)
      }
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'An error occurred'
      setError(message)
    } finally {
      setLoading(false)
    }
  }

  const isFirstSetup = setupStatus && !setupStatus.initialized
  const canRegister = setupStatus?.allow_registration !== false

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ background: 'var(--bg-primary)' }}
    >
      <div
        className="w-full max-w-md mx-auto p-8 rounded-2xl shadow-2xl"
        style={{
          background: 'var(--bg-secondary)',
          border: '1px solid var(--border-color)',
        }}
      >
        {/* Logo / Title */}
        <div className="text-center mb-8">
          <div
            className="inline-flex items-center justify-center w-16 h-16 rounded-2xl mb-4"
            style={{
              background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)',
            }}
          >
            <span className="text-3xl text-white font-bold">A</span>
          </div>
          <h1
            className="text-2xl font-bold"
            style={{ color: 'var(--text-primary)' }}
          >
            {isFirstSetup ? 'Set Up Astonish' : 'Welcome to Astonish'}
          </h1>
          <p
            className="mt-2 text-sm"
            style={{ color: 'var(--text-muted)' }}
          >
            {isFirstSetup
              ? 'Create your admin account to get started'
              : mode === 'register'
                ? 'Create your account'
                : 'Sign in to your account'}
          </p>
        </div>

        {/* Error message */}
        {error && (
          <div
            className="flex items-center gap-2 p-3 rounded-lg mb-4 text-sm"
            style={{
              background: 'rgba(239, 68, 68, 0.1)',
              color: '#ef4444',
              border: '1px solid rgba(239, 68, 68, 0.2)',
            }}
          >
            <AlertCircle size={16} />
            <span>{error}</span>
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Display name (register only) */}
          {mode === 'register' && (
            <div>
              <label
                className="block text-sm font-medium mb-1.5"
                style={{ color: 'var(--text-secondary)' }}
              >
                Display Name
              </label>
              <input
                type="text"
                value={displayName}
                onChange={e => setDisplayName(e.target.value)}
                placeholder="Your name"
                className="w-full px-4 py-2.5 rounded-xl text-sm outline-none transition-all"
                style={{
                  background: 'var(--bg-tertiary)',
                  color: 'var(--text-primary)',
                  border: '1px solid var(--border-color)',
                }}
                autoFocus={mode === 'register'}
              />
            </div>
          )}

          {/* Email */}
          <div>
            <label
              className="block text-sm font-medium mb-1.5"
              style={{ color: 'var(--text-secondary)' }}
            >
              Email
            </label>
            <input
              type="email"
              value={email}
              onChange={e => setEmail(e.target.value)}
              placeholder="you@example.com"
              required
              className="w-full px-4 py-2.5 rounded-xl text-sm outline-none transition-all"
              style={{
                background: 'var(--bg-tertiary)',
                color: 'var(--text-primary)',
                border: '1px solid var(--border-color)',
              }}
              autoFocus={mode === 'login'}
            />
          </div>

          {/* Password */}
          <div>
            <label
              className="block text-sm font-medium mb-1.5"
              style={{ color: 'var(--text-secondary)' }}
            >
              Password
            </label>
            <div className="relative">
              <input
                type={showPassword ? 'text' : 'password'}
                value={password}
                onChange={e => setPassword(e.target.value)}
                placeholder={mode === 'register' ? 'At least 8 characters' : 'Enter your password'}
                required
                minLength={mode === 'register' ? 8 : undefined}
                className="w-full px-4 py-2.5 rounded-xl text-sm outline-none transition-all pr-10"
                style={{
                  background: 'var(--bg-tertiary)',
                  color: 'var(--text-primary)',
                  border: '1px solid var(--border-color)',
                }}
              />
              <button
                type="button"
                onClick={() => setShowPassword(!showPassword)}
                className="absolute right-3 top-1/2 -translate-y-1/2 opacity-50 hover:opacity-100 transition-opacity"
                style={{ color: 'var(--text-muted)' }}
              >
                {showPassword ? <EyeOff size={16} /> : <Eye size={16} />}
              </button>
            </div>
          </div>

          {/* Submit button */}
          <button
            type="submit"
            disabled={loading}
            className="w-full py-3 rounded-xl text-white font-medium text-sm flex items-center justify-center gap-2 transition-all hover:opacity-90 disabled:opacity-50"
            style={{
              background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)',
            }}
          >
            {loading ? (
              <Loader2 size={18} className="animate-spin" />
            ) : mode === 'register' ? (
              <>
                <UserPlus size={18} />
                {isFirstSetup ? 'Create Admin Account' : 'Create Account'}
              </>
            ) : (
              <>
                <LogIn size={18} />
                Sign In
              </>
            )}
          </button>
        </form>

        {/* Mode toggle */}
        {!isFirstSetup && canRegister && (
          <div className="mt-6 text-center">
            <span
              className="text-sm"
              style={{ color: 'var(--text-muted)' }}
            >
              {mode === 'login' ? "Don't have an account? " : 'Already have an account? '}
            </span>
            <button
              onClick={() => {
                setMode(mode === 'login' ? 'register' : 'login')
                setError('')
              }}
              className="text-sm font-medium hover:underline"
              style={{ color: '#a855f7' }}
            >
              {mode === 'login' ? 'Sign up' : 'Sign in'}
            </button>
          </div>
        )}

        {/* OIDC / SSO login */}
        {setupStatus?.auth_mode === 'oidc' && !isFirstSetup && (
          <div className="mt-4">
            <div className="flex items-center gap-3 mb-4">
              <div className="flex-1 h-px" style={{ background: 'var(--border-color)' }} />
              <span className="text-xs" style={{ color: 'var(--text-muted)' }}>or</span>
              <div className="flex-1 h-px" style={{ background: 'var(--border-color)' }} />
            </div>
            <button
              onClick={() => { window.location.href = '/api/auth/oidc/login' }}
              className="w-full py-3 rounded-xl font-medium text-sm flex items-center justify-center gap-2 transition-all hover:opacity-90"
              style={{
                background: 'var(--bg-tertiary)',
                color: 'var(--text-primary)',
                border: '1px solid var(--border-color)',
              }}
            >
              <LogIn size={18} />
              Sign in with SSO
            </button>
          </div>
        )}

        {/* First setup hint */}
        {isFirstSetup && (
          <p
            className="mt-4 text-xs text-center"
            style={{ color: 'var(--text-muted)' }}
          >
            This account will be the organization owner with full admin access.
          </p>
        )}
      </div>
    </div>
  )
}
