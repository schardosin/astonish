import { useState, useEffect, useRef } from 'react';
import { patchAppModel } from '../api/apps';
import type { AppModelStatus } from '../api/apps';
import { ChevronDown, AlertTriangle } from 'lucide-react';

interface AppModelPickerProps {
  slug: string;
  initialStatus: AppModelStatus;
  onUpdate: (status: AppModelStatus) => void;
}

export default function AppModelPicker({ slug, initialStatus, onUpdate }: AppModelPickerProps) {
  const [provider, setProvider] = useState(initialStatus.pinnedProvider || '');
  const [model, setModel] = useState(initialStatus.pinnedModel || '');
  const [isOpen, setIsOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const wrapperRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (wrapperRef.current && !wrapperRef.current.contains(event.target as Node)) {
        setIsOpen(false);
        // Reset unsaved changes
        setProvider(initialStatus.pinnedProvider || '');
        setModel(initialStatus.pinnedModel || '');
        setError(null);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, [wrapperRef, initialStatus]);

  const handleSave = async () => {
    setError(null);
    try {
      const newStatus = await patchAppModel(slug, provider, model);
      onUpdate(newStatus);
      setIsOpen(false);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to save model';
      setError(message);
      setTimeout(() => setError(null), 4000);
    }
  };

  const displayName = initialStatus.pinnedProvider
    ? `${initialStatus.pinnedProvider}/${initialStatus.pinnedModel}`
    : 'Cascade';

  return (
    <div className="relative" ref={wrapperRef}>
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="flex items-center gap-1 text-xs px-2 py-1 rounded transition-colors"
        style={{ color: 'var(--text-muted)', background: 'var(--bg-tertiary)' }}
      >
        <span>{displayName}</span>
        <ChevronDown size={12} />
      </button>

      {isOpen && (
        <div
          className="absolute right-0 top-full mt-2 w-64 rounded-lg shadow-xl p-3 z-10"
          style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}
        >
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-muted)' }}>
            Provider
          </label>
          <input
            type="text"
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
            placeholder="e.g. openai"
            className="w-full text-xs px-2 py-1.5 rounded"
            style={{ background: 'var(--bg-primary)', border: '1px solid var(--border-color)', color: 'var(--text-primary)'}}
          />
          <label className="block text-xs font-medium mt-2 mb-1" style={{ color: 'var(--text-muted)' }}>
            Model
          </label>
          <input
            type="text"
            value={model}
            onChange={(e) => setModel(e.target.value)}
            placeholder="e.g. gpt-4-turbo"
            className="w-full text-xs px-2 py-1.5 rounded"
            style={{ background: 'var(--bg-primary)', border: '1px solid var(--border-color)', color: 'var(--text-primary)'}}
          />
          <div className="mt-3 flex justify-end gap-2">
            <button
              onClick={() => setIsOpen(false)}
              className="px-3 py-1 text-xs rounded"
              style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)'}}
            >
              Cancel
            </button>
            <button
              onClick={handleSave}
              className="px-3 py-1 text-xs rounded text-white"
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
    </div>
  );
}
