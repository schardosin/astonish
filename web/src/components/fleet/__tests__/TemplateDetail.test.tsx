import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import TemplateDetail from '../TemplateDetail'

const fetchFleet = vi.fn()
const saveFleet = vi.fn()
const cloneFleet = vi.fn()

vi.mock('../../../api/fleetChat', () => ({
  fetchFleet: (...args: unknown[]) => fetchFleet(...args),
  saveFleet: (...args: unknown[]) => saveFleet(...args),
  cloneFleet: (...args: unknown[]) => cloneFleet(...args),
}))

const fleetPayload = {
  name: 'Software Dev',
  description: 'Build software',
  setup_profile: 'software-development',
  settings: { max_turns_per_agent: 10, max_parallel_agents: 2 },
  agents: {
    dev: {
      name: 'Developer',
      identity: 'Writes code',
      behaviors: 'Implement changes',
      capabilities: { 'code.write': true },
      execution: { parallelizable: true, workspace: 'shared' },
    },
  },
  communication: { flow: [{ role: 'dev', entry_point: true, talks_to: [] }] },
}

describe('TemplateDetail', () => {
  it('shows linked setup profile on template overview', async () => {
    fetchFleet.mockResolvedValue({ key: 'software-dev', fleet: fleetPayload, source: 'bundled' })
    const onNavigateToSetupProfile = vi.fn()
    window.location.hash = '#/fleet/template/software-dev'

    render(
      <TemplateDetail
        templateKey="software-dev"
        templates={[{
          key: 'software-dev',
          name: 'Software Dev',
          description: 'Build software',
          agent_count: 1,
          agent_names: ['dev'],
          source: 'bundled',
        }]}
        setupProfiles={[{
          key: 'software-development',
          name: 'Software Development Setup',
          description: 'Full SDLC setup flow',
          step_count: 9,
          source: 'bundled',
        }]}
        onNavigateToSetupProfile={onNavigateToSetupProfile}
      />,
    )

    expect(await screen.findByText('Setup Profile')).toBeInTheDocument()
    expect(screen.getByText('Software Development Setup')).toBeInTheDocument()
    expect(screen.getByText('software-development')).toBeInTheDocument()
    expect(screen.getByText(/Setup: Software Development Setup/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /View profile/i }))
    expect(onNavigateToSetupProfile).toHaveBeenCalledWith('software-development')
  })

  it('renders bundled templates as read-only with clone CTA', async () => {
    fetchFleet.mockResolvedValue({ key: 'software-dev', fleet: fleetPayload, source: 'bundled' })
    window.location.hash = '#/fleet/template/software-dev'

    render(
      <TemplateDetail
        templateKey="software-dev"
        templates={[{
          key: 'software-dev',
          name: 'Software Dev',
          description: 'Build software',
          agent_count: 1,
          agent_names: ['dev'],
          source: 'bundled',
        }]}
      />,
    )

    expect(await screen.findByText('Astonish template')).toBeInTheDocument()
    expect(screen.getByText(/cannot be edited/i)).toBeInTheDocument()
    expect(screen.getByText('Clone to edit')).toBeInTheDocument()

    fireEvent.click(screen.getByText('settings'))
    expect(await screen.findByText(/read-only/i)).toBeInTheDocument()
    expect(screen.queryByText('Save Settings')).not.toBeInTheDocument()
  })

  it('opens clone dialog and submits name and key separately', async () => {
    fetchFleet.mockResolvedValue({ key: 'software-dev', fleet: fleetPayload, source: 'bundled' })
    cloneFleet.mockResolvedValue({ status: 'ok', key: 'acme-dev', source: 'custom' })
    const onCloned = vi.fn()
    window.location.hash = '#/fleet/template/software-dev'

    render(
      <TemplateDetail
        templateKey="software-dev"
        templates={[{
          key: 'software-dev',
          name: 'Software Dev',
          description: 'Build software',
          agent_count: 1,
          agent_names: ['dev'],
          source: 'bundled',
        }]}
        onCloned={onCloned}
      />,
    )

    expect(await screen.findByText('key: software-dev')).toBeInTheDocument()
    fireEvent.click(screen.getByText('Clone to edit'))

    expect(await screen.findByRole('heading', { name: 'Clone Template' })).toBeInTheDocument()
    const nameInput = screen.getByPlaceholderText(/Software Dev — Acme/i)
    const keyInput = screen.getByPlaceholderText(/software-dev-acme/i)
    fireEvent.change(nameInput, { target: { value: 'Acme Software Dev' } })
    fireEvent.change(keyInput, { target: { value: 'acme-dev' } })
    fireEvent.click(screen.getByRole('button', { name: 'Clone Template' }))

    await waitFor(() => {
      expect(cloneFleet).toHaveBeenCalledWith('software-dev', 'acme-dev', 'Acme Software Dev')
    })
    expect(onCloned).toHaveBeenCalledWith('acme-dev')
  })

  it('adds and deletes agents on custom templates', async () => {
    const customFleet = {
      ...fleetPayload,
      name: 'My Fleet',
      agents: {
        ...fleetPayload.agents,
        po: {
          name: 'PO',
          identity: 'Owns the backlog',
          behaviors: 'Prioritize work',
          tools: true,
        },
      },
      communication: {
        flow: [
          { role: 'po', entry_point: true, talks_to: ['dev', 'customer'] },
          { role: 'dev', talks_to: ['po', 'customer'] },
        ],
      },
    }
    fetchFleet.mockResolvedValue({ key: 'my-fleet', fleet: customFleet, source: 'custom' })
    saveFleet.mockResolvedValue({ status: 'ok', key: 'my-fleet' })
    window.location.hash = '#/fleet/template/my-fleet/agents'
    window.confirm = vi.fn(() => true)

    render(
      <TemplateDetail
        templateKey="my-fleet"
        templates={[{
          key: 'my-fleet',
          name: 'My Fleet',
          description: 'Custom',
          agent_count: 2,
          agent_names: ['po', 'dev'],
          source: 'custom',
        }]}
      />,
    )

    expect(await screen.findByText('Add Agent')).toBeInTheDocument()
    fireEvent.click(screen.getByText('Add Agent'))
    fireEvent.change(screen.getByPlaceholderText(/Product Owner/i), { target: { value: 'QA' } })
    fireEvent.change(screen.getByPlaceholderText(/^e\.g\. po$/i), { target: { value: 'qa' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create Agent' }))

    await waitFor(() => {
      expect(saveFleet).toHaveBeenCalled()
      const calls = saveFleet.mock.calls
      const saved = calls[calls.length - 1]?.[1] as { agents: Record<string, unknown> }
      expect(saved.agents.qa).toBeTruthy()
    })

    const deleteButtons = screen.getAllByTitle(/Delete @/)
    fireEvent.click(deleteButtons[0])

    await waitFor(() => {
      const calls = saveFleet.mock.calls
      const saved = calls[calls.length - 1]?.[1] as { agents: Record<string, unknown> }
      expect(Object.keys(saved.agents).length).toBeLessThan(3)
    })
  })

  it('shows identity fields first and keeps capabilities on the Advanced tab', async () => {
    const minimalFleet = {
      ...fleetPayload,
      agents: {
        agent: {
          name: 'Agent',
          identity: 'You are an agent.',
          behaviors: 'Follow instructions.',
          tools: true,
        },
      },
      communication: { flow: [{ role: 'agent', entry_point: true, talks_to: ['customer'] }] },
    }
    fetchFleet.mockResolvedValue({ key: 'my-fleet', fleet: minimalFleet, source: 'custom' })
    window.location.hash = '#/fleet/template/my-fleet/agents'

    render(
      <TemplateDetail
        templateKey="my-fleet"
        templates={[{
          key: 'my-fleet',
          name: 'My Fleet',
          description: 'Custom',
          agent_count: 1,
          agent_names: ['agent'],
          source: 'custom',
        }]}
      />,
    )

    expect(await screen.findByText('Agents (1)')).toBeInTheDocument()
    fireEvent.click(screen.getByText('agent', { exact: true }))
    expect(await screen.findByLabelText('Identity')).toBeInTheDocument()
    expect(screen.queryByLabelText('Capabilities')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Advanced' }))
    expect(await screen.findByLabelText('Capabilities')).toBeInTheDocument()
    expect(await screen.findByLabelText('Task claims')).toBeInTheDocument()
  })

  it('shows agent capabilities when opening the Advanced tab', async () => {
    fetchFleet.mockResolvedValue({ key: 'my-fleet', fleet: fleetPayload, source: 'custom' })
    window.location.hash = '#/fleet/template/my-fleet/agents'

    render(
      <TemplateDetail
        templateKey="my-fleet"
        templates={[{
          key: 'my-fleet',
          name: 'My Fleet',
          description: 'Custom',
          agent_count: 1,
          agent_names: ['dev'],
          source: 'custom',
        }]}
      />,
    )

    expect(await screen.findByText('Agents (1)')).toBeInTheDocument()
    fireEvent.click(screen.getByText('dev', { exact: true }))
    expect(await screen.findByRole('heading', { name: 'Edit @dev' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Advanced' }))
    const caps = await screen.findByLabelText('Capabilities')
    expect(caps).toHaveValue('code.write')
  })

  it('saves custom template settings through the fleet save API', async () => {
    fetchFleet.mockResolvedValue({ key: 'my-fleet', fleet: { ...fleetPayload, name: 'My Fleet' }, source: 'custom' })
    saveFleet.mockResolvedValue({ status: 'ok', key: 'my-fleet' })
    window.location.hash = '#/fleet/template/my-fleet/settings'

    render(
      <TemplateDetail
        templateKey="my-fleet"
        templates={[{
          key: 'my-fleet',
          name: 'My Fleet',
          description: 'Custom',
          agent_count: 1,
          agent_names: ['dev'],
          source: 'custom',
        }]}
        setupProfiles={[
          { key: 'software-development', name: 'Software Development Setup', step_count: 9, source: 'bundled' },
          { key: 'generic', name: 'Generic Setup', step_count: 5, source: 'bundled' },
        ]}
      />,
    )

    const input = await screen.findByLabelText('Max turns per agent')
    fireEvent.change(input, { target: { value: '12' } })
    fireEvent.click(screen.getByText('Save Settings'))

    await waitFor(() => expect(saveFleet).toHaveBeenCalled())
  })

  it('saves setup profile selection from settings tab', async () => {
    fetchFleet.mockResolvedValue({
      key: 'my-fleet',
      fleet: { ...fleetPayload, name: 'My Fleet', setup_profile: 'generic' },
      source: 'custom',
    })
    saveFleet.mockResolvedValue({ status: 'ok', key: 'my-fleet' })
    window.location.hash = '#/fleet/template/my-fleet/settings'

    render(
      <TemplateDetail
        templateKey="my-fleet"
        templates={[{
          key: 'my-fleet',
          name: 'My Fleet',
          description: 'Custom',
          agent_count: 1,
          agent_names: ['dev'],
          source: 'custom',
        }]}
        setupProfiles={[
          { key: 'software-development', name: 'Software Development Setup', step_count: 9, source: 'bundled' },
          { key: 'generic', name: 'Generic Setup', step_count: 5, source: 'bundled' },
          { key: 'acme-setup', name: 'Acme Setup', step_count: 6, source: 'custom' },
        ]}
      />,
    )

    const select = await screen.findByLabelText('Setup profile')
    fireEvent.change(select, { target: { value: 'software-development' } })
    fireEvent.click(screen.getByText('Save Settings'))

    await waitFor(() => {
      expect(saveFleet).toHaveBeenCalled()
      const calls = saveFleet.mock.calls
      const saved = calls[calls.length - 1]?.[1] as { setup_profile?: string }
      expect(saved.setup_profile).toBe('software-development')
    })
  })
})
