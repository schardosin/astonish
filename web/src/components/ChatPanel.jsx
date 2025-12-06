import { useState } from 'react'
import { Send, Brain, Wrench } from 'lucide-react'

export default function ChatPanel({ messages, onSendMessage, theme }) {
  const [input, setInput] = useState('')

  const handleSubmit = (e) => {
    e.preventDefault()
    if (input.trim()) {
      onSendMessage(input.trim())
      setInput('')
    }
  }

  return (
    <div className="flex flex-col h-full" style={{ background: 'var(--bg-secondary)' }}>
      {/* Header */}
      <div className="p-4" style={{ borderBottom: '1px solid var(--border-color)' }}>
        <h2 className="font-semibold" style={{ color: 'var(--text-primary)' }}>Chat</h2>
      </div>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {messages.map((message, index) => (
          <div key={index}>
            {message.type === 'agent' && (
              <div className="space-y-2">
                <div className="text-xs font-medium" style={{ color: 'var(--text-muted)' }}>Agent</div>
                <div 
                  className="p-4 rounded-lg max-w-[90%]"
                  style={{ 
                    background: theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'white',
                    border: `1px solid var(--border-color)` 
                  }}
                >
                  <p style={{ color: 'var(--text-primary)' }}>{message.content}</p>
                </div>
              </div>
            )}
            {message.type === 'user' && (
              <div className="flex justify-end">
                <div className="space-y-2">
                  <div className="text-xs font-medium text-right" style={{ color: 'var(--text-muted)' }}>User</div>
                  <div className="chat-bubble-user p-4 rounded-lg max-w-[90%]">
                    <p>{message.content}</p>
                  </div>
                </div>
              </div>
            )}
            {message.type === 'thinking' && (
              <div className="flex items-center gap-2 px-3 py-2 rounded-lg text-sm w-fit bg-yellow-500/20 text-yellow-400 border border-yellow-500/30">
                <Brain size={16} />
                <span>Thinking...</span>
              </div>
            )}
            {message.type === 'tool' && (
              <div className="flex items-center gap-2 px-3 py-2 rounded-lg text-sm w-fit bg-blue-500/20 text-blue-400 border border-blue-500/30">
                <Wrench size={16} />
                <span>Calling tool: {message.toolName}...</span>
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Input */}
      <form onSubmit={handleSubmit} className="p-4" style={{ borderTop: '1px solid var(--border-color)' }}>
        <div className="flex gap-3">
          <input
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="Disabled for Response..."
            className="flex-1 px-4 py-3 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
            style={{ 
              background: 'var(--bg-tertiary)', 
              color: 'var(--text-primary)',
              border: '1px solid var(--border-color)'
            }}
          />
          <button
            type="submit"
            className="px-4 py-3 bg-[#805AD5] hover:bg-[#6B46C1] text-white rounded-lg transition-colors"
          >
            <Send size={20} />
          </button>
        </div>
      </form>
    </div>
  )
}
