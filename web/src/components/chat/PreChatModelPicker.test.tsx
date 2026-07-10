import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import PreChatModelPicker from './PreChatModelPicker';

// Mock ProviderModelSelector since it does network calls
vi.mock('../ProviderModelSelector', () => ({
  default: ({ isOpen, onClose, onSelect, currentModel }: { isOpen: boolean; onClose: () => void; onSelect: (id: string) => void; currentModel?: string }) => {
    if (!isOpen) return null;
    return (
      <div data-testid="model-selector-modal">
        <span data-testid="current-model">{currentModel}</span>
        <button onClick={() => onSelect('claude-4')}>Select claude-4</button>
        <button onClick={onClose}>Close modal</button>
      </div>
    );
  },
}));

function renderPicker(overrides?: {
  availableProviders?: string[];
  provider?: string;
  model?: string;
}) {
  const onChange = vi.fn();
  render(
    <PreChatModelPicker
      availableProviders={overrides?.availableProviders ?? ['openai', 'anthropic', 'google']}
      provider={overrides?.provider ?? ''}
      model={overrides?.model ?? ''}
      onChange={onChange}
    />
  );
  return { onChange };
}

describe('PreChatModelPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders with default label when provider/model empty', () => {
    renderPicker();
    expect(screen.getByText(/Model:/)).toBeInTheDocument();
    expect(screen.getByText(/default/)).toBeInTheDocument();
  });

  it('renders with provider/model label when provided', () => {
    renderPicker({ provider: 'openai', model: 'gpt-4o' });
    expect(screen.getByRole('button', { name: /Model: openai\/gpt-4o/ })).toBeInTheDocument();
  });

  it('opens dropdown on button click', () => {
    renderPicker();
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /Model:/ }));
    expect(screen.getByRole('combobox')).toBeInTheDocument();
  });

  it('calls onChange with selected provider+model on Apply', () => {
    const { onChange } = renderPicker();
    fireEvent.click(screen.getByRole('button', { name: /Model:/ }));
    // Select provider
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'anthropic' } });
    // Open model selector and pick a model
    fireEvent.click(screen.getByText(/Click to browse models/));
    fireEvent.click(screen.getByText('Select claude-4'));
    // Apply
    fireEvent.click(screen.getByRole('button', { name: /apply/i }));
    expect(onChange).toHaveBeenCalledWith('anthropic', 'claude-4');
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it('calls onChange with empty strings on Reset', () => {
    const { onChange } = renderPicker({ provider: 'openai', model: 'gpt-4o' });
    fireEvent.click(screen.getByRole('button', { name: /Model:/ }));
    fireEvent.click(screen.getByRole('button', { name: /reset/i }));
    expect(onChange).toHaveBeenCalledWith('', '');
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it('closes dropdown after Apply', () => {
    renderPicker();
    fireEvent.click(screen.getByRole('button', { name: /Model:/ }));
    expect(screen.getByRole('combobox')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /apply/i }));
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
  });

  it('closes dropdown on click outside', () => {
    renderPicker();
    fireEvent.click(screen.getByRole('button', { name: /Model:/ }));
    expect(screen.getByRole('combobox')).toBeInTheDocument();
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
  });

  it('populates select from availableProviders prop', () => {
    renderPicker({ availableProviders: ['openai', 'anthropic'] });
    fireEvent.click(screen.getByRole('button', { name: /Model:/ }));
    const select = screen.getByRole('combobox') as HTMLSelectElement;
    const values = Array.from(select.options).map((o) => o.value);
    expect(values).toContain('');
    expect(values).toContain('openai');
    expect(values).toContain('anthropic');
    expect(values).not.toContain('google');
  });

  it('syncs local state when props change', () => {
    const onChange = vi.fn();
    const { rerender } = render(
      <PreChatModelPicker
        availableProviders={['openai', 'anthropic']}
        provider=""
        model=""
        onChange={onChange}
      />
    );
    fireEvent.click(screen.getByRole('button', { name: /Model:/ }));
    // Prop update from parent should refresh local state
    rerender(
      <PreChatModelPicker
        availableProviders={['openai', 'anthropic']}
        provider="openai"
        model="gpt-4o"
        onChange={onChange}
      />
    );
    const select = screen.getByRole('combobox') as HTMLSelectElement;
    expect(select.value).toBe('openai');
    // Model is displayed in the browse button text
    expect(screen.getByText('gpt-4o')).toBeInTheDocument();
  });

  it('does not call onChange without user action', () => {
    const { onChange } = renderPicker({ provider: 'openai', model: 'gpt-4o' });
    fireEvent.click(screen.getByRole('button', { name: /Model:/ }));
    // Opening modal selector and selecting does not auto-fire onChange (must click Apply)
    fireEvent.click(screen.getByTitle('Browse models'));
    fireEvent.click(screen.getByText('Select claude-4'));
    expect(onChange).not.toHaveBeenCalled();
  });

  it('disables model browse button when no provider selected', () => {
    renderPicker();
    fireEvent.click(screen.getByRole('button', { name: /Model:/ }));
    const browseBtn = screen.getByTitle('Select a provider first');
    expect(browseBtn).toBeDisabled();
  });

  it('clears model when provider changes', () => {
    const { onChange } = renderPicker({ provider: 'openai', model: 'gpt-4o' });
    fireEvent.click(screen.getByRole('button', { name: /Model:/ }));
    // Change provider — model should clear
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'anthropic' } });
    // Apply
    fireEvent.click(screen.getByRole('button', { name: /apply/i }));
    expect(onChange).toHaveBeenCalledWith('anthropic', '');
  });
});
