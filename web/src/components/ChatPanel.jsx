import { useState } from 'react'
import { Send, Brain, Wrench } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

export default function ChatPanel({ messages, onSendMessage, onStartRun, theme }) {
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
        {messages.length === 0 && onStartRun && (
          <div className="h-full flex flex-col items-center justify-center gap-4 text-center">
            <div className="p-4 rounded-full bg-purple-500/10 mb-2">
              <Brain size={32} className="text-purple-400" />
            </div>
            <h3 className="text-lg font-medium" style={{ color: 'var(--text-primary)' }}>
              Ready to Run
            </h3>
            <p className="text-sm max-w-xs mb-4" style={{ color: 'var(--text-muted)' }}>
              Start the agent execution to see the flow in action.
            </p>
            <button
              onClick={onStartRun}
              className="px-6 py-3 bg-gradient-to-r from-purple-600 to-blue-600 hover:from-purple-500 hover:to-blue-500 text-white font-medium rounded-xl shadow-lg transition-all hover:scale-105 flex items-center gap-2"
            >
              <Send size={18} />
              Start Execution
            </button>
          </div>
        )}
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
                  <div style={{ color: 'var(--text-primary)' }} className="markdown-body text-sm">
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>
                      {message.content}
                    </ReactMarkdown>
                  </div>
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
            {message.type === 'node' && (
              <div className="flex items-center justify-center my-2">
                <div className="px-3 py-1 rounded-full text-xs font-medium bg-purple-500/10 text-purple-400 border border-purple-500/20">
                  ⚡ Executing Node: {message.nodeName}
                </div>
              </div>
            )}
            {message.type === 'system' && (
              <div className="text-xs text-center my-1 italic" style={{ color: 'var(--text-muted)' }}>
                {message.content}
              </div>
            )}
            {message.type === 'error' && (
              <div className="p-3 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
                Error: {message.content}
              </div>
            )}
            {message.type === 'thinking' && (
              <div className="flex items-center gap-2 px-3 py-2 rounded-lg text-sm w-fit bg-yellow-500/20 text-yellow-400 border border-yellow-500/30">
                <Brain size={16} />
                <span>Thinking...</span>
              </div>
            )}
            {message.type === 'input_request' && message.options && (
               <div className="grid grid-cols-2 gap-2 mt-2">
                 {message.options.map((opt, i) => (
                   <button 
                     key={i}
                     onClick={() => onSendMessage(opt)}
                     className="px-3 py-2 text-sm bg-purple-500/20 hover:bg-purple-500/30 text-purple-300 border border-purple-500/30 rounded transition-colors text-left truncate"
                   >
                     {opt}
                   </button>
                 ))}
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
