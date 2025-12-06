import { X } from 'lucide-react'
import CodeMirror from '@uiw/react-codemirror'
import { yaml } from '@codemirror/lang-yaml'

export default function YamlDrawer({ content, onChange, onClose }) {
  return (
    <div className="flex flex-col h-full bg-white">
      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b border-gray-200">
        <div>
          <h2 className="font-semibold text-gray-800">Source (YAML)</h2>
          <p className="text-xs text-gray-500">Live synchronized with flow</p>
        </div>
        <button
          onClick={onClose}
          className="p-2 hover:bg-gray-100 rounded-lg transition-colors"
        >
          <X size={20} className="text-gray-500" />
        </button>
      </div>

      {/* Code Editor */}
      <div className="flex-1 overflow-auto">
        <CodeMirror
          value={content}
          onChange={onChange}
          extensions={[yaml()]}
          theme="light"
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
