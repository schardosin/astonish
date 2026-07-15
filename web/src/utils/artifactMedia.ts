/** Kind of binary media an artifact can play inline (null = text/code viewer). */
export type ArtifactMediaKind = 'video' | 'audio' | 'image' | null

const VIDEO_EXTS = new Set(['mp4', 'webm', 'mov'])
const AUDIO_EXTS = new Set(['mp3', 'wav', 'ogg', 'm4a', 'aac'])
const IMAGE_EXTS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg'])

function extOf(fileName?: string): string {
  if (!fileName) return ''
  const i = fileName.lastIndexOf('.')
  return i >= 0 ? fileName.slice(i + 1).toLowerCase() : ''
}

/**
 * Human-readable fileType for an artifact path (mirrors pkg/api fileTypeFromExt
 * for the extensions Studio cares about at SSE ingest time).
 */
export function fileTypeFromFileName(fileName: string): string {
  const ext = extOf(fileName)
  const map: Record<string, string> = {
    md: 'Markdown',
    markdown: 'Markdown',
    py: 'Python',
    go: 'Go',
    js: 'JavaScript',
    ts: 'TypeScript',
    tsx: 'TypeScript JSX',
    jsx: 'JSX',
    json: 'JSON',
    yaml: 'YAML',
    yml: 'YAML',
    html: 'HTML',
    htm: 'HTML',
    css: 'CSS',
    sh: 'Shell',
    bash: 'Shell',
    mp4: 'Video',
    webm: 'Video',
    mov: 'Video',
  }
  if (map[ext]) return map[ext]
  return ext || 'File'
}

/** Classify an artifact for inline media playback. */
export function artifactMediaKind(fileType: string, fileName?: string): ArtifactMediaKind {
  if (fileType === 'Video') return 'video'
  if (fileType === 'Audio') return 'audio'
  if (fileType === 'Image') return 'image'
  const ext = extOf(fileName)
  if (VIDEO_EXTS.has(ext)) return 'video'
  if (AUDIO_EXTS.has(ext)) return 'audio'
  if (IMAGE_EXTS.has(ext)) return 'image'
  return null
}

/** Label for the primary raw-file download action in the artifact viewer. */
export function downloadLabelForArtifact(fileType: string, fileName?: string): string {
  switch (artifactMediaKind(fileType, fileName)) {
    case 'video':
      return 'Download Video'
    case 'audio':
      return 'Download Audio'
    case 'image':
      return 'Download Image'
    default:
      break
  }
  if (fileType === 'Markdown') return 'Download as Markdown'
  return 'Download File'
}
