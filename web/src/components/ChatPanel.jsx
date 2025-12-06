import { useState } from 'react'
import { Send, Brain, Wrench } from 'lucide-react'

export default function ChatPanel({ messages, onSendMessage }) {
  const [input, setInput] = useState('')

  const handleSubmit = (e) => {
    e.preventDefault()
    if (input.trim()) {
      onSendMessage(input.trim())
      setInput('')
    }
  }

  return (
    <div className="flex flex-col h-full bg-white">
      {/* Header */}
      <div className="p-4 border-b border-gray-200">
        <h2 className="font-semibold text-gray-800">Chat</h2>
      </div>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {messages.map((message, index) => (
          <div key={index}>
            {message.type === 'agent' && (
              <div className="space-y-2">
                <div className="text-xs font-medium text-gray-500">Agent</div>
                <div className="chat-bubble-agent p-4 rounded-lg max-w-[90%]">
                  <p className="text-gray-700">{message.content}</p>
                </div>
              </div>
            )}
            {message.type === 'user' && (
              <div className="flex justify-end">
                <div className="space-y-2">
                  <div className="text-xs font-medium text-gray-500 text-right">User</div>
                  <div className="chat-bubble-user p-4 rounded-lg max-w-[90%]">
                    <p>{message.content}</p>
                  </div>
                </div>
              </div>
            )}
            {message.type === 'thinking' && (
              <div className="flex items-center gap-2 thinking-indicator px-3 py-2 rounded-lg text-sm w-fit">
                <Brain size={16} />
                <span>Thinking...</span>
              </div>
            )}
            {message.type === 'tool' && (
              <div className="flex items-center gap-2 tool-indicator px-3 py-2 rounded-lg text-sm w-fit">
                <Wrench size={16} />
                <span>Calling tool: {message.toolName}...</span>
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Input */}
      <form onSubmit={handleSubmit} className="p-4 border-t border-gray-200">
        <div className="flex gap-3">
          <input
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="Disabled for Response..."
            className="flex-1 px-4 py-3 border border-gray-200 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent"
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
