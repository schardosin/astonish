import { describe, expect, it } from 'vitest'
import { generateRunInstructions } from './generateRunInstructions'

describe('generateRunInstructions', () => {
  it('places inject_drill_credentials before start-services when credentials are declared', () => {
    const got = generateRunInstructions('juicytrade', {
      template: 'juicytrade',
      workspace: '/root/juicytrade',
      branch: 'main',
      credentials: { providers: 'juicytrade-providers' },
      credential_injection: {
        files: [{ credential: 'providers', path: '/root/juicytrade/config/providers.yaml', field: 'value' }],
      },
      setup: ['bash /root/juicytrade/.astonish/start-services.sh'],
      ready_check: { type: 'http', url: 'http://localhost:8008/health', timeout: 60 },
    })
    const injectIdx = got.indexOf('inject_drill_credentials')
    const startIdx = got.indexOf('start-services.sh')
    expect(injectIdx).toBeGreaterThanOrEqual(0)
    expect(startIdx).toBeGreaterThan(injectIdx)
    expect(got).toContain('run_drill(suite_name: "juicytrade")')
  })

  it('omits inject when no credentials are declared', () => {
    const got = generateRunInstructions('plain', {
      template: 'plain',
      setup: ['echo start'],
    })
    expect(got).not.toContain('inject_drill_credentials')
  })
})
