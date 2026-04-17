import { FileText, Download, Edit3, FilePlus, ExternalLink } from 'lucide-react'
import type { ArtifactMessage } from './chatTypes'
import { getArtifactDownloadUrl } from '../../api/studioChat'

// Extracts filename from an absolute path
function getFileName(path: string): string {
  const parts = path.split('/')
  return parts[parts.length - 1] || path
}

// Gets a human-readable file extension label
function getFileType(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase()
  const typeMap: Record<string, string> = {
    md: 'Markdown', txt: 'Text', json: 'JSON', yaml: 'YAML', yml: 'YAML',
    py: 'Python', go: 'Go', js: 'JavaScript', ts: 'TypeScript', tsx: 'TSX', jsx: 'JSX',
    html: 'HTML', css: 'CSS', sh: 'Shell', bash: 'Shell',
    csv: 'CSV', xml: 'XML', sql: 'SQL', toml: 'TOML',
    rs: 'Rust', rb: 'Ruby', java: 'Java', c: 'C', cpp: 'C++', h: 'Header',
    dockerfile: 'Dockerfile', makefile: 'Makefile',
  }
  return typeMap[ext || ''] || (ext ? ext.toUpperCase() : 'File')
}

interface ArtifactCardProps {
  data: ArtifactMessage
  sessionId?: string | null
  onOpenInPanel?: (path: string) => void
}

// Inline artifact card showing a file that was created/modified by a tool.
// Displays the filename, path, and buttons to open in the Files panel or download.
export default function ArtifactCard({ data, sessionId, onOpenInPanel }: ArtifactCardProps) {
  const fileName = getFileName(data.path)
  const fileType = getFileType(data.path)
  const isEdit = data.toolName === 'edit_file'

  const handleDownload = () => {
    const url = getArtifactDownloadUrl(data.path, sessionId || undefined)
    window.open(url, '_blank')
  }

  return (
    <div
      className="my-1.5 rounded-lg overflow-hidden inline-flex items-center gap-3 px-3 py-2 max-w-md"
      style={{
        border: '1px solid rgba(34, 197, 94, 0.3)',
        background: 'rgba(34, 197, 94, 0.05)',
      }}
    >
      <div className="flex items-center justify-center w-8 h-8 rounded bg-green-500/15">
        {isEdit ? (
          <Edit3 size={16} className="text-green-400" />
        ) : (
          <FilePlus size={16} className="text-green-400" />
        )}
      </div>
      <div className="flex flex-col min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <FileText size={12} className="text-green-400 flex-shrink-0" />
          <span className="text-xs font-medium text-gray-200 truncate">{fileName}</span>
          <span className="text-[10px] text-gray-500 flex-shrink-0">{fileType}</span>
        </div>
        <span className="text-[10px] text-gray-500 truncate" title={data.path}>{data.path}</span>
      </div>
      {onOpenInPanel && (
        <button
          onClick={() => onOpenInPanel(data.path)}
          className="flex items-center justify-center w-7 h-7 rounded hover:bg-green-500/15 transition-colors cursor-pointer flex-shrink-0"
          title="Open in Files panel"
        >
          <ExternalLink size={14} className="text-green-400" />
        </button>
      )}
      <button
        onClick={handleDownload}
        className="flex items-center justify-center w-7 h-7 rounded hover:bg-green-500/15 transition-colors cursor-pointer flex-shrink-0"
        title="Download file"
      >
        <Download size={14} className="text-green-400" />
      </button>
    </div>
  )
}
