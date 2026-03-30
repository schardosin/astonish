import React, { useState, useEffect } from 'react'
import type { InputNodeFormProps } from './nodeEditorTypes'

/**
 * Input Node Form - Horizontal layout
 */
export function InputNodeForm({ data, onChange, theme }: InputNodeFormProps) {
  // Local state for options field to prevent reformatting while typing
  const [optionsText, setOptionsText] = useState(() => {
    return data.options ? data.options.join(', ') : ''
  })
  
  // Update local state when data.options changes externally
  useEffect(() => {
    const newText = data.options ? data.options.join(', ') : ''
    // Only update if the parsed values are different (to avoid cursor jumping)
    const currentParsed = optionsText.split(',').map(s => s.trim()).filter(Boolean)
    const newParsed = data.options || []
    if (JSON.stringify(currentParsed) !== JSON.stringify(newParsed)) {
      setOptionsText(newText)
    }
  }, [data.options])
  
  const handleOptionsBlur = () => {
    const val = optionsText.split(',').map(s => s.trim()).filter(Boolean)
    onChange({ ...data, options: val.length > 0 ? val : undefined })
  }
  
  return (
    <div className="flex gap-6 h-full">
      {/* Left column - Options */}
      <div className="w-64 space-y-4">
        <div>
          <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
            Options (comma-separated)
          </label>
          <input
            type="text"
            value={optionsText}
            onChange={(e) => setOptionsText(e.target.value)}
            onBlur={handleOptionsBlur}
            className="w-full px-3 py-2 rounded border font-mono text-sm"
            style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
            placeholder="Option 1, Option 2, Option 3"
          />
          <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
            Separate options with commas
          </p>
        </div>
      </div>
      
      {/* Right column - Prompt */}
      <div className="flex-1 flex flex-col">
        <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
          Prompt
        </label>
        <textarea
          value={data.prompt || ''}
          onChange={(e) => onChange({ ...data, prompt: e.target.value })}
          className="w-full flex-1 min-h-[120px] px-3 py-2 rounded border font-mono text-sm resize-none"
          style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
          placeholder="Enter the prompt to show to the user..."
        />
      </div>
    </div>
  )
}
