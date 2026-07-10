import { render, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import ModelCredentialBanner from './ModelCredentialBanner';

describe('ModelCredentialBanner', () => {
  it('should not render when pinnedModel is not set', () => {
    const { container } = render(
      <ModelCredentialBanner
        pinnedProvider=""
        pinnedModel=""
        effectiveProvider="openai"
        effectiveModel="gpt-4"
        onPickAnother={() => {}}
      />
    );
    expect(container.firstChild).toBeNull();
  });

  it('should not render when pinnedModel is the same as effectiveModel', () => {
    const { container } = render(
      <ModelCredentialBanner
        pinnedProvider="openai"
        pinnedModel="gpt-4"
        effectiveProvider="openai"
        effectiveModel="gpt-4"
        onPickAnother={() => {}}
      />
    );
    expect(container.firstChild).toBeNull();
  });

  it('should render when pinnedModel is different from effectiveModel', () => {
    const { getByText } = render(
      <ModelCredentialBanner
        pinnedProvider="openai"
        pinnedModel="gpt-4-turbo"
        effectiveProvider="openai"
        effectiveModel="gpt-4"
        onPickAnother={() => {}}
      />
    );
    expect(getByText(/Model 'gpt-4-turbo' unavailable — using 'gpt-4' instead./)).toBeInTheDocument();
  });

  it('should call onPickAnother when the button is clicked', () => {
    const handlePickAnother = vi.fn();
    const { getByText } = render(
      <ModelCredentialBanner
        pinnedProvider="openai"
        pinnedModel="gpt-4-turbo"
        effectiveProvider="openai"
        effectiveModel="gpt-4"
        onPickAnother={handlePickAnother}
      />
    );
    fireEvent.click(getByText('Pick another model.'));
    expect(handlePickAnother).toHaveBeenCalledTimes(1);
  });
});
