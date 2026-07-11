import type { SetupField } from '../../api/fleetChat'

export function renderSetupField(
  field: SetupField,
  value: unknown,
  onChange: (value: unknown) => void,
) {
  const label = (
    <span className="block text-xs mb-1" style={{ color: 'var(--text-secondary)' }}>
      {field.label}{field.required ? ' *' : ''}
    </span>
  )

  switch (field.type) {
    case 'enum':
      return (
        <label className="block text-xs">
          {label}
          <select
            value={String(value ?? field.default ?? '')}
            onChange={e => onChange(e.target.value)}
            className="w-full px-3 py-2 rounded-lg"
            style={{ background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }}
          >
            {(field.options || []).map(opt => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </select>
        </label>
      )
    case 'cron':
    case 'repo':
    case 'string':
    case 'credential_ref':
      return (
        <label className="block text-xs">
          {label}
          <input
            value={String(value ?? field.default ?? '')}
            onChange={e => onChange(e.target.value)}
            placeholder={field.hint}
            className="w-full px-3 py-2 rounded-lg"
            style={{ background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }}
          />
        </label>
      )
    default:
      return (
        <label className="block text-xs">
          {label}
          <input
            value={String(value ?? '')}
            onChange={e => onChange(e.target.value)}
            className="w-full px-3 py-2 rounded-lg"
            style={{ background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }}
          />
        </label>
      )
  }
}
