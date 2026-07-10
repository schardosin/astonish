import { useState, useEffect, useRef } from 'react';
import { ChevronDown, RotateCcw, Search } from 'lucide-react';
import ProviderModelSelector from '../ProviderModelSelector';

interface PreChatModelPickerProps {
  availableProviders: string[];
  provider: string;
  model: string;
  onChange: (provider: string, model: string) => void;
}

export default function PreChatModelPicker({
  availableProviders,
  provider,
  model,
  onChange,
}: PreChatModelPickerProps) {
  const wrapperRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [localProvider, setLocalProvider] = useState(provider);
  const [localModel, setLocalModel] = useState(model);
  const [showModelSelector, setShowModelSelector] = useState(false);

  useEffect(() => {
    setLocalProvider(provider);
    setLocalModel(model);
  }, [provider, model]);

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const handleApply = () => {
    onChange(localProvider, localModel);
    setOpen(false);
  };

  const handleReset = () => {
    onChange('', '');
    setOpen(false);
  };

  const handleModelSelected = (modelId: string) => {
    setLocalModel(modelId);
    setShowModelSelector(false);
  };

  const displayLabel = provider ? `${provider}${model ? '/' + model : ''}` : 'default';

  return (
    <div ref={wrapperRef} className="relative inline-block">
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1 px-2 py-1 text-xs rounded"
        style={{ color: 'var(--text-muted)', border: '1px solid var(--border-color)' }}
      >
        Model: {displayLabel}
        <ChevronDown size={12} />
      </button>
      {open && (
        <div
          className="absolute left-0 top-full mt-1 min-w-72 w-max max-w-md rounded-lg shadow-xl p-3 z-50"
          style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}
        >
          <label className="block text-xs mb-1" style={{ color: 'var(--text-primary)' }}>
            Provider
          </label>
          <select
            value={localProvider}
            onChange={(e) => { setLocalProvider(e.target.value); setLocalModel(''); }}
            role="combobox"
            className="w-full rounded px-2 py-1 text-sm mb-2"
            style={{
              background: 'var(--bg-primary, #1a1a2e)',
              border: '1px solid var(--border-color)',
              color: 'var(--text-primary)',
            }}
          >
            <option value="">(default — cascade)</option>
            {availableProviders.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
          <label className="block text-xs mb-1" style={{ color: 'var(--text-primary)' }}>
            Model
          </label>
          <button
            onClick={() => localProvider && setShowModelSelector(true)}
            disabled={!localProvider}
            className="w-full rounded px-2 py-1 text-sm mb-2 text-left flex items-center justify-between disabled:opacity-50"
            style={{
              background: 'var(--bg-primary, #1a1a2e)',
              border: '1px solid var(--border-color)',
              color: localModel ? 'var(--text-primary)' : 'var(--text-muted)',
            }}
            title={!localProvider ? 'Select a provider first' : 'Browse models'}
          >
            <span className="break-all">{localModel || (localProvider ? 'Click to browse models…' : 'Select provider first')}</span>
            <Search size={12} style={{ color: 'var(--text-muted)', flexShrink: 0 }} />
          </button>
          <div className="flex items-center justify-between">
            <button
              onClick={handleReset}
              className="flex items-center gap-1 text-xs"
              style={{ color: 'var(--text-muted)' }}
            >
              <RotateCcw size={12} /> Reset
            </button>
            <button
              onClick={handleApply}
              className="px-3 py-1 text-xs rounded text-white"
              style={{ background: 'var(--accent-color, #3b82f6)' }}
            >
              Apply
            </button>
          </div>
        </div>
      )}
      <ProviderModelSelector
        isOpen={showModelSelector}
        onClose={() => setShowModelSelector(false)}
        onSelect={handleModelSelected}
        currentModel={localModel}
        provider={localProvider}
      />
    </div>
  );
}
