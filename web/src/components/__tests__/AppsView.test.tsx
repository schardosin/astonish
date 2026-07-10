/// <reference types="vitest" />

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import AppsView from '../AppsView';
import * as appsApi from '../../api/apps';
import '@testing-library/jest-dom';

// Mock the API functions
vi.mock('../../api/apps', () => ({
  fetchApps: vi.fn(),
  fetchApp: vi.fn(),
  deleteApp: vi.fn(),
  saveApp: vi.fn(),
  patchAppModel: vi.fn(),
}));

const mockApps: appsApi.AppListItem[] = [
  {
    slug: 'test-app-1',
    name: 'Test App 1',
    description: 'A test app',
    version: 1,
    updatedAt: new Date().toISOString(),
    scope: 'personal',
    pinnedProvider: 'openai',
    pinnedModel: 'gpt-3.5-turbo',
    effectiveProvider: 'openai',
    effectiveModel: 'gpt-3.5-turbo',
  },
];

describe('AppsView', () => {
  beforeEach(() => {
    vi.mocked(appsApi.fetchApps).mockResolvedValue({ apps: mockApps });
    vi.mocked(appsApi.patchAppModel).mockResolvedValue({
      pinnedProvider: 'openai',
      pinnedModel: 'gpt-4',
      effectiveProvider: 'openai',
      effectiveModel: 'gpt-4',
    });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('should render the model picker and update the model on change', async () => {
    render(<AppsView theme="dark" />);

    // Wait for apps to load
    await waitFor(() => expect(appsApi.fetchApps).toHaveBeenCalled());

    // Find the button to open the picker
    const pickerButton = await screen.findByText('openai/gpt-3.5-turbo');
    expect(pickerButton).toBeInTheDocument();

    // Open the picker
    fireEvent.click(pickerButton);

    // Find input fields and change values
    const providerInput = await screen.findByPlaceholderText('e.g. openai');
    const modelInput = await screen.findByPlaceholderText('e.g. gpt-4-turbo');
    const saveButton = await screen.findByText('Save');

    expect(providerInput).toBeInTheDocument();
    expect(modelInput).toBeInTheDocument();

    fireEvent.change(providerInput, { target: { value: 'openai' } });
    fireEvent.change(modelInput, { target: { value: 'gpt-4' } });

    // Save the changes
    fireEvent.click(saveButton);

    // Check if patchAppModel was called
    await waitFor(() => {
      expect(appsApi.patchAppModel).toHaveBeenCalledWith('test-app-1', 'openai', 'gpt-4');
    });

    // Check if the UI updated with the new model
    await waitFor(() => {
         const newPickerButton = screen.getByText('openai/gpt-4');
         expect(newPickerButton).toBeInTheDocument();
    });
  });

  it('should show an error message if updating the model fails', async () => {
    vi.mocked(appsApi.patchAppModel).mockRejectedValue(new Error('Failed to update'));
    render(<AppsView theme="dark" />);

    // Wait for apps to load
    await waitFor(() => expect(appsApi.fetchApps).toHaveBeenCalled());

    // Open the picker
    const pickerButton = await screen.findByText('openai/gpt-3.5-turbo');
    fireEvent.click(pickerButton);
    
    // Find input fields and change values
    const providerInput = await screen.findByPlaceholderText('e.g. openai');
    fireEvent.change(providerInput, { target: { value: 'anthropic' } });

    // Save the changes
    const saveButton = await screen.findByText('Save');
    fireEvent.click(saveButton);

    // Check for error message
    await waitFor(() => {
      const errorMessage = screen.getByText('Failed to update');
      expect(errorMessage).toBeInTheDocument();
    });
  });
});
