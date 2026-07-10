
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import UserDefaultModelSettings from '../UserDefaultModelSettings';
import * as userSettingsApi from '../../../api/userSettings';
import * as settingsApi from '../settingsApi';

vi.mock('../../ProviderModelSelector', () => ({
  default: ({ isOpen, onSelect, currentModel }: { isOpen: boolean, onSelect: (model: string) => void, currentModel: string }) => {
    if (!isOpen) return null;
    return (
      <div data-testid="mock-model-selector">
        <p>Current: {currentModel}</p>
        <button onClick={() => onSelect('selected-model-from-modal')}>Select Model</button>
      </div>
    );
  }
}));


describe('UserDefaultModelSettings', () => {
  beforeEach(() => {
    vi.spyOn(userSettingsApi, 'fetchUserDefaultModel').mockResolvedValue({
      defaultProvider: 'test-provider',
      defaultModel: 'test-model',
    });
    vi.spyOn(settingsApi, 'fetchSettings').mockResolvedValue({
        general: {
            default_provider: 'team-default-provider',
            default_model: 'team-default-model',
            web_search_tool: '',
            web_extract_tool: '',
            timezone: ''
        },
        providers: [
            { name: 'test-provider', type: 'basic-type', display_name: 'Test Provider', configured: true, fields: {}},
            { name: 'another-provider', type: 'anthropic', display_name: 'Another Provider', configured: true, fields: {}},
            { name: 'openrouter-provider', type: 'openrouter', display_name: 'Open Router', configured: true, fields: {}},
        ]
    });
    vi.spyOn(userSettingsApi, 'patchUserDefaultModel').mockResolvedValue(undefined);
    vi.spyOn(settingsApi, 'fetchProviderModels').mockResolvedValue({ models: ['model-1', 'model-2'] });
  });

  it('renders with initial data loaded from APIs', async () => {
    render(<UserDefaultModelSettings />);
    await waitFor(() => {
      const providerSelect = screen.getByLabelText('Default Provider') as HTMLSelectElement;
      expect(providerSelect.value).toBe('test-provider');
    });
  });

  it('shows inheritance info when no user default is set', async () => {
    vi.spyOn(userSettingsApi, 'fetchUserDefaultModel').mockResolvedValue({
        defaultProvider: '',
        defaultModel: '',
      });
    render(<UserDefaultModelSettings />);
    await waitFor(() => {
        expect(screen.getByText(/Inheriting from Team:/)).toBeInTheDocument();
        expect(screen.getByText('team-default-provider')).toBeInTheDocument();
        expect(screen.getByText('team-default-model')).toBeInTheDocument();
    });
  });

  it('calls patchUserDefaultModel with current form values on save', async () => {
    render(<UserDefaultModelSettings />);
    await waitFor(() => {
      expect((screen.getByLabelText('Default Provider') as HTMLSelectElement).value).toBe('test-provider');
    });
    
    fireEvent.click(screen.getByText('Save Default'));

    await waitFor(() => {
      expect(userSettingsApi.patchUserDefaultModel).toHaveBeenCalledWith('test-provider', 'test-model');
    });
  });

  it('clears the form and calls patchUserDefaultModel on clear', async () => {
    render(<UserDefaultModelSettings />);
    await waitFor(() => {
        expect((screen.getByLabelText('Default Provider') as HTMLSelectElement).value).toBe('test-provider');
    });

    fireEvent.click(screen.getByText('Clear'));

    await waitFor(() => {
      expect(userSettingsApi.patchUserDefaultModel).toHaveBeenCalledWith('', '');
    });
  });

  it('opens the enhanced model selector for supported provider types', async () => {
    vi.spyOn(userSettingsApi, 'fetchUserDefaultModel').mockResolvedValue({
        defaultProvider: 'openrouter-provider',
        defaultModel: '',
      });
    render(<UserDefaultModelSettings />);

    await waitFor(() => {
        const modelSelectorButton = screen.getByRole('button', { name: /Click to select a model/i });
        fireEvent.click(modelSelectorButton);
    });

    await waitFor(() => {
        expect(screen.getByTestId('mock-model-selector')).toBeInTheDocument();
    });
  });

  it('loads models for basic select on focus', async () => {
    vi.spyOn(userSettingsApi, 'fetchUserDefaultModel').mockResolvedValue({
        defaultProvider: 'test-provider',
        defaultModel: '',
      });
    render(<UserDefaultModelSettings />);

    let modelSelect: HTMLElement;
    await waitFor(() => {
        modelSelect = screen.getByLabelText('Default Model');
        fireEvent.focus(modelSelect);
    });

    await waitFor(() => {
        expect(settingsApi.fetchProviderModels).toHaveBeenCalledWith('test-provider');
        expect(screen.getByText('model-1')).toBeInTheDocument();
        expect(screen.getByText('model-2')).toBeInTheDocument();
    });
  });
});
