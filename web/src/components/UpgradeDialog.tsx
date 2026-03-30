import { Download, Terminal, ExternalLink } from 'lucide-react'

interface UpgradeDialogProps {
  info: { version: string; url: string }
  onClose: () => void
}

export default function UpgradeDialog({ info, onClose }: UpgradeDialogProps) {
  return (
    <div className="fixed inset-0 z-[200] flex items-center justify-center p-4" style={{ background: 'rgba(0,0,0,0.7)' }}>
      <div 
        className="rounded-xl w-full max-w-lg p-6 shadow-2xl"
        style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', border: '1px solid var(--border-color)' }}
      >
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-xl font-semibold flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
            <Download size={20} className="text-purple-500" />
            Update Available: {info.version}
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 rounded-lg hover:bg-gray-600/30 transition-colors"
            style={{ color: 'var(--text-muted)' }}
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="space-y-4">
          <p style={{ color: 'var(--text-secondary)' }}>
            A new version of Astonish is available. Choose one of the methods below to update:
          </p>

          {/* Option 1: Homebrew */}
          <div className="p-4 rounded-lg" style={{ background: 'var(--bg-tertiary)' }}>
            <div className="flex items-center gap-2 mb-2">
              <Terminal size={18} style={{ color: 'var(--accent)' }} />
              <span className="font-medium" style={{ color: 'var(--text-primary)' }}>Homebrew (Recommended)</span>
            </div>
            <code className="block px-3 py-2 rounded font-mono text-sm" style={{ background: 'var(--bg-primary)', color: 'var(--text-secondary)' }}>
              brew upgrade schardosin/astonish/astonish
            </code>
          </div>

          {/* Option 2: Shell Script */}
          <div className="p-4 rounded-lg" style={{ background: 'var(--bg-tertiary)' }}>
            <div className="flex items-center gap-2 mb-2">
              <Terminal size={18} style={{ color: 'var(--accent)' }} />
              <span className="font-medium" style={{ color: 'var(--text-primary)' }}>Install Script</span>
            </div>
            <code className="block px-3 py-2 rounded font-mono text-sm" style={{ background: 'var(--bg-primary)', color: 'var(--text-secondary)' }}>
              curl -sSL https://schardosin.github.io/astonish/install.sh | bash
            </code>
          </div>

          {/* Option 3: Manual Download */}
          <div className="p-4 rounded-lg" style={{ background: 'var(--bg-tertiary)' }}>
            <div className="flex items-center gap-2 mb-2">
              <Download size={18} style={{ color: 'var(--accent)' }} />
              <span className="font-medium" style={{ color: 'var(--text-primary)' }}>Manual Download</span>
            </div>
            <button
              onClick={() => window.open(info.url, '_blank')}
              className="flex items-center gap-2 text-sm underline hover:no-underline"
              style={{ color: 'var(--text-primary)' }}
            >
              <ExternalLink size={14} />
              Download from GitHub Releases
            </button>
          </div>
        </div>

        <div className="flex justify-end mt-6">
          <button
            onClick={onClose}
            className="px-4 py-2 rounded-lg font-medium transition-colors"
            style={{ 
              background: 'var(--bg-tertiary)', 
              color: 'var(--text-secondary)',
              border: '1px solid var(--border-color)'
            }}
          >
            Close
          </button>
        </div>
      </div>
    </div>
  )
}
