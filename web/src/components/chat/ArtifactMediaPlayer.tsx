import { useEffect, useState } from 'react'
import { Loader } from 'lucide-react'
import { fetchArtifactBlob } from '../../api/studioChat'
import type { ArtifactMediaKind } from '../../utils/artifactMedia'

/**
 * Loads a binary artifact via teamFetch (headers) and plays it with a native
 * media element. Used by FilePanel and EmbeddedFileViewer — not a markdown report.
 */
export default function ArtifactMediaPlayer({
  path,
  fileName,
  kind,
  sessionId,
}: {
  path: string
  fileName: string
  kind: Exclude<ArtifactMediaKind, null>
  sessionId?: string | null
}) {
  const [objectUrl, setObjectUrl] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    let url: string | null = null
    setLoading(true)
    setError(null)
    setObjectUrl(null)

    fetchArtifactBlob(path, sessionId || undefined)
      .then(blob => {
        if (cancelled) return
        url = URL.createObjectURL(blob)
        setObjectUrl(url)
      })
      .catch(err => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
      if (url) URL.revokeObjectURL(url)
    }
  }, [path, sessionId])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Loader size={22} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
      </div>
    )
  }

  if (error || !objectUrl) {
    return (
      <div className="text-sm p-4 rounded-lg" style={{ color: '#f87171', background: 'rgba(248, 113, 113, 0.08)' }}>
        Failed to load media: {error || 'unknown error'}
      </div>
    )
  }

  if (kind === 'video') {
    return (
      <video
        key={objectUrl}
        src={objectUrl}
        controls
        playsInline
        className="w-full rounded-lg max-h-[70vh] bg-black"
        style={{ border: '1px solid var(--border-color)' }}
      >
        <track kind="captions" />
        Your browser cannot play this video ({fileName}).
      </video>
    )
  }

  if (kind === 'audio') {
    return (
      <div className="py-8 px-2">
        <audio key={objectUrl} src={objectUrl} controls className="w-full">
          Your browser cannot play this audio ({fileName}).
        </audio>
      </div>
    )
  }

  return (
    <img
      key={objectUrl}
      src={objectUrl}
      alt={fileName}
      className="max-w-full max-h-[70vh] rounded-lg mx-auto"
      style={{ border: '1px solid var(--border-color)' }}
    />
  )
}
