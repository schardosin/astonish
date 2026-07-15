/**
 * Build Studio chat prep instructions for a drill suite.
 * Mirrors pkg/drill.GenerateRunInstructions — keep in sync.
 */
export function generateRunInstructions(
  suiteName: string,
  suiteConfig?: Record<string, unknown> | null
): string {
  const name = (suiteName || '<suite>').trim() || '<suite>'
  const sc = suiteConfig || {}

  const override = typeof sc.run_instructions === 'string' ? sc.run_instructions.trim() : ''
  if (override) {
    if (!override.includes('run_drill')) {
      return `${override}\n\nThen call run_drill(suite_name: "${name}"). Do not write credential files manually — run_drill injects them.`
    }
    return override
  }

  const lines: string[] = [
    `Prepare then run the drill suite "${name}".`,
    '',
    'Do these prep steps with tools before calling run_drill. run_drill only injects credentials and executes tests — it does not switch templates, git-pull, or start services.',
    '',
  ]

  let step = 1
  const template = String(sc.template || '').trim().replace(/^@/, '')
  if (template) {
    lines.push(
      `${step}. If the current sandbox is not already on template "${template}", call use_sandbox_template(template_name: "${template}").`
    )
    step++
  }

  const workspace = String(sc.workspace || '').trim()
  const branch = String(sc.branch || '').trim() || 'main'
  if (workspace) {
    lines.push(`${step}. Sync the workspace to the latest ${branch} (do not force-reset dirty trees):`)
    lines.push(
      `   shell_command: \`cd ${workspace} && git fetch && git checkout ${branch} && git pull --ff-only\``
    )
    lines.push('   If the pull fails (dirty or diverged), stop and report — do not force-reset.')
    step++
  }

  const configure = Array.isArray(sc.configure) ? (sc.configure as string[]) : []
  for (const cmd of configure) {
    const c = String(cmd || '').trim()
    if (!c) continue
    lines.push(`${step}. Configure: shell_command \`${c}\``)
    step++
  }

  const setup = Array.isArray(sc.setup) ? (sc.setup as string[]) : []
  for (const cmd of setup) {
    const c = String(cmd || '').trim()
    if (!c) continue
    lines.push(`${step}. Start services: shell_command \`${c}\``)
    step++
  }

  const services = Array.isArray(sc.services) ? (sc.services as Array<Record<string, unknown>>) : []
  for (const svc of services) {
    const setupCmd = String(svc?.setup || '').trim()
    if (!setupCmd) continue
    const svcName = String(svc?.name || 'service').trim() || 'service'
    lines.push(`${step}. Start ${svcName}: shell_command \`${setupCmd}\``)
    step++
  }

  const ready = sc.ready_check as Record<string, unknown> | undefined
  const readyHint = readyCheckHint(ready)
  if (readyHint) {
    lines.push(`${step}. Wait until ready: ${readyHint}`)
    step++
  }
  for (const svc of services) {
    const hint = readyCheckHint(svc?.ready_check as Record<string, unknown> | undefined)
    if (!hint) continue
    const svcName = String(svc?.name || 'service').trim() || 'service'
    lines.push(`${step}. Wait until ${svcName} is ready: ${hint}`)
    step++
  }

  lines.push(
    `${step}. Call run_drill(suite_name: "${name}"). Do not write credential files manually — run_drill injects them.`
  )
  return lines.join('\n')
}

function readyCheckHint(rc?: Record<string, unknown>): string {
  if (!rc) return ''
  const type = String(rc.type || '').toLowerCase()
  const timeout = Number(rc.timeout) > 0 ? Number(rc.timeout) : 60
  const interval = Number(rc.interval) > 0 ? Number(rc.interval) : 2
  if (type === 'http') {
    const url = String(rc.url || '').trim()
    if (!url) return ''
    const loops = Math.max(1, Math.floor(timeout / Math.max(1, interval)))
    return `poll with curl until HTTP success (timeout ~${timeout}s, every ${interval}s): \`for i in $(seq 1 ${loops}); do curl -sf ${JSON.stringify(url)} >/dev/null && exit 0; sleep ${interval}; done; exit 1\``
  }
  if (type === 'port') {
    const host = String(rc.host || '').trim() || '127.0.0.1'
    const port = Number(rc.port)
    if (!(port > 0)) return ''
    return `wait until TCP ${host}:${port} accepts connections (timeout ~${timeout}s)`
  }
  if (type === 'output_contains') {
    const pat = String(rc.pattern || '').trim()
    if (!pat) return ''
    return `confirm setup output contains "${pat}"`
  }
  return ''
}
