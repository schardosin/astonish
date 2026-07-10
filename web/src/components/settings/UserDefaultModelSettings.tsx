
import { useState, useEffect } from 'react';
import { Save, X, AlertCircle, Loader2, Settings2, Search } from 'lucide-react';
import { fetchUserDefaultModel, patchUserDefaultModel, UserDefaultModel } from '../../api/userSettings';
import { fetchSettings, fetchProviderModels, SettingsData, ProviderInfo } from './settingsApi';
import ProviderModelSelector from '../ProviderModelSelector';

export default function UserDefaultModelSettings() {
  const [form, setForm] = useState<UserDefaultModel>({ defaultProvider: '', defaultModel: '' });
  const [settings, setSettings] = useState<SettingsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);

  const [showModelSelector, setShowModelSelector] = useState(false);
  const [availableModels, setAvailableModels] = useState<string[]>([]);
  const [loadingModels, setLoadingModels] = useState(false);

  const enhancedModelTypes = ['openrouter', 'anthropic', 'gemini', 'groq', 'litellm', 'openai', 'poe', 'sap_ai_core', 'xai', 'lm_studio', 'ollama', 'openai_compat'];

  useEffect(() => {
    const loadData = async () => {
      try {
        const [userDefault, settingsData] = await Promise.all([
          fetchUserDefaultModel(),
          fetchSettings(),
        ]);
        setForm(userDefault);
        setSettings(settingsData);
      } catch (err: any) {
        setError(err.message);
      } finally {
        setLoading(false);
      }
    };
    loadData();
  }, []);

  const allEffectiveProviders: ProviderInfo[] = settings?.providers || [];
  const selectedDefaultType = allEffectiveProviders.find(p => p.name === form.defaultProvider)?.type || '';
  const inheritedDefaults = {
    provider: settings?.general.default_provider || '',
    model: settings?.general.default_model || '',
    source: 'Team'
  };

  const handleDefaultProviderChange = (providerName: string) => {
    setForm({ ...form, defaultProvider: providerName, defaultModel: '' });
    setAvailableModels([]);
    setError(null);
  };

  const loadModelsForDefaultProvider = async (providerId: string) => {
    if (!providerId) {
      setAvailableModels([]);
      return;
    }
    setLoadingModels(true);
    setError(null);
    try {
      const data = await fetchProviderModels(providerId);
      setAvailableModels(data.models || []);
    } catch (err: any) {
      setError(err.message);
      setAvailableModels([]);
    } finally {
      setLoadingModels(false);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    try {
      await patchUserDefaultModel(form.defaultProvider, form.defaultModel);
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 2000);
    } catch (err: any) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  };

  const handleClear = async () => {
    setForm({ defaultProvider: '', defaultModel: '' });
    setSaving(true);
    setError(null);
    try {
      await patchUserDefaultModel('', '');
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 2000);
    } catch (err: any) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
        <div className="flex items-center justify-center p-8">
            <Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} />
        </div>
    );
  }

  return (
    <>
      <div className="rounded-lg border p-4" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
        <div className="flex items-center gap-2 mb-4">
          <Settings2 size={16} style={{ color: '#a855f7' }} />
          <h3 className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>
            Default Model
          </h3>
        </div>

        <div className="space-y-4">
          <div>
            <label htmlFor="user-default-provider" className="block text-xs font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>
              Default Provider
            </label>
            {allEffectiveProviders.length > 0 ? (
              <div className="flex items-center gap-2">
                <select
                  id="user-default-provider"
                  value={form.defaultProvider}
                  onChange={(e) => handleDefaultProviderChange(e.target.value)}
                  className="flex-1 px-3 py-2 rounded-lg border text-sm"
                  style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                >
                  <option value="">Not Set</option>
                  {allEffectiveProviders.map(p => (
                    <option key={p.name} value={p.name}>
                      {p.name} ({p.type})
                    </option>
                  ))}
                </select>
              </div>
            ) : (
              <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
                No providers configured at the team level.
              </p>
            )}
          </div>

          <div>
            <label htmlFor="user-default-model" className="block text-xs font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>
              Default Model
            </label>
            {form.defaultProvider ? (
              <>
                {enhancedModelTypes.includes(selectedDefaultType) ? (
                  <div className="flex items-center gap-2">
                    <button
                      onClick={() => setShowModelSelector(true)}
                      className="flex-1 px-3 py-2 rounded-lg border text-sm text-left flex items-center justify-between"
                      style={{
                        background: 'var(--bg-primary)',
                        borderColor: 'var(--border-color)',
                        color: form.defaultModel ? 'var(--text-primary)' : 'var(--text-muted)'
                      }}
                    >
                      <span className="truncate">
                        {form.defaultModel || 'Click to select a model...'}
                      </span>
                      <Search size={14} style={{ color: 'var(--text-muted)' }} />
                    </button>
                  </div>
                ) : (
                  <div className="flex items-center gap-2">
                    <div className="relative flex-1">
                      <select
                        id="user-default-model"
                        value={form.defaultModel}
                        onChange={(e) => setForm({ ...form, defaultModel: e.target.value })}
                        onFocus={() => {
                          if (form.defaultProvider && availableModels.length === 0 && !loadingModels) {
                            loadModelsForDefaultProvider(form.defaultProvider);
                          }
                        }}
                        className="w-full px-3 py-2 rounded-lg border text-sm"
                        style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                      >
                        {availableModels.length === 0 && !loadingModels && (
                          <option value={form.defaultModel || ''}>
                            {form.defaultModel || 'Click to load models...'}
                          </option>
                        )}
                        {loadingModels && <option value="">Loading models...</option>}
                        {availableModels.length > 0 && (
                          <>
                            <option value="">Select a model...</option>
                            {availableModels.map(model => (
                              <option key={model} value={model}>{model}</option>
                            ))}
                          </>
                        )}
                      </select>
                      {loadingModels && (
                        <div className="absolute right-8 top-1/2 -translate-y-1/2">
                          <Loader2 size={14} className="animate-spin" style={{ color: 'var(--accent)' }} />
                        </div>
                      )}
                    </div>
                  </div>
                )}
              </>
            ) : (
              <div className="px-3 py-2 rounded-lg border text-sm" style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-muted)' }}>
                Not Set
              </div>
            )}
          </div>

          {!form.defaultProvider && (
            <div className="flex items-start gap-2 p-2.5 rounded-lg" style={{ background: 'var(--bg-primary)', border: '1px dashed var(--border-color)' }}>
              <Settings2 size={12} className="mt-0.5 flex-shrink-0" style={{ color: 'var(--text-muted)' }} />
              <div className="text-xs" style={{ color: 'var(--text-muted)' }}>
                {inheritedDefaults.provider ? (
                  <>
                    <span className="font-medium">Inheriting from {inheritedDefaults.source}:</span>{' '}
                    <span>{inheritedDefaults.provider}</span>
                    {inheritedDefaults.model && <span> / </span>}
                    {inheritedDefaults.model && <span className="font-mono">{inheritedDefaults.model}</span>}
                  </>
                ) : (
                    "No default configured at any level"
                )}
              </div>
            </div>
          )}

          <div className="flex items-center gap-3">
            <button
              onClick={handleSave}
              disabled={saving}
              className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-white text-sm font-medium transition-all shadow-sm hover:shadow-md hover:scale-[1.02] active:scale-95 disabled:opacity-50"
              style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }}
            >
              <Save size={14} />
              {saving ? 'Saving...' : 'Save Default'}
            </button>
            <button
              onClick={handleClear}
              disabled={saving}
              className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-medium transition-all border disabled:opacity-50"
              style={{ borderColor: 'var(--border-color)', color: 'var(--text-secondary)', background: 'var(--bg-primary)' }}
              >
                <X size={14} />
                Clear
              </button>
          </div>
          
          {saveSuccess && <div className="text-sm text-green-500">Default model saved.</div>}

          {error && (
            <div className="flex items-center gap-2 p-2 rounded text-xs" style={{ background: 'rgba(239, 68, 68, 0.1)', color: '#f87171' }}>
              <AlertCircle size={12} />
              {error}
            </div>
          )}
        </div>
      </div>
      <ProviderModelSelector
        isOpen={showModelSelector}
        onClose={() => setShowModelSelector(false)}
        onSelect={(modelId) => {
          setForm({ ...form, defaultModel: modelId });
          setShowModelSelector(false);
        }}
        currentModel={form.defaultModel}
        provider={form.defaultProvider}
      />
    </>
  );
}
