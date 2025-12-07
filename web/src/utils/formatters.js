/**
 * Convert snake_case to Title Case
 * e.g., "github_pr_generator" -> "Github Pr Generator"
 */
export function snakeToTitleCase(str) {
  if (!str) return ''
  return str
    .split('_')
    .map(word => word.charAt(0).toUpperCase() + word.slice(1).toLowerCase())
    .join(' ')
}
