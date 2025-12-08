import { useState, useEffect } from 'react'
import { X, Save, Edit3, Brain, Wrench, Settings, MessageSquare, Plus, Trash2, AlertCircle, Sparkles } from 'lucide-react'

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
 * Output Model Editor - key-value list for output_model field
 */
function OutputModelEditor({ value, onChange, theme }) {
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
      <div className="flex items-center justify-between">
        <label className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>
          Output Model
        </label>
        <button
          onClick={handleAdd}
          className="flex items-center gap-1 text-xs px-2 py-1 rounded bg-purple-600 text-white hover:bg-purple-700"
        >
          <Plus size={12} /> Add
        </button>
      </div>
      
      {entries.length === 0 ? (
        <div className="text-xs py-2" style={{ color: 'var(--text-muted)' }}>
          No fields. Add at least one.
        </div>
      ) : (
        <div className="space-y-1">
          {entries.map(([key, type]) => (
            <div key={key} className="flex items-center gap-1">
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
              <button
                onClick={() => handleRemove(key)}
                className="p-0.5 text-red-400 hover:text-red-300"
              >
                <Trash2 size={12} />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

/**
 * Update State Form - Horizontal layout
 */
function UpdateStateForm({ data, onChange, theme }) {
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
      </div>
      
      {/* Right column - Value */}
      <div className="flex-1">
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
      </div>
    </div>
  )
}

/**
 * Input Node Form - Horizontal layout
 */
function InputNodeForm({ data, onChange, theme }) {
  return (
    <div className="flex gap-6 h-full">
      {/* Left column - Options */}
      <div className="w-64 space-y-4">
        <div>
          <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
            Options (variable)
          </label>
          <input
            type="text"
            value={data.options ? `[${data.options.join(', ')}]` : ''}
            onChange={(e) => {
              const val = e.target.value.replace(/[\[\]]/g, '').split(',').map(s => s.trim()).filter(Boolean)
              onChange({ ...data, options: val.length > 0 ? val : undefined })
            }}
            className="w-full px-3 py-2 rounded border font-mono text-sm"
            style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
            placeholder="[variable]"
          />
        </div>
      </div>
      
      {/* Right column - Prompt */}
      <div className="flex-1">
        <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
          Prompt
        </label>
        <textarea
          value={data.prompt || ''}
          onChange={(e) => onChange({ ...data, prompt: e.target.value })}
          className="w-full h-32 px-3 py-2 rounded border font-mono text-sm resize-none"
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
function LlmNodeForm({ data, onChange, theme, availableTools = [] }) {
  const [activeTab, setActiveTab] = useState('prompts')
  
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
  
  const tabs = [
    { id: 'prompts', label: 'Prompts' },
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
      <div className="flex-1">
        {activeTab === 'prompts' && (
          <div className="flex gap-4 h-full">
            {/* System Prompt */}
            <div className="flex-1">
              <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
                System Prompt (optional)
              </label>
              <textarea
                value={data.system || ''}
                onChange={(e) => onChange({ ...data, system: e.target.value || undefined })}
                className="w-full h-32 px-3 py-2 rounded border font-mono text-sm resize-none"
                style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                placeholder="Enter system instructions..."
              />
            </div>
            
            {/* User Prompt */}
            <div className="flex-1">
              <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
                Prompt
              </label>
              <textarea
                value={data.prompt || ''}
                onChange={(e) => onChange({ ...data, prompt: e.target.value })}
                className="w-full h-32 px-3 py-2 rounded border font-mono text-sm resize-none"
                style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                placeholder="Enter the LLM prompt. Use {variable} for state references..."
              />
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
                  
                  {/* Selected tools */}
                  <div className="space-y-1 mb-2">
                    {currentTools.map((tool, idx) => {
                      const isInvalid = !toolNames.includes(tool)
                      return (
                        <div 
                          key={idx} 
                          className={`flex items-center justify-between px-3 py-1.5 rounded border ${isInvalid ? 'border-red-500 bg-red-500/10' : ''}`}
                          style={!isInvalid ? { background: 'var(--bg-primary)', borderColor: 'var(--border-color)' } : {}}
                        >
                          <div className="flex items-center gap-2">
                            {isInvalid && <AlertCircle size={14} className="text-red-400" />}
                            <span className="text-sm font-mono" style={{ color: isInvalid ? '#F87171' : 'var(--text-primary)' }}>
                              {tool}
                            </span>
                            {isInvalid && (
                              <span className="text-xs" style={{ color: 'var(--text-muted)' }}>(not found)</span>
                            )}
                          </div>
                          <button
                            onClick={() => handleRemoveTool(tool)}
                            className="p-1 text-red-400 hover:text-red-300"
                          >
                            <Trash2 size={12} />
                          </button>
                        </div>
                      )
                    })}
                  </div>
                  
                  {/* Dropdown to add new tool */}
                  <select
                    value=""
                    onChange={(e) => handleAddTool(e.target.value)}
                    className="w-full px-3 py-2 rounded border text-sm"
                    style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                  >
                    <option value="">+ Add a tool...</option>
                    {availableTools.map((tool) => (
                      <option 
                        key={tool.name} 
                        value={tool.name}
                        disabled={currentTools.includes(tool.name)}
                      >
                        {tool.name} ({tool.source})
                      </option>
                    ))}
                  </select>
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
          
          {/* Selected tools */}
          <div className="space-y-1 mb-2">
            {currentTools.map((tool, idx) => {
              const isInvalid = !toolNames.includes(tool)
              return (
                <div 
                  key={idx} 
                  className={`flex items-center justify-between px-3 py-1.5 rounded border ${isInvalid ? 'border-red-500 bg-red-500/10' : ''}`}
                  style={!isInvalid ? { background: 'var(--bg-primary)', borderColor: 'var(--border-color)' } : {}}
                >
                  <div className="flex items-center gap-2">
                    {isInvalid && <AlertCircle size={14} className="text-red-400" />}
                    <span className="text-sm font-mono" style={{ color: isInvalid ? '#F87171' : 'var(--text-primary)' }}>
                      {tool}
                    </span>
                    {isInvalid && (
                      <span className="text-xs" style={{ color: 'var(--text-muted)' }}>(not found)</span>
                    )}
                  </div>
                  <button
                    onClick={() => handleRemoveTool(tool)}
                    className="p-1 text-red-400 hover:text-red-300"
                  >
                    <Trash2 size={12} />
                  </button>
                </div>
              )
            })}
          </div>
          
          {/* Dropdown to add new tool */}
          <select
            value=""
            onChange={(e) => handleAddTool(e.target.value)}
            className="w-full px-3 py-2 rounded border text-sm"
            style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
          >
            <option value="">+ Add a tool...</option>
            {availableTools.map((tool) => (
              <option 
                key={tool.name} 
                value={tool.name}
                disabled={currentTools.includes(tool.name)}
              >
                {tool.name} ({tool.source})
              </option>
            ))}
          </select>
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
        <label className="text-sm font-medium block mb-1" style={{ color: 'var(--text-secondary)' }}>
          Args (JSON)
        </label>
        <textarea
          value={data.args ? JSON.stringify(data.args, null, 2) : ''}
          onChange={(e) => {
            try {
              const parsed = JSON.parse(e.target.value)
              onChange({ ...data, args: parsed })
            } catch {
              // Invalid JSON, don't update
            }
          }}
          className="w-full h-32 px-3 py-2 rounded border font-mono text-sm resize-none"
          style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
          placeholder='{"key": {"variable_name"}}'
        />
      </div>
    </div>
  )
}

/**
 * Output Node Form
 */
function OutputNodeForm({ data, onChange, theme }) {
  return (
    <div className="flex items-center gap-4" style={{ color: 'var(--text-muted)' }}>
      <MessageSquare size={24} className="opacity-50" />
      <div>
        <p className="text-sm">Output nodes display the final state.</p>
        <p className="text-xs">No additional configuration required.</p>
      </div>
    </div>
  )
}

/**
 * Main Node Editor Component - Horizontal Bottom Layout
 */
export default function NodeEditor({ node, onSave, onClose, theme, availableTools = [], onAIAssist }) {
  const [editedData, setEditedData] = useState({})
  const [nodeName, setNodeName] = useState('')
  
  // Initialize state when node changes
  useEffect(() => {
    if (node) {
      setNodeName(node.data?.label || node.id)
      setEditedData(node.data?.yaml || {})
    }
  }, [node])
  
  if (!node) return null
  
  const nodeType = node.data?.nodeType || node.type
  const Icon = NODE_ICONS[nodeType] || Brain
  const color = NODE_COLORS[nodeType] || '#6B46C1'
  
  const handleSave = () => {
    const savedData = {
      ...editedData,
      name: nodeName,
      type: nodeType === 'updateState' ? 'update_state' : nodeType,
    }
    onSave(node.id, savedData)
  }
  
  // Render type-specific form
  const renderForm = () => {
    const props = { data: editedData, onChange: setEditedData, theme }
    
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
              Edit Node
            </span>
          </div>
          
          {/* Node Name */}
          <div className="flex items-center gap-2">
            <label className="text-xs" style={{ color: 'var(--text-muted)' }}>Name:</label>
            <input
              type="text"
              value={nodeName}
              onChange={(e) => setNodeName(e.target.value)}
              className="px-2 py-1 rounded border font-mono text-sm w-40"
              style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
            />
          </div>
          
          {/* Type Badge */}
          <span 
            className="px-2 py-0.5 rounded text-xs font-medium text-white"
            style={{ background: color }}
          >
            {nodeType}
          </span>
          
          {/* Output Model - inline in header for compact layout */}
          {nodeType !== 'output' && (
            <div className="flex items-center gap-2 pl-4" style={{ borderLeft: '1px solid var(--border-color)' }}>
              <OutputModelEditor
                value={editedData.output_model}
                onChange={(newModel) => setEditedData({ ...editedData, output_model: newModel })}
                theme={theme}
              />
            </div>
          )}
        </div>
        
        {/* Actions */}
        <div className="flex items-center gap-2">
          {onAIAssist && (
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
            onClick={onClose}
            className="px-3 py-1.5 rounded text-sm font-medium transition-colors hover:bg-gray-500/20"
            style={{ color: 'var(--text-secondary)' }}
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded text-sm font-medium text-white bg-purple-600 hover:bg-purple-700 transition-colors"
          >
            <Save size={14} />
            Save
          </button>
          <button
            onClick={onClose}
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
