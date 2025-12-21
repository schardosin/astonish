import { useState } from 'react'
import { X, Maximize2, Minimize2 } from 'lucide-react'
import CodeMirror from '@uiw/react-codemirror'
import { yaml } from '@codemirror/lang-yaml'
import { search, searchKeymap, highlightSelectionMatches } from '@codemirror/search'
import { keymap, EditorView } from '@codemirror/view'

export default function YamlDrawer({ content, onChange, onClose, theme }) {
  const [isFullscreen, setIsFullscreen] = useState(false)

  return (
    <div 
      className={`flex flex-col ${isFullscreen ? 'fixed inset-0 z-50' : 'h-full'}`}
      style={{ background: 'var(--bg-secondary)' }}
    >
      {/* Header */}
      <div className="flex items-center justify-between p-4" style={{ borderBottom: '1px solid var(--border-color)' }}>
        <div>
          <h2 className="font-semibold" style={{ color: 'var(--text-primary)' }}>Source (YAML)</h2>
          <p className="text-xs" style={{ color: 'var(--text-muted)' }}>Live synchronized with flow</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setIsFullscreen(!isFullscreen)}
            className="p-2 rounded-lg transition-colors hover:bg-purple-500/20"
            title={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'}
          >
            {isFullscreen ? (
              <Minimize2 size={18} style={{ color: 'var(--text-muted)' }} />
            ) : (
              <Maximize2 size={18} style={{ color: 'var(--text-muted)' }} />
            )}
          </button>
          <button
            onClick={onClose}
            className="p-2 rounded-lg transition-colors hover:bg-purple-500/20"
          >
            <X size={20} style={{ color: 'var(--text-muted)' }} />
          </button>
        </div>
      </div>

      {/* Code Editor */}
      <div className="flex-1 overflow-hidden">
        <CodeMirror
          value={content}
          onChange={onChange}
          height="100%"
          extensions={[
            yaml(),
            search({
              scrollToMatch: (range) => EditorView.scrollIntoView(range, { y: 'center', yMargin: 100 })
            }),
            highlightSelectionMatches(),
            keymap.of(searchKeymap),
          ]}
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
