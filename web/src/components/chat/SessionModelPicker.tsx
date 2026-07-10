import { useState, useEffect, useRef, useMemo } from 'react';
import { AlertTriangle, ChevronDown, RotateCcw, Search } from 'lucide-react';
import { patchSessionModel } from '../../api/studioChat';
import type { SessionModelStatus } from '../../api/studioChat';
import ProviderModelSelector from '../ProviderModelSelector';

interface SessionModelPickerProps {
  sessionId: string;
  modelStatus: SessionModelStatus;
  /** Extra provider names to union with modelStatus.availableProviders (e.g. pre-chat list). */
  availableProviders?: string[];
  /** Increment to force-open the popover (e.g. from ModelCredentialBanner). */
  openSignal?: number;
  onUpdate: (status: SessionModelStatus) => void;
}

/**
 * In-session model picker — same visual structure as PreChatModelPicker
 * (trigger button + popover with provider select + model browse), but persists
 * via patchSessionModel instead of local onChange.
 */
export default function SessionModelPicker({
  sessionId,
  modelStatus,
  availableProviders,
  openSignal,
  onUpdate,
}: SessionModelPickerProps) {
  const wrapperRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [provider, setProvider] = useState(modelStatus.pinnedProvider || '');
  const [model, setModel] = useState(modelStatus.pinnedModel || '');
  const [error, setError] = useState<string | null>(null);
  const [showModelSelector, setShowModelSelector] = useState(false);

  useEffect(() => {
    if (openSignal) setOpen(true);
  }, [openSignal]);

  const providers = useMemo(() => {
    const set = new Set<string>([
      ...(modelStatus.availableProviders || []),
      ...(availableProviders || []),
    ]);
    if (modelStatus.pinnedProvider) set.add(modelStatus.pinnedProvider);
    return Array.from(set).sort();
  }, [modelStatus.availableProviders, modelStatus.pinnedProvider, availableProviders]);

  useEffect(() => {
    setProvider(modelStatus.pinnedProvider || '');
    setModel(modelStatus.pinnedModel || '');
  }, [modelStatus.pinnedProvider, modelStatus.pinnedModel]);

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setProvider(modelStatus.pinnedProvider || '');
        setModel(modelStatus.pinnedModel || '');
        setError(null);
        setShowModelSelector(false);
        setOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [modelStatus.pinnedProvider, modelStatus.pinnedModel]);

  const handleSave = async () => {
    setError(null);
    try {
      const patched = await patchSessionModel(sessionId, provider, model);
      onUpdate({
        ...modelStatus,
        ...patched,
        availableProviders: modelStatus.availableProviders,
      });
      setOpen(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save');
      setTimeout(() => setError(null), 4000);
    }
  };

  const handleReset = async () => {
    setError(null);
    try {
      const patched = await patchSessionModel(sessionId, '', '');
      onUpdate({
        ...modelStatus,
        ...patched,
        availableProviders: modelStatus.availableProviders,
      });
      setOpen(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to reset');
      setTimeout(() => setError(null), 4000);
    }
  };

  const handleModelSelected = (modelId: string) => {
    setModel(modelId);
    setShowModelSelector(false);
  };

  const displayLabel = modelStatus.pinnedProvider
    ? `${modelStatus.pinnedProvider}${modelStatus.pinnedModel ? '/' + modelStatus.pinnedModel : ''}`
    : 'default';

  const noProviders = providers.length === 0;

  return (
    <div ref={wrapperRef} className="relative inline-block">
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1 px-2 py-1 text-xs rounded"
        style={{ color: 'var(--text-muted)', border: '1px solid var(--border-color)' }}
        title={`Model: ${modelStatus.effectiveProvider}/${modelStatus.effectiveModel}${modelStatus.pinnedProvider ? ' (pinned)' : ''}`}
      >
        Model: {displayLabel}
        <ChevronDown size={12} />
      </button>
      {open && (
        <div
          className="absolute left-0 top-full mt-1 min-w-72 w-max max-w-md rounded-lg shadow-xl p-3 z-50"
          style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}
        >
          <p className="text-[10px] mb-2" style={{ color: 'var(--text-muted)' }}>
            Currently: {modelStatus.effectiveProvider}/{modelStatus.effectiveModel}
          </p>
          <label className="block text-xs mb-1" style={{ color: 'var(--text-primary)' }}>
            Provider
          </label>
          <select
            value={provider}
            onChange={(e) => { setProvider(e.target.value); setModel(''); }}
            role="combobox"
            className="w-full rounded px-2 py-1 text-sm mb-2"
            style={{
              background: 'var(--bg-primary, #1a1a2e)',
              border: '1px solid var(--border-color)',
              color: 'var(--text-primary)',
            }}
          >
            <option value="">(default — cascade)</option>
            {providers.map((p) => (
              <option key={p} value={p}>{p}</option>
            ))}
          </select>
          <label className="block text-xs mb-1" style={{ color: 'var(--text-primary)' }}>
            Model
          </label>
          <button
            onClick={() => provider && setShowModelSelector(true)}
            disabled={!provider}
            className="w-full rounded px-2 py-1 text-sm mb-2 text-left flex items-center justify-between gap-2 disabled:opacity-50"
            style={{
              background: 'var(--bg-primary, #1a1a2e)',
              border: '1px solid var(--border-color)',
              color: model ? 'var(--text-primary)' : 'var(--text-muted)',
            }}
            title={!provider ? 'Select a provider first' : 'Browse models'}
          >
            <span className="break-all">{model || (provider ? 'Click to browse models…' : 'Select provider first')}</span>
            <Search size={12} style={{ color: 'var(--text-muted)', flexShrink: 0 }} />
          </button>
          <div className="flex items-center justify-between">
            {modelStatus.pinnedProvider ? (
              <button
                onClick={handleReset}
                className="flex items-center gap-1 text-xs"
                style={{ color: 'var(--text-muted)' }}
              >
                <RotateCcw size={12} /> Reset
              </button>
            ) : (
              <span />
            )}
            <button
              onClick={handleSave}
              disabled={noProviders}
              title={noProviders ? 'No providers configured' : undefined}
              className="px-3 py-1 text-xs rounded text-white disabled:opacity-50 ml-auto"
              style={{ background: 'var(--accent-color, #3b82f6)' }}
            >
              Save
            </button>
          </div>
          {error && (
            <div className="mt-2 text-xs flex items-center gap-1.5" style={{ color: 'var(--danger-color, #ef4444)' }}>
              <AlertTriangle size={12} />
              <span>{error}</span>
            </div>
          )}
        </div>
      )}
      <ProviderModelSelector
        isOpen={showModelSelector}
        onClose={() => setShowModelSelector(false)}
        onSelect={handleModelSelected}
        currentModel={model}
        provider={provider}
      />
    </div>
  );
}
