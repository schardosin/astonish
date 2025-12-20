import React, { useState, useEffect, useCallback, useRef } from 'react'
import { X, Edit3, Brain, Wrench, Settings, MessageSquare, Plus, Trash2, AlertCircle, Sparkles, Link, ChevronRight, ChevronDown } from 'lucide-react'
import ToolSelector from './ToolSelector'

// Node type icons
const NODE_ICONS = {
  input: Edit3,
  llm: Brain,
  tool: Wrench,
  updateState: Settings,
  update_state: Settings,
  output: MessageSquare,
}

// Node type colors
const NODE_COLORS = {
  input: '#E9D5FF',
  llm: '#6B46C1',
  tool: '#805AD5',
  updateState: '#4A5568',
  update_state: '#4A5568',
  output: '#9F7AEA',
}

/**
 * VariablePanel - Left sidebar showing variables grouped by node
 * Inserts variables into the currently focused textarea
 */
function VariablePanel({ variableGroups, activeTextareaRef, getValue, setValue }) {
  const [filterNode, setFilterNode] = useState('all')
  const [collapsed, setCollapsed] = useState({})
  
  if (!variableGroups || variableGroups.length === 0) {
    return (
      <div className="w-48 flex-shrink-0 border-r pr-3 mr-3" style={{ borderColor: 'var(--border-color)' }}>
        <div className="text-xs font-medium mb-2" style={{ color: 'var(--text-muted)' }}>
          Variables
        </div>
        <div className="text-xs italic" style={{ color: 'var(--text-muted)' }}>
          No variables available. Add output_model to nodes to create variables.
        </div>
      </div>
    )
  }
  
  const insertVariable = (varName) => {
    const textarea = activeTextareaRef?.current
    const currentValue = getValue() || ''
    
    if (!textarea) {
      // Fallback: append to end
      setValue(currentValue + `{${varName}}`)
      return
    }
    
    const start = textarea.selectionStart || 0
    const end = textarea.selectionEnd || 0
    const insertion = `{${varName}}`
    const newValue = currentValue.slice(0, start) + insertion + currentValue.slice(end)
    setValue(newValue)
    
    // Restore cursor after insertion
    setTimeout(() => {
      textarea.focus()
      const newPos = start + insertion.length
      textarea.setSelectionRange(newPos, newPos)
    }, 0)
  }
  
  const toggleCollapse = (nodeName) => {
    setCollapsed(prev => ({ ...prev, [nodeName]: !prev[nodeName] }))
  }
  
  const filteredGroups = filterNode === 'all' 
    ? variableGroups 
    : variableGroups.filter(g => g.nodeName === filterNode)
  
  return (
    <div className="w-56 flex-shrink-0 border-r pr-3 mr-3 flex flex-col overflow-hidden" style={{ borderColor: 'var(--border-color)', maxHeight: '100%' }}>
      {/* Header */}
      <div className="flex items-center gap-2 mb-2">
        <Link size={12} style={{ color: 'var(--text-muted)' }} />
        <span className="text-xs font-medium" style={{ color: 'var(--text-secondary)' }}>
          Variables
        </span>
      </div>
      
      {/* Node Filter */}
      <select
        value={filterNode}
        onChange={(e) => setFilterNode(e.target.value)}
        className="w-full px-2 py-1 rounded text-xs mb-2"
        style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }}
      >
        <option value="all">All nodes</option>
        {variableGroups.map(g => (
          <option key={g.nodeName} value={g.nodeName}>
            {g.nodeName} ({g.variables.length})
          </option>
        ))}
      </select>
      
      {/* Variable List */}
      <div className="flex-1 overflow-y-auto space-y-2">
        {filteredGroups.map(group => (
          <div key={group.nodeName}>
            {/* Node Header - only show if not filtered to single node */}
            {filterNode === 'all' && (
              <button
                onClick={() => toggleCollapse(group.nodeName)}
                className="w-full flex items-center gap-1 text-xs font-medium py-1 hover:bg-purple-500/10 rounded transition-colors"
                style={{ color: 'var(--text-secondary)' }}
              >
                {collapsed[group.nodeName] ? (
                  <ChevronRight size={12} />
                ) : (
                  <ChevronDown size={12} />
                )}
                <span className="truncate flex-1 text-left">{group.nodeName}</span>
                <span className="text-xs px-1 rounded" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
                  {group.variables.length}
                </span>
              </button>
            )}
            
            {/* Variables */}
            {(!collapsed[group.nodeName] || filterNode !== 'all') && (
              <div className={`flex flex-wrap gap-1 ${filterNode === 'all' ? 'pl-4' : ''}`}>
                {group.variables.map(v => (
                  <button
                    key={v}
                    type="button"
                    onClick={() => insertVariable(v)}
                    className="px-2 py-0.5 text-xs font-mono rounded transition-all hover:scale-105 hover:bg-purple-500/30"
                    style={{ 
                      background: 'rgba(168, 85, 247, 0.15)', 
                      color: '#c084fc',
                      border: '1px solid rgba(168, 85, 247, 0.25)'
                    }}
                    title={`Insert {${v}} at cursor`}
                  >
                    {v}
                  </button>
                ))}
              </div>
            )}
          </div>
        ))}
      </div>
      
      {/* Help text */}
      <div className="text-xs mt-2 pt-2 border-t" style={{ color: 'var(--text-muted)', borderColor: 'var(--border-color)' }}>
        Click to insert at cursor
      </div>
    </div>
  )
}

