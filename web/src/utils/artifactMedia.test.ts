import { describe, expect, it } from 'vitest'
import { artifactMediaKind, downloadLabelForArtifact, fileTypeFromFileName } from './artifactMedia'

describe('fileTypeFromFileName', () => {
  it('maps recording extensions to Video', () => {
    expect(fileTypeFromFileName('demo.mp4')).toBe('Video')
    expect(fileTypeFromFileName('clip.webm')).toBe('Video')
  })

  it('maps markdown reports', () => {
    expect(fileTypeFromFileName('report.md')).toBe('Markdown')
  })
})

describe('artifactMediaKind', () => {
  it('classifies Video fileType and common extensions', () => {
    expect(artifactMediaKind('Video', 'demo.mp4')).toBe('video')
    expect(artifactMediaKind('File', 'clip.webm')).toBe('video')
    expect(artifactMediaKind('File', 'clip.mov')).toBe('video')
  })

  it('returns null for text/report types', () => {
    expect(artifactMediaKind('Markdown', 'report.md')).toBeNull()
    expect(artifactMediaKind('Python', 'main.py')).toBeNull()
  })
})

describe('downloadLabelForArtifact', () => {
  it('uses media-specific labels', () => {
    expect(downloadLabelForArtifact('Video', 'demo.mp4')).toBe('Download Video')
    expect(downloadLabelForArtifact('File', 'a.webm')).toBe('Download Video')
  })

  it('keeps markdown wording for reports', () => {
    expect(downloadLabelForArtifact('Markdown', 'report.md')).toBe('Download as Markdown')
  })

  it('falls back for other files', () => {
    expect(downloadLabelForArtifact('Python', 'main.py')).toBe('Download File')
  })
})
