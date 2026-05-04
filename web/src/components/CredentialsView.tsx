import { lazy, Suspense } from 'react'
import { Loader2 } from 'lucide-react'

const CredentialsSettings = lazy(() => import('./settings/CredentialsSettings'))

/**
 * Top-level Credentials view — rendered at #/credentials.
 * Wraps the existing CredentialsSettings component in a full-page layout.
 */
export default function CredentialsView() {
  return (
    <div className="flex flex-col h-full" style={{ background: 'var(--bg-primary)', color: 'var(--text-primary)' }}>
      <Suspense fallback={
        <div className="flex items-center justify-center h-full">
          <Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} />
        </div>
      }>
        <CredentialsSettings />
      </Suspense>
    </div>
  )
}
