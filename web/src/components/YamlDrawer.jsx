import { X } from 'lucide-react'
import CodeMirror from '@uiw/react-codemirror'
import { yaml } from '@codemirror/lang-yaml'

export default function YamlDrawer({ content, onChange, onClose, theme }) {
  return (
    <div className="flex flex-col h-full" style={{ background: 'var(--bg-secondary)' }}>
      {/* Header */}
      <div className="flex items-center justify-between p-4" style={{ borderBottom: '1px solid var(--border-color)' }}>
        <div>
          <h2 className="font-semibold" style={{ color: 'var(--text-primary)' }}>Source (YAML)</h2>
          <p className="text-xs" style={{ color: 'var(--text-muted)' }}>Live synchronized with flow</p>
        </div>
        <button
          onClick={onClose}
          className="p-2 rounded-lg transition-colors hover:bg-purple-500/20"
        >
          <X size={20} style={{ color: 'var(--text-muted)' }} />
        </button>
      </div>

      {/* Code Editor */}
      <div className="flex-1 overflow-auto">
        <CodeMirror
          value={content}
          onChange={onChange}
          extensions={[yaml()]}
          theme={theme === 'dark' ? 'dark' : 'light'}
          className="h-full text-sm"
          basicSetup={{
            lineNumbers: true,
            highlightActiveLineGutter: true,
            highlightActiveLine: true,
            foldGutter: true,
          }}
        />
      </div>
    </div>
  )
}
