/// <reference types="vitest" />

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import AppsView from '../AppsView';
import * as appsApi from '../../api/apps';
import * as studioChatApi from '../../api/studioChat';
import '@testing-library/jest-dom';

vi.mock('../../api/apps', () => ({
  fetchApps: vi.fn(),
  fetchApp: vi.fn(),
  deleteApp: vi.fn(),
  saveApp: vi.fn(),
  patchAppModel: vi.fn(),
}));

vi.mock('../../api/studioChat', () => ({
  fetchAvailableProviders: vi.fn().mockResolvedValue(['openai', 'anthropic']),
}));

vi.mock('../ProviderModelSelector', () => ({
  default: ({ isOpen, onClose, onSelect }: { isOpen: boolean; onClose: () => void; onSelect: (id: string) => void }) => {
    if (!isOpen) return null;
    return (
      <div data-testid="model-selector-modal">
        <button onClick={() => onSelect('gpt-4')}>Select gpt-4</button>
        <button onClick={onClose}>Close modal</button>
      </div>
    );
  },
}));

const mockApps: appsApi.AppListItem[] = [
  {
    slug: 'test-app-1',
    name: 'Test App 1',
    description: 'A test app',
    version: 1,
    updatedAt: new Date().toISOString(),
    scope: 'personal',
  },
];

const mockVisualApp: appsApi.VisualApp = {
  name: 'Test App 1',
  description: 'A test app',
  code: 'export default function App() { return <div>Hi</div> }',
  version: 1,
  createdAt: new Date().toISOString(),
  updatedAt: new Date().toISOString(),
  pinnedProvider: 'openai',
  pinnedModel: 'gpt-3.5-turbo',
  effectiveProvider: 'openai',
  effectiveModel: 'gpt-3.5-turbo',
};

describe('AppsView', () => {
  beforeEach(() => {
    vi.mocked(appsApi.fetchApps).mockResolvedValue({ apps: mockApps });
    vi.mocked(appsApi.fetchApp).mockResolvedValue({ ...mockVisualApp });
    vi.mocked(appsApi.patchAppModel).mockResolvedValue({
      pinnedProvider: 'openai',
      pinnedModel: 'gpt-4',
      effectiveProvider: 'openai',
      effectiveModel: 'gpt-4',
    });
    vi.mocked(studioChatApi.fetchAvailableProviders).mockResolvedValue(['openai', 'anthropic']);
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('does not show a model picker on the apps list cards', async () => {
    render(<AppsView theme="dark" />);
    await waitFor(() => expect(appsApi.fetchApps).toHaveBeenCalled());
    expect(screen.queryByText(/Model:/)).not.toBeInTheDocument();
    expect(screen.getByText('Test App 1')).toBeInTheDocument();
  });

  it('shows the chat-style model picker in the app detail header after the title', async () => {
    render(<AppsView theme="dark" appName="test-app-1" />);
    await waitFor(() => expect(appsApi.fetchApp).toHaveBeenCalledWith('test-app-1'));

    expect(screen.getByRole('button', { name: /Back/i })).toBeInTheDocument();
    expect(screen.getByText('Test App 1')).toBeInTheDocument();
    const pickerButton = await screen.findByRole('button', { name: /Model: openai\/gpt-3\.5-turbo/ });
    expect(pickerButton).toBeInTheDocument();

    fireEvent.click(pickerButton);
    expect(screen.getByRole('combobox')).toBeInTheDocument();
    expect(screen.getByText('Currently: openai/gpt-3.5-turbo')).toBeInTheDocument();
  });

  it('saves a new app model pin from the detail header picker', async () => {
    render(<AppsView theme="dark" appName="test-app-1" />);
    await waitFor(() => expect(appsApi.fetchApp).toHaveBeenCalled());

    fireEvent.click(await screen.findByRole('button', { name: /Model: openai\/gpt-3\.5-turbo/ }));
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'openai' } });
    fireEvent.click(screen.getByTitle('Browse models'));
    fireEvent.click(screen.getByText('Select gpt-4'));
    fireEvent.click(screen.getByRole('button', { name: /save/i }));

    await waitFor(() => {
      expect(appsApi.patchAppModel).toHaveBeenCalledWith('test-app-1', 'openai', 'gpt-4');
    });
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Model: openai\/gpt-4/ })).toBeInTheDocument();
    });
  });
});