/**
 * HighlightedTextarea - Textarea with variable highlighting overlay
 * Uses a transparent textarea over a highlighted pre element
 */
const HighlightedTextarea = React.forwardRef(function HighlightedTextarea(
  { value, onChange, onFocus, isActive, placeholder, className, style, validVariables = [] },
  ref
) {
  const containerRef = useRef(null)
  const [scrollTop, setScrollTop] = useState(0)
  
  // Get flat list of all valid variable names
  const allValidVars = validVariables.flatMap(g => g.variables || [])
  
  // Highlight {variables} in the text - only valid ones, exclude {{double braces}}
  const getHighlightedContent = () => {
    if (!value) return <span style={{ color: 'var(--text-muted)' }}>{placeholder}</span>
    
    // Match {variable} but NOT {{escaped}} - use negative lookbehind/lookahead
    // Split by single braces pattern, preserving the delimiter
    const parts = value.split(/(\{[^{}]+\})/g)
    
    return parts.map((part, i) => {
      // Check if it's a {variable} pattern (single braces only)
      const match = part.match(/^\{([^{}]+)\}$/)
      if (match) {
        const varName = match[1]
        // Only highlight if it's a valid variable
        if (allValidVars.includes(varName)) {
          return (
            <span 
              key={i}
              style={{ 
                background: 'rgba(168, 85, 247, 0.3)',
                color: '#c084fc',
                borderRadius: '2px',
              }}
            >
              {part}
            </span>
          )
        }
      }
      return <span key={i}>{part}</span>
    })
  }
  
  const handleScroll = (e) => {
    setScrollTop(e.target.scrollTop)
  }
  
  // Common styles for exact alignment
  const commonStyles = {
    fontFamily: 'ui-monospace, SFMono-Regular, SF Mono, Menlo, Consolas, Liberation Mono, monospace',
    fontSize: '0.875rem',
    lineHeight: '1.25rem',
    padding: '0.5rem 0.75rem',
    margin: 0,
    border: '1px solid',
    borderRadius: '0.25rem',
    whiteSpace: 'pre-wrap',
    wordBreak: 'break-word',
    overflowWrap: 'break-word',
    letterSpacing: 'normal',
  }
  
  return (
    <div 
      ref={containerRef}
      className="relative flex-1"
      style={{ minHeight: '120px' }}
    >
      {/* Highlighted background layer */}
      <div
        className={`absolute inset-0 overflow-hidden pointer-events-none ${isActive ? 'ring-1 ring-purple-500' : ''}`}
        style={{ 
          ...commonStyles,
          background: 'var(--bg-primary)', 
          borderColor: isActive ? '#7c3aed' : 'var(--border-color)', 
          color: 'var(--text-primary)',
        }}
      >
        <div style={{ transform: `translateY(-${scrollTop}px)` }}>
          {getHighlightedContent()}
        </div>
      </div>
      
      {/* Transparent textarea on top for editing */}
      <textarea
        ref={ref}
        value={value || ''}
        onChange={onChange}
        onFocus={onFocus}
        onScroll={handleScroll}
        className="absolute inset-0 w-full h-full resize-none"
        style={{ 
          ...commonStyles,
          background: 'transparent',
          color: 'transparent',
          caretColor: 'var(--text-primary)',
          borderColor: 'transparent',
          outline: 'none',
        }}
        placeholder=""
      />
    </div>
  )
})

/**
 * Output Model Editor - key-value list for output_model field
 */
