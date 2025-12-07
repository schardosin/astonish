import { useState, useRef, useEffect } from 'react'
import { MessageSquare, Send, X, Sparkles, Loader2, Check, Eye } from 'lucide-react'

// API function to chat with AI
async function sendChatMessage(message, context, currentYaml, selectedNodes, history) {
  const response = await fetch('/api/ai/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      message,
      context,
      currentYaml,
      selectedNodes,
      history,
    }),
  })
  return response.json()
}

export default function AIChatPanel({ 
  isOpen, 
  onClose, 
  context = 'create_flow',
  currentYaml = '',
  selectedNodes = [],
  onApplyYaml,
  onPreviewYaml,
}) {
  const [messages, setMessages] = useState([])
  const [input, setInput] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [pendingYaml, setPendingYaml] = useState(null)
  const messagesEndRef = useRef(null)
  const inputRef = useRef(null)

  // Scroll to bottom when new messages arrive
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  // Focus input when panel opens
  useEffect(() => {
    if (isOpen) {
      inputRef.current?.focus()
    }
  }, [isOpen])

  // Reset when context changes
  useEffect(() => {
    setMessages([])
    setPendingYaml(null)
  }, [context])

  const handleSend = async () => {
    if (!input.trim() || isLoading) return

    const userMessage = input.trim()
    setInput('')
    setMessages(prev => [...prev, { role: 'user', content: userMessage }])
    setIsLoading(true)

    try {
      // Build history for API (exclude current message)
      const history = messages.map(m => ({ role: m.role, content: m.content }))
      
      const response = await sendChatMessage(
        userMessage,
        context,
        currentYaml,
        selectedNodes,
        history
      )

      if (response.error) {
        setMessages(prev => [...prev, { 
          role: 'assistant', 
          content: `Error: ${response.error}`,
          isError: true 
        }])
      } else {
        setMessages(prev => [...prev, { 
          role: 'assistant', 
          content: response.message,
          proposedYaml: response.proposedYaml,
          action: response.action,
        }])
        
        if (response.proposedYaml) {
          setPendingYaml(response.proposedYaml)
        }
      }
    } catch (err) {
      setMessages(prev => [...prev, { 
        role: 'assistant', 
        content: `Network error: ${err.message}`,
        isError: true 
      }])
    } finally {
      setIsLoading(false)
    }
  }

  const handleKeyDown = (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  const handlePreview = () => {
    if (pendingYaml && onPreviewYaml) {
      onPreviewYaml(pendingYaml)
    }
  }

  const handleApply = () => {
    if (pendingYaml && onApplyYaml) {
      onApplyYaml(pendingYaml)
      setPendingYaml(null)
      setMessages(prev => [...prev, { 
        role: 'system', 
        content: '✓ Changes applied successfully!' 
      }])
    }
  }

  const getContextTitle = () => {
    switch (context) {
      case 'create_flow': return 'Create Flow'
      case 'modify_nodes': return 'Modify Nodes'
      case 'node_config': return 'Node Assistant'
      default: return 'AI Assistant'
    }
  }

  const getPlaceholder = () => {
    switch (context) {
      case 'create_flow': return 'Describe the flow you want to create...'
      case 'modify_nodes': return 'What changes do you want to make?'
      case 'node_config': return 'How can I help with this node?'
      default: return 'Ask me anything about your flow...'
    }
  }

  if (!isOpen) return null

  return (
    <div className="fixed bottom-4 right-4 w-96 h-[500px] bg-[var(--bg-secondary)] border border-[var(--border-color)] rounded-lg shadow-2xl flex flex-col z-50">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-[var(--border-color)] bg-gradient-to-r from-purple-600/20 to-blue-600/20">
        <div className="flex items-center gap-2">
          <Sparkles size={18} className="text-purple-400" />
          <span className="font-semibold text-[var(--text-primary)]">{getContextTitle()}</span>
        </div>
        <button 
          onClick={onClose}
          className="p-1 hover:bg-white/10 rounded transition-colors"
        >
          <X size={18} className="text-[var(--text-secondary)]" />
        </button>
      </div>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {messages.length === 0 && (
          <div className="text-center text-[var(--text-secondary)] text-sm py-8">
            <Sparkles size={32} className="mx-auto mb-3 text-purple-400 opacity-50" />
            <p>Start a conversation to get help</p>
            <p className="text-xs mt-2 opacity-70">I can help you design and build flows</p>
          </div>
        )}
        
        {messages.map((msg, idx) => (
          <div 
            key={idx} 
            className={`flex ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}
          >
            <div 
              className={`max-w-[85%] px-3 py-2 rounded-lg text-sm ${
                msg.role === 'user' 
                  ? 'bg-purple-600 text-white' 
                  : msg.role === 'system'
                  ? 'bg-green-600/20 text-green-400 text-center w-full'
                  : msg.isError
                  ? 'bg-red-600/20 text-red-400'
                  : 'bg-[var(--bg-primary)] text-[var(--text-primary)]'
              }`}
            >
              <div className="whitespace-pre-wrap break-words">
                {/* Render message content, removing YAML blocks for cleaner display */}
                {msg.content.split('```yaml')[0].trim() || msg.content}
              </div>
              
              {/* Show YAML indicator if present */}
              {msg.proposedYaml && (
                <div className="mt-2 pt-2 border-t border-white/10 text-xs flex items-center gap-1 text-purple-300">
                  <Check size={12} />
                  YAML generated - use buttons below to preview/apply
                </div>
              )}
            </div>
          </div>
        ))}
        
        {isLoading && (
          <div className="flex justify-start">
            <div className="bg-[var(--bg-primary)] px-3 py-2 rounded-lg">
              <Loader2 size={16} className="animate-spin text-purple-400" />
            </div>
          </div>
        )}
        
        <div ref={messagesEndRef} />
      </div>

      {/* Pending YAML Actions */}
      {pendingYaml && (
        <div className="px-4 py-2 border-t border-[var(--border-color)] bg-purple-600/10 flex items-center justify-between">
          <span className="text-sm text-purple-300">YAML ready</span>
          <div className="flex gap-2">
            <button
              onClick={handlePreview}
              className="flex items-center gap-1 px-3 py-1 text-xs bg-blue-600/20 hover:bg-blue-600/30 text-blue-400 rounded transition-colors"
            >
              <Eye size={12} />
              Preview
            </button>
            <button
              onClick={handleApply}
              className="flex items-center gap-1 px-3 py-1 text-xs bg-green-600/20 hover:bg-green-600/30 text-green-400 rounded transition-colors"
            >
              <Check size={12} />
              Apply
            </button>
          </div>
        </div>
      )}

      {/* Input */}
      <div className="p-3 border-t border-[var(--border-color)]">
        <div className="flex gap-2">
          <textarea
            ref={inputRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={getPlaceholder()}
            rows={1}
            className="flex-1 px-3 py-2 bg-[var(--bg-primary)] border border-[var(--border-color)] rounded-lg text-sm text-[var(--text-primary)] placeholder:text-[var(--text-secondary)] resize-none focus:outline-none focus:ring-2 focus:ring-purple-500/50"
            disabled={isLoading}
          />
          <button
            onClick={handleSend}
            disabled={!input.trim() || isLoading}
            className="px-3 py-2 bg-purple-600 hover:bg-purple-500 disabled:opacity-50 disabled:cursor-not-allowed rounded-lg transition-colors"
          >
            <Send size={16} className="text-white" />
          </button>
        </div>
      </div>
    </div>
  )
}
