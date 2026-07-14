import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import SetupWizard from '../SetupWizard'

vi.mock('../../../api/fleetChat', () => ({
  createSetupDraft: vi.fn().mockResolvedValue({
    draft: {
      id: 'draft-1',
      template_key: 'software-dev',
      setup_profile_key: 'software-development',
      collected: {},
    },
  }),
  fetchSetupProfile: vi.fn().mockResolvedValue({
    profile: {
      key: 'software-development',
      name: 'Software Development Setup',
      steps: [
        { id: 'overview', title: 'Overview', type: 'info' },
        { id: 'channel', title: 'Channel', type: 'form', fields: [{ id: 'type', label: 'Type', type: 'enum', maps_to: 'plan.channel.type', options: [{ value: 'chat', label: 'Chat' }] }] },
        { id: 'review', title: 'Review', type: 'review', required: true },
      ],
    },
  }),
  fetchFleet: vi.fn().mockResolvedValue({ fleet: { agents: { dev: { name: 'Dev' } } } }),
  patchSetupDraft: vi.fn().mockImplementation(async (_id, body) => ({ draft: { id: 'draft-1', template_key: 'software-dev', setup_profile_key: 'software-development', collected: body.collected || {} } })),
  validateSetupStep: vi.fn().mockResolvedValue({ status: 'ok' }),
  finalizeSetupDraft: vi.fn().mockResolvedValue({ status: 'saved', key: 'my-plan' }),
}))

describe('SetupWizard', () => {
  it('renders setup wizard after loading profile', async () => {
    render(
      <SetupWizard
        templateKey="software-dev"
        templateName="Software Dev"
        onClose={() => {}}
        onComplete={() => {}}
      />,
    )

    expect(await screen.findByText('Create Plan')).toBeInTheDocument()
    expect(await screen.findByText(/Software Development Setup/)).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByText(/Step 1 of/)).toBeInTheDocument()
    })
  })
})