function OutputModelEditor({ value, onChange, theme, hideLabel = false, singleField = false }) {
  const entries = Object.entries(value || {})
  
  const handleAdd = () => {
    const newKey = `field_${Object.keys(value || {}).length + 1}`
    onChange({ ...value, [newKey]: 'str' })
  }
  
  const handleRemove = (key) => {
    const newValue = { ...value }
    delete newValue[key]
    onChange(newValue)
  }
  
  const handleKeyChange = (oldKey, newKey) => {
    if (oldKey === newKey) return
    const newValue = {}
    Object.entries(value || {}).forEach(([k, v]) => {
      newValue[k === oldKey ? newKey : k] = v
    })
    onChange(newValue)
  }
  
  const handleTypeChange = (key, type) => {
    onChange({ ...value, [key]: type })
  }
  
  return (
    <div className="space-y-2">
      {!hideLabel && (
        <div className="flex items-center justify-between">
          <label className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>
            Output Model
          </label>
          {!singleField && (
            <button
              onClick={handleAdd}
              className="flex items-center gap-1 text-xs px-2 py-1 rounded bg-purple-600 text-white hover:bg-purple-700"
            >
              <Plus size={12} /> Add
            </button>
          )}
        </div>
      )}
      
      {entries.length === 0 ? (
        <div className="text-xs py-2" style={{ color: 'var(--text-muted)' }}>
          {singleField ? 'Enter a variable name.' : 'No fields. Add at least one.'}
        </div>
      ) : (
        <div className="space-y-1">
          {entries.slice(0, singleField ? 1 : undefined).map(([key, type], idx) => (
            <div key={idx} className="flex items-center gap-1">
              <input
                type="text"
                value={key}
                onChange={(e) => handleKeyChange(key, e.target.value)}
                className="flex-1 px-2 py-1 text-xs rounded border"
                style={{ 
                  background: 'var(--bg-primary)', 
                  borderColor: 'var(--border-color)',
                  color: 'var(--text-primary)'
                }}
                placeholder="field"
              />
              <select
                value={type}
                onChange={(e) => handleTypeChange(key, e.target.value)}
                className="px-1 py-1 text-xs rounded border"
                style={{ 
                  background: 'var(--bg-primary)', 
                  borderColor: 'var(--border-color)',
                  color: 'var(--text-primary)'
                }}
              >
                <option value="str">str</option>
                <option value="int">int</option>
                <option value="list">list</option>
                <option value="any">any</option>
                <option value="bool">bool</option>
              </select>
              {!singleField && (
                <button
                  onClick={() => handleRemove(key)}
                  className="p-0.5 text-red-400 hover:text-red-300"
                >
                  <Trash2 size={12} />
                </button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

/**
 * Update State Form - Horizontal layout with source_variable support
 */
function UpdateStateForm({ data, onChange, theme }) {
  // Determine if using source_variable or value
  const useSourceVariable = data.source_variable !== undefined
  
  return (
    <div className="flex gap-6 h-full">
      {/* Left column - Settings */}
      <div className="w-64 space-y-4">
        <div>
          <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
            Action
          </label>
          <select
            value={data.action || 'overwrite'}
            onChange={(e) => onChange({ ...data, action: e.target.value })}
            className="w-full px-3 py-2 rounded border"
            style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
          >
            <option value="overwrite">overwrite</option>
            <option value="append">append</option>
          </select>
        </div>
        
        <div>
          <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
            Source Type
          </label>
          <select
            value={useSourceVariable ? 'variable' : 'value'}
            onChange={(e) => {
              if (e.target.value === 'variable') {
                // Switch to source_variable mode
                const newData = { ...data, source_variable: data.value || '' }
                delete newData.value
                onChange(newData)
              } else {
                // Switch to value mode
                const newData = { ...data, value: data.source_variable || '' }
                delete newData.source_variable
                onChange(newData)
              }
            }}
            className="w-full px-3 py-2 rounded border"
            style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
          >
            <option value="variable">From Variable</option>
            <option value="value">Literal Value</option>
          </select>
        </div>
      </div>
      
      {/* Right column - Source Variable or Value */}
      <div className="flex-1">
        {useSourceVariable ? (
          <>
            <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
              Source Variable
            </label>
            <input
              type="text"
              value={data.source_variable || ''}
              onChange={(e) => onChange({ ...data, source_variable: e.target.value })}
              className="w-full px-3 py-2 rounded border font-mono text-sm"
              style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
              placeholder="Enter state variable name..."
            />
            <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
              The value from this state variable will be used
            </p>
          </>
        ) : (
          <>
            <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
              Value
            </label>
            <textarea
              value={data.value || ''}
              onChange={(e) => onChange({ ...data, value: e.target.value })}
              className="w-full h-32 px-3 py-2 rounded border font-mono text-sm resize-none"
              style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
              placeholder="Enter value or expression..."
            />
          </>
        )}
      </div>
    </div>
  )
}

/**
 * Input Node Form - Horizontal layout
 */
function InputNodeForm({ data, onChange, theme }) {
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

/**
 * LLM Node Form - Horizontal layout with tabs
 */
function LlmNodeForm({ data, onChange, theme, availableTools = [], availableVariables = [] }) {
  const [activeTab, setActiveTab] = useState('prompts')
  const [activeField, setActiveField] = useState('prompt') // 'system' or 'prompt'
  const systemPromptRef = useRef(null)
  const userPromptRef = useRef(null)
  
  const currentTools = data.tools_selection || []
  const toolNames = availableTools.map(t => t.name)
  
  const handleAddTool = (toolName) => {
    if (toolName && !currentTools.includes(toolName)) {
      onChange({ ...data, tools_selection: [...currentTools, toolName] })
    }
  }
  
  const handleRemoveTool = (toolName) => {
    onChange({ ...data, tools_selection: currentTools.filter(t => t !== toolName) })
  }
  
  // Get the currently active textarea ref based on focus tracking
  const getActiveRef = () => activeField === 'system' ? systemPromptRef : userPromptRef
  const getActiveValue = () => activeField === 'system' ? (data.system || '') : (data.prompt || '')
  const setActiveValue = (val) => {
    if (activeField === 'system') {
      onChange({ ...data, system: val || undefined })
    } else {
      onChange({ ...data, prompt: val })
    }
  }
  
  const tabs = [
    { id: 'prompts', label: 'Prompts' },
    { id: 'output', label: 'Output' },
    { id: 'tools', label: 'Tools' },
    { id: 'advanced', label: 'Advanced' },
  ]
  
  return (
    <div className="h-full flex flex-col">
      {/* Tabs */}
      <div className="flex gap-1 mb-3">
        {tabs.map(tab => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={`px-3 py-1.5 text-sm font-medium rounded-t transition-colors ${
              activeTab === tab.id ? 'bg-purple-600 text-white' : 'bg-gray-600/30'
            }`}
            style={{ color: activeTab !== tab.id ? 'var(--text-muted)' : undefined }}
          >
            {tab.label}
          </button>
        ))}
      </div>
      
      {/* Tab Content */}
      <div className="flex-1 overflow-hidden">
        {activeTab === 'prompts' && (
          <div className="flex h-full overflow-hidden">
            {/* Variable Panel - Left Sidebar */}
            <VariablePanel
              variableGroups={availableVariables}
              activeTextareaRef={getActiveRef()}
              getValue={getActiveValue}
              setValue={setActiveValue}
            />
            
            {/* Prompts - Right side */}
            <div className="flex-1 flex gap-4">
              {/* System Prompt */}
              <div className="flex-1 flex flex-col">
                <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
                  System Prompt (optional)
                </label>
                <HighlightedTextarea
                  ref={systemPromptRef}
                  value={data.system || ''}
                  onChange={(e) => onChange({ ...data, system: e.target.value || undefined })}
                  onFocus={() => setActiveField('system')}
                  isActive={activeField === 'system'}
                  placeholder="Enter system instructions..."
                  validVariables={availableVariables}
                />
              </div>
              
              {/* User Prompt */}
              <div className="flex-1 flex flex-col">
                <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
                  Prompt
                </label>
                <HighlightedTextarea
                  ref={userPromptRef}
                  value={data.prompt || ''}
                  onChange={(e) => onChange({ ...data, prompt: e.target.value })}
                  onFocus={() => setActiveField('prompt')}
                  isActive={activeField === 'prompt'}
                  placeholder="Enter the LLM prompt. Use {variable} for state references..."
                  validVariables={availableVariables}
                />
              </div>
            </div>
          </div>
        )}
        
        {activeTab === 'output' && (
          <div className="flex gap-8 h-full">
            {/* Left column - Output Model */}
            <div className="flex-1">
              <div className="flex items-center justify-between mb-3">
                <div>
                  <label className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>
                    Output Model
                  </label>
                  <p className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
                    Variables saved to state
                  </p>
                </div>
                <button
                  onClick={() => {
                    const newKey = `field_${Object.keys(data.output_model || {}).length + 1}`
                    onChange({ ...data, output_model: { ...(data.output_model || {}), [newKey]: 'str' } })
                  }}
                  className="flex items-center gap-1 px-2.5 py-1 text-xs rounded bg-purple-600 hover:bg-purple-500 text-white transition-colors"
                >
                  <Plus size={12} />
                  Add
                </button>
              </div>
              <OutputModelEditor
                value={data.output_model}
                onChange={(newModel) => onChange({ ...data, output_model: newModel })}
                theme={theme}
                hideLabel={true}
              />
            </div>
            
            {/* Right column - User Message */}
            <div className="flex-1">
              <div className="flex items-center justify-between mb-3">
                <div>
                  <label className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>
                    User Message
                  </label>
                  <p className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
                    Display to user
                  </p>
                </div>
                <button
                  onClick={() => {
                    const newItems = [...(data.user_message || []), '']
                    onChange({ ...data, user_message: newItems })
                  }}
                  className="flex items-center gap-1 px-2.5 py-1 text-xs rounded bg-purple-600 hover:bg-purple-500 text-white transition-colors"
                >
                  <Plus size={12} />
                  Add
                </button>
              </div>
              
              <div className="space-y-2">
                {(data.user_message || []).map((item, idx) => (
                  <div key={idx} className="flex items-center gap-2">
                    <input
                      type="text"
                      value={item}
                      onChange={(e) => {
                        const newItems = [...(data.user_message || [])]
                        newItems[idx] = e.target.value
                        onChange({ ...data, user_message: newItems })
                      }}
                      className="flex-1 px-3 py-1.5 rounded border font-mono text-sm"
                      style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                      placeholder="Variable name..."
                    />
                    <button
                      onClick={() => {
                        const newItems = (data.user_message || []).filter((_, i) => i !== idx)
                        onChange({ ...data, user_message: newItems.length > 0 ? newItems : undefined })
                      }}
                      className="p-1.5 text-red-400 hover:text-red-300 hover:bg-red-500/20 rounded"
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                ))}
                {(!data.user_message || data.user_message.length === 0) && (
                  <p className="text-xs italic py-2" style={{ color: 'var(--text-muted)' }}>
                    No items. Add output_model variables to show results.
                  </p>
                )}
              </div>
            </div>
          </div>
        )}
        
        {activeTab === 'tools' && (
          <div className="flex gap-6">
            <div className="w-48 flex items-center justify-between">
              <label className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>
                Enable Tools
              </label>
              <input
                type="checkbox"
                checked={data.tools === true}
                onChange={(e) => onChange({ ...data, tools: e.target.checked || undefined })}
                className="w-4 h-4 accent-purple-600"
              />
            </div>
            
            {data.tools && (
              <>
                <div className="flex-1 max-w-md">
                  <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
                    Tools Selection
                  </label>
                  
                  <ToolSelector
                    availableTools={availableTools}
                    selectedTools={currentTools}
                    onAddTool={handleAddTool}
                    onRemoveTool={handleRemoveTool}
                    placeholder="Search tools..."
                  />
                  
                  {/* Show invalid tools warning */}
                  {currentTools.some(t => !toolNames.includes(t)) && (
                    <div className="mt-2 p-2 rounded border border-red-500 bg-red-500/10">
                      <div className="flex items-center gap-2 text-xs text-red-400">
                        <AlertCircle size={12} />
                        <span>Some selected tools are not available</span>
                      </div>
                    </div>
                  )}
                </div>
                
                <div className="w-48 flex items-center justify-between">
                  <label className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>
                    Auto-approve
                  </label>
                  <input
                    type="checkbox"
                    checked={data.tools_auto_approval === true}
                    onChange={(e) => onChange({ ...data, tools_auto_approval: e.target.checked || undefined })}
                    className="w-4 h-4 accent-purple-600"
                  />
                </div>
              </>
            )}
          </div>
        )}
        
        {activeTab === 'advanced' && (
          <div className="flex gap-6 flex-wrap">
            <div className="w-40">
              <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
                Output Action
              </label>
              <select
                value={data.output_action || 'overwrite'}
                onChange={(e) => onChange({ ...data, output_action: e.target.value === 'overwrite' ? undefined : e.target.value })}
                className="w-full px-3 py-2 rounded border"
                style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
              >
                <option value="overwrite">overwrite</option>
                <option value="append">append</option>
              </select>
            </div>
            
            <div className="w-32">
              <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
                Max Retries
              </label>
              <input
                type="number"
                value={data.max_retries || ''}
                onChange={(e) => onChange({ ...data, max_retries: e.target.value ? parseInt(e.target.value) : undefined })}
                className="w-full px-3 py-2 rounded border"
                style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                placeholder="3"
                min={0}
              />
            </div>
            
            <div className="w-36 flex items-center gap-2">
              <input
                type="checkbox"
                checked={data.print_state !== false}
                onChange={(e) => onChange({ ...data, print_state: e.target.checked ? undefined : false })}
                className="w-4 h-4 accent-purple-600"
              />
              <label className="text-sm" style={{ color: 'var(--text-secondary)' }}>Print State</label>
            </div>
            
            <div className="w-36 flex items-center gap-2">
              <input
                type="checkbox"
                checked={data.print_prompt !== false}
                onChange={(e) => onChange({ ...data, print_prompt: e.target.checked ? undefined : false })}
                className="w-4 h-4 accent-purple-600"
              />
              <label className="text-sm" style={{ color: 'var(--text-secondary)' }}>Print Prompt</label>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

/**
 * Tool Node Form - Horizontal layout with dropdown
 */
function ToolNodeForm({ data, onChange, theme, availableTools = [] }) {
  const currentTools = data.tools_selection || []
  const toolNames = availableTools.map(t => t.name)
  
  // Check if any selected tool is not in the available list
  const hasInvalidTool = currentTools.length > 0 && currentTools.some(t => !toolNames.includes(t))
  
  const handleAddTool = (toolName) => {
    if (toolName && !currentTools.includes(toolName)) {
      onChange({ ...data, tools_selection: [...currentTools, toolName] })
    }
  }
  
  const handleRemoveTool = (toolName) => {
    onChange({ ...data, tools_selection: currentTools.filter(t => t !== toolName) })
  }
  
  return (
    <div className="flex gap-6 h-full">
      {/* Left column - Settings */}
      <div className="w-80 space-y-4">
        <div>
          <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
            Tools Selection
          </label>
          
          <ToolSelector
            availableTools={availableTools}
            selectedTools={currentTools}
            onAddTool={handleAddTool}
            onRemoveTool={handleRemoveTool}
            placeholder="Search tools..."
          />
          
          {/* Show invalid tools warning */}
          {hasInvalidTool && (
            <div className="mt-2 p-2 rounded border border-red-500 bg-red-500/10">
              <div className="flex items-center gap-2 text-xs text-red-400">
                <AlertCircle size={12} />
                <span>Some selected tools are not available</span>
              </div>
            </div>
          )}
        </div>
        
        <div className="flex items-center justify-between">
          <label className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>
            Auto-approve
          </label>
          <input
            type="checkbox"
            checked={data.tools_auto_approval === true}
            onChange={(e) => onChange({ ...data, tools_auto_approval: e.target.checked || undefined })}
            className="w-4 h-4 accent-purple-600"
          />
        </div>
      </div>
      
      {/* Right column - Args */}
      <div className="flex-1">
        <div className="flex items-center justify-between mb-2">
          <div>
            <label className="text-sm font-medium block" style={{ color: 'var(--text-secondary)' }}>
              Arguments
            </label>
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
              Key-value pairs passed to the tool
            </p>
          </div>
          <button
            onClick={() => {
              const newKey = `arg_${Object.keys(data.args || {}).length + 1}`
              onChange({ ...data, args: { ...(data.args || {}), [newKey]: '{variable}' } })
            }}
            className="flex items-center gap-1 px-2.5 py-1 text-xs rounded bg-purple-600 hover:bg-purple-500 text-white transition-colors"
          >
            <Plus size={12} />
            Add
          </button>
        </div>
        
        <div className="space-y-2">
          {Object.entries(data.args || {}).map(([key, value], idx) => (
            <div key={idx} className="flex items-center gap-2">
              <input
                type="text"
                value={key}
                onChange={(e) => {
                  const newArgs = {}
                  Object.entries(data.args || {}).forEach(([k, v]) => {
                    newArgs[k === key ? e.target.value : k] = v
                  })
                  onChange({ ...data, args: newArgs })
                }}
                className="w-32 px-2 py-1.5 rounded border font-mono text-sm"
                style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                placeholder="key"
              />
              <span className="text-xs" style={{ color: 'var(--text-muted)' }}>=</span>
              <input
                type="text"
                value={typeof value === 'string' ? value : JSON.stringify(value)}
                onChange={(e) => {
                  onChange({ ...data, args: { ...(data.args || {}), [key]: e.target.value } })
                }}
                className="flex-1 px-2 py-1.5 rounded border font-mono text-sm"
                style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                placeholder="{variable} or value"
              />
              <button
                onClick={() => {
                  const newArgs = { ...data.args }
                  delete newArgs[key]
                  onChange({ ...data, args: Object.keys(newArgs).length > 0 ? newArgs : undefined })
                }}
                className="p-1.5 text-red-400 hover:text-red-300 hover:bg-red-500/20 rounded"
              >
                <Trash2 size={14} />
              </button>
            </div>
          ))}
          {(!data.args || Object.keys(data.args).length === 0) && (
            <p className="text-xs italic py-2" style={{ color: 'var(--text-muted)' }}>
              No arguments. Click "Add" to add key-value pairs.
            </p>
          )}
        </div>
      </div>
    </div>
  )
}

/**
 * Output Node Form - with user_message array editor
 */
function OutputNodeForm({ data, onChange, theme }) {
  const userMessage = data.user_message || []
  
  const handleAddItem = () => {
    onChange({ ...data, user_message: [...userMessage, ''] })
  }
  
  const handleRemoveItem = (index) => {
    const newItems = userMessage.filter((_, i) => i !== index)
    onChange({ ...data, user_message: newItems })
  }
  
  const handleItemChange = (index, value) => {
    const newItems = [...userMessage]
    newItems[index] = value
    onChange({ ...data, user_message: newItems })
  }
  
  return (
    <div className="flex gap-6 h-full">
      {/* Left column - Info */}
      <div className="w-64 space-y-4">
        <div className="flex items-center gap-3" style={{ color: 'var(--text-muted)' }}>
          <MessageSquare size={20} className="opacity-50" />
          <div>
            <p className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>Output Display</p>
            <p className="text-xs">Configure what to show</p>
          </div>
        </div>
        <div className="text-xs space-y-2 p-3 rounded" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
          <p><strong>Tips:</strong></p>
          <p>• Add literal text strings</p>
          <p>• Reference state variables by name</p>
          <p>• Items are joined with spaces</p>
        </div>
      </div>
      
      {/* Right column - User Message Editor */}
      <div className="flex-1">
        <div className="flex justify-between items-center mb-2">
          <label className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>
            User Message
          </label>
          <button
            type="button"
            onClick={handleAddItem}
            className="flex items-center gap-1 px-2 py-1 rounded text-xs transition-colors hover:opacity-80"
            style={{ background: 'var(--accent-color)', color: 'white' }}
          >
            <Plus size={12} />
            Add Item
          </button>
        </div>
        
        {userMessage.length === 0 ? (
          <div 
            className="text-sm p-4 rounded border border-dashed text-center"
            style={{ borderColor: 'var(--border-color)', color: 'var(--text-muted)' }}
          >
            No items yet. Click "Add Item" to add a message part.
          </div>
        ) : (
          <div className="space-y-2 max-h-48 overflow-y-auto">
            {userMessage.map((item, index) => (
              <div key={index} className="flex items-center gap-2">
                <span 
                  className="text-xs px-2 py-1 rounded" 
                  style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}
                >
                  {index + 1}
                </span>
                <input
                  type="text"
                  value={item}
                  onChange={(e) => handleItemChange(index, e.target.value)}
                  className="flex-1 px-3 py-2 rounded border text-sm"
                  style={{ 
                    background: 'var(--bg-primary)', 
                    borderColor: 'var(--border-color)', 
                    color: 'var(--text-primary)' 
                  }}
                  placeholder='Text or variable name (e.g., "Result:" or answer)'
                />
                <button
                  type="button"
                  onClick={() => handleRemoveItem(index)}
                  className="p-1 rounded hover:bg-red-500/20 text-red-500"
                >
                  <Trash2 size={14} />
                </button>
              </div>
            ))}
          </div>
        )}
        
        <p className="text-xs mt-2" style={{ color: 'var(--text-muted)' }}>
          Enter text strings or state variable names. Variables will be resolved at runtime.
        </p>
      </div>
    </div>
  )
}

/**
 * Main Node Editor Component - Horizontal Bottom Layout
 */
export default function NodeEditor({ node, onSave, onClose, theme, availableTools = [], availableVariables = [], readOnly, onAIAssist }) {
  const [editedData, setEditedData] = useState({})
  const [nodeName, setNodeName] = useState('')
  
  // Initialize state when node changes
  useEffect(() => {
    if (node) {
      setNodeName(node.data?.label || node.id)
      setEditedData(node.data?.yaml || {})
    }
  }, [node])
  
  // Save and close - called when user clicks Done, X, or clicks outside
  const handleClose = useCallback(() => {
    if (node) {
      const nodeType = node.data?.nodeType || node.type
      const savedData = {
        ...editedData,
        name: nodeName,
        type: nodeType === 'updateState' ? 'update_state' : nodeType,
      }
      onSave(node.id, savedData)
    }
    onClose()
  }, [node, editedData, nodeName, onSave, onClose])
  
  if (!node) return null
  
  const nodeType = node.data?.nodeType || node.type
  const Icon = NODE_ICONS[nodeType] || Brain
  // Enforce stable purple color for all nodes as requested
  const color = '#7c3aed'
  
  // Render type-specific form
  const renderForm = () => {
    const props = { data: editedData, onChange: setEditedData, theme, availableVariables }
    
    switch (nodeType) {
      case 'update_state':
      case 'updateState':
        return <UpdateStateForm {...props} />
      case 'input':
        return <InputNodeForm {...props} />
      case 'llm':
        return <LlmNodeForm {...props} availableTools={availableTools} />
      case 'tool':
        return <ToolNodeForm {...props} availableTools={availableTools} />
      case 'output':
        return <OutputNodeForm {...props} />
      default:
        return <LlmNodeForm {...props} availableTools={availableTools} />
    }
  }
  
  return (
    <div 
      className="h-full flex flex-col overflow-hidden"
      style={{ background: 'var(--bg-secondary)' }}
    >
      {/* Header */}
      <div 
        className="flex items-center justify-between px-4 py-2 shrink-0"
        style={{ borderBottom: '1px solid var(--border-color)' }}
      >
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <div 
              className="w-7 h-7 rounded-lg flex items-center justify-center"
              style={{ background: color }}
            >
              <Icon size={14} className="text-white" />
            </div>
            <span className="font-semibold text-sm" style={{ color: 'var(--text-primary)' }}>
              {readOnly ? 'View Node' : 'Edit Node'}
            </span>
          </div>
          
          {/* Node Name */}
          <div className="flex items-center gap-2">
            <label className="text-xs" style={{ color: 'var(--text-muted)' }}>Name:</label>
            <input
              type="text"
              value={nodeName}
              onChange={(e) => setNodeName(e.target.value)}
              readOnly={readOnly}
              className="px-2 py-1 rounded border font-mono text-sm w-40"
              style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)', opacity: readOnly ? 0.7 : 1 }}
            />
          </div>
          
          {/* Type Badge */}
          <span 
            className="px-2 py-0.5 rounded text-xs font-medium text-white"
            style={{ background: color }}
          >
            {nodeType}
          </span>
          
          {/* Output Model - inline in header for non-LLM nodes (LLM has it in Output tab) */}
          {nodeType !== 'output' && nodeType !== 'llm' && (
            <div className="flex items-center gap-2 pl-4" style={{ borderLeft: '1px solid var(--border-color)' }}>
              <OutputModelEditor
                value={editedData.output_model}
                onChange={(newModel) => setEditedData({ ...editedData, output_model: newModel })}
                theme={theme}
                singleField={nodeType === 'input'}
              />
            </div>
          )}
        </div>
        
        {/* Actions */}
        <div className="flex items-center gap-2">
          {/* AI Assist - hidden in read-only mode */}
          {onAIAssist && !readOnly && (
            <button
              onClick={() => onAIAssist(node, nodeName, editedData)}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded text-sm font-medium bg-gradient-to-r from-purple-600 to-blue-600 hover:from-purple-500 hover:to-blue-500 text-white transition-all"
              title="Get AI suggestions for this node"
            >
              <Sparkles size={14} />
              AI Assist
            </button>
          )}
          <button
            onClick={handleClose}
            className="px-3 py-1.5 rounded text-sm font-medium text-white bg-purple-600 hover:bg-purple-700 transition-colors"
          >
            Done
          </button>
          <button
            onClick={handleClose}
            className="p-1 rounded hover:bg-gray-500/20 ml-2"
            style={{ color: 'var(--text-muted)' }}
          >
            <X size={18} />
          </button>
        </div>
      </div>
      
      {/* Content - Type-specific form */}
      <div className="flex-1 overflow-auto p-4">
        {renderForm()}
      </div>
    </div>
  )
}
