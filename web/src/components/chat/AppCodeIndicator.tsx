import FenceIndicator from './FenceIndicator'

interface AppCodeIndicatorProps {
  /** Whether the code is still being streamed */
  streaming: boolean
  /** The raw code content */
  code: string
  /** Whether the code panel is expanded (controlled by parent) */
  expanded: boolean
  /** Toggle expand/collapse (controlled by parent) */
  onToggle: () => void
}

/**
 * Backward-compatible wrapper around FenceIndicator for astonish-app fences.
 */
export default function AppCodeIndicator(props: AppCodeIndicatorProps) {
  return <FenceIndicator {...props} fenceType="app" />
}
