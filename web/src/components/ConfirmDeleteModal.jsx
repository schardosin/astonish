import { useEffect, useRef } from 'react'
import { X, AlertTriangle } from 'lucide-react'

/**
 * Confirmation dialog for deleting an agent
 */
export default function ConfirmDeleteModal({ isOpen, onClose, onConfirm, agentName }) {
  const cancelRef = useRef(null)

  // Focus cancel button when modal opens
  useEffect(() => {
    if (isOpen && cancelRef.current) {
      cancelRef.current.focus()
    }
  }, [isOpen])

  const handleKeyDown = (e) => {
    if (e.key === 'Escape') {
      onClose()
    }
  }

  if (!isOpen) return null

  return (
    <div 
      className="fixed inset-0 z-50 flex items-center justify-center"
      onKeyDown={handleKeyDown}
    >
      {/* Backdrop */}
      <div 
        className="absolute inset-0 bg-black/60 backdrop-blur-sm"
        onClick={onClose}
      />
      
      {/* Modal */}
      <div 
        className="relative w-full max-w-md mx-4 rounded-2xl shadow-2xl overflow-hidden"
        style={{ background: 'var(--bg-secondary)' }}
      >
        {/* Header */}
        <div 
          className="px-6 py-5"
          style={{ 
            background: 'linear-gradient(135deg, #DC2626 0%, #EF4444 50%, #F87171 100%)'
          }}
        >
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-xl bg-white/20 flex items-center justify-center">
                <AlertTriangle size={20} className="text-white" />
              </div>
              <div>
                <h2 className="text-lg font-semibold text-white">Delete Agent</h2>
                <p className="text-sm text-white/70">This action cannot be undone</p>
              </div>
            </div>
            <button
              onClick={onClose}
              className="p-2 rounded-lg hover:bg-white/10 transition-colors"
            >
              <X size={20} className="text-white" />
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="p-6">
          <p style={{ color: 'var(--text-secondary)' }}>
            Are you sure you want to delete <strong style={{ color: 'var(--text-primary)' }}>{agentName}</strong>?
          </p>
          <p className="text-sm mt-2" style={{ color: 'var(--text-muted)' }}>
            This will permanently remove the agent file from your system.
          </p>

          {/* Actions */}
          <div className="flex gap-3 mt-6">
            <button
              ref={cancelRef}
              onClick={onClose}
              className="flex-1 px-4 py-3 rounded-xl text-sm font-medium transition-colors"
              style={{ 
                background: 'var(--bg-tertiary)', 
                color: 'var(--text-secondary)' 
              }}
            >
              Cancel
            </button>
            <button
              onClick={onConfirm}
              className="flex-1 flex items-center justify-center gap-2 px-4 py-3 rounded-xl text-sm font-medium text-white transition-colors hover:opacity-90"
              style={{ 
                background: 'linear-gradient(135deg, #DC2626 0%, #EF4444 100%)'
              }}
            >
              Delete Agent
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
