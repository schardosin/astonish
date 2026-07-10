import { useState, useEffect, useRef, useMemo } from 'react';
import { AlertTriangle, ChevronDown, RotateCcw, Search } from 'lucide-react';
import { patchAppModel } from '../api/apps';
import type { AppModelStatus } from '../api/apps';
import ProviderModelSelector from './ProviderModelSelector';

interface AppModelPickerProps {
  slug: string;
  initialStatus: AppModelStatus;
  availableProviders: string[];
  onUpdate: (status: AppModelStatus) => void;
}

/**
 * Per-app model picker — same visual structure as the chat SessionModelPicker /
 * PreChatModelPicker (trigger + provider select + model browse).
 */
export default function AppModelPicker({
  slug,
  initialStatus,
  availableProviders,
  onUpdate,
}: AppModelPickerProps) {
  const wrapperRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [provider, setProvider] = useState(initialStatus.pinnedProvider || '');
  const [model, setModel] = useState(initialStatus.pinnedModel || '');
  const [error, setError] = useState<string | null>(null);
  const [showModelSelector, setShowModelSelector] = useState(false);

  const providers = useMemo(() => {
    const set = new Set<string>(availableProviders || []);
    if (initialStatus.pinnedProvider) set.add(initialStatus.pinnedProvider);
    return Array.from(set).sort();
  }, [availableProviders, initialStatus.pinnedProvider]);

  useEffect(() => {
    setProvider(initialStatus.pinnedProvider || '');
    setModel(initialStatus.pinnedModel || '');
  }, [initialStatus.pinnedProvider, initialStatus.pinnedModel]);

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setProvider(initialStatus.pinnedProvider || '');
        setModel(initialStatus.pinnedModel || '');
        setError(null);
        setShowModelSelector(false);
        setOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [initialStatus.pinnedProvider, initialStatus.pinnedModel]);

  const handleSave = async () => {
    setError(null);
    try {
      const patched = await patchAppModel(slug, provider || null, model || null);
      onUpdate(patched);
      setOpen(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save');
      setTimeout(() => setError(null), 4000);
    }
  };

  const handleReset = async () => {
    setError(null);
    try {
      const patched = await patchAppModel(slug, null, null);
      onUpdate(patched);
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

  const displayLabel = initialStatus.pinnedProvider
    ? `${initialStatus.pinnedProvider}${initialStatus.pinnedModel ? '/' + initialStatus.pinnedModel : ''}`
    : 'default';

  const noProviders = providers.length === 0;
  const currently = initialStatus.effectiveProvider
    ? `${initialStatus.effectiveProvider}/${initialStatus.effectiveModel || ''}`
    : 'cascade default';

  return (
    <div ref={wrapperRef} className="relative inline-block">
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1 px-2 py-1 text-xs rounded"
        style={{ color: 'var(--text-muted)', border: '1px solid var(--border-color)' }}
        title={`Model: ${currently}${initialStatus.pinnedProvider ? ' (pinned)' : ''}`}
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
            Currently: {currently}
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
            {initialStatus.pinnedProvider ? (
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
