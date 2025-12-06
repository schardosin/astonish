import { useState, useCallback } from 'react'
import { ReactFlowProvider } from '@xyflow/react'
import Sidebar from './components/Sidebar'
import FlowCanvas from './components/FlowCanvas'
import ChatPanel from './components/ChatPanel'
import YamlDrawer from './components/YamlDrawer'
import Header from './components/Header'
import { useTheme } from './hooks/useTheme'
import './index.css'

// Mock data for agents
const mockAgents = [
  { id: 'github-pr-review', name: 'GitHub PR Review', description: 'Review pull requests' },
  { id: 'file-summarizer', name: 'File Summarizer', description: 'Summarize file contents' },
  { id: 'agent-listager', name: 'Agent Listager', description: 'List available agents' },
]

// Mock flow data
const mockNodes = [
  { id: 'start', type: 'start', position: { x: 250, y: 0 }, data: { label: 'START', description: 'Run time the owner a repository' } },
  { id: 'set_owner', type: 'input', position: { x: 250, y: 120 }, data: { label: 'set_owner', description: 'Set up in owner repository' } },
  { id: 'set_repo', type: 'input', position: { x: 250, y: 240 }, data: { label: 'set_repo', description: 'Set up the repo repository' } },
  { id: 'list_pull_requests', type: 'llm', position: { x: 250, y: 360 }, data: { label: 'list_pull_requests', description: 'Inr use list_pull_requests for your project.', isActive: true } },
  { id: 'end', type: 'end', position: { x: 250, y: 480 }, data: { label: 'END' } },
]

const mockEdges = [
  { id: 'e-start-owner', source: 'start', target: 'set_owner', animated: true },
  { id: 'e-owner-repo', source: 'set_owner', target: 'set_repo', animated: true },
  { id: 'e-repo-pr', source: 'set_repo', target: 'list_pull_requests', animated: true },
  { id: 'e-pr-end', source: 'list_pull_requests', target: 'end' },
]

const mockYaml = `description: GitHub PR Review Agent

nodes:
  - name: set_owner
    type: input
    prompt: "Enter the repository owner:"
    output_model:
      owner: str

  - name: set_repo
    type: input
    prompt: "Enter the repository name:"
    output_model:
      repo: str

  - name: list_pull_requests
    type: llm
    system: "You are a helpful assistant."
    prompt: "List pull requests for {owner}/{repo}"
    tools: true
    output_model:
      pull_requests: list

flow:
  - from: START
    to: set_owner
  - from: set_owner
    to: set_repo
  - from: set_repo
    to: list_pull_requests
  - from: list_pull_requests
    to: END
`

function App() {
  const { theme, toggleTheme } = useTheme()
  const [agents] = useState(mockAgents)
  const [selectedAgent, setSelectedAgent] = useState(mockAgents[0])
  const [nodes, setNodes] = useState(mockNodes)
  const [edges, setEdges] = useState(mockEdges)
  const [yamlContent, setYamlContent] = useState(mockYaml)
  const [showYaml, setShowYaml] = useState(false)
  const [isRunning, setIsRunning] = useState(false)
  const [chatMessages, setChatMessages] = useState([
    { type: 'agent', content: 'What is the first repository naom for tus for the repository. If you want to detexmine your repository?' },
  ])

  const handleAgentSelect = useCallback((agent) => {
    setSelectedAgent(agent)
    // In real app, would load agent's flow here
  }, [])

  const handleCreateNew = useCallback(() => {
    // Create new agent flow
    setSelectedAgent({ id: 'new', name: 'New Agent', description: '' })
    setNodes([
      { id: 'start', type: 'start', position: { x: 250, y: 0 }, data: { label: 'START' } },
      { id: 'end', type: 'end', position: { x: 250, y: 120 }, data: { label: 'END' } },
    ])
    setEdges([{ id: 'e-start-end', source: 'start', target: 'end' }])
  }, [])

  const handleRun = useCallback(() => {
    setIsRunning(true)
    // In real app, would start agent execution here
  }, [])

  const handleStopRun = useCallback(() => {
    setIsRunning(false)
  }, [])

  return (
    <ReactFlowProvider>
      <div className="flex h-screen" style={{ background: 'var(--bg-primary)' }}>
        {/* Sidebar */}
        <Sidebar
          agents={agents}
          selectedAgent={selectedAgent}
          onAgentSelect={handleAgentSelect}
          onCreateNew={handleCreateNew}
          theme={theme}
          onToggleTheme={toggleTheme}
        />

        {/* Main Content */}
        <div className="flex-1 flex flex-col">
          <Header
            agentName={selectedAgent?.name || 'Select Agent'}
            showYaml={showYaml}
            onToggleYaml={() => setShowYaml(!showYaml)}
            isRunning={isRunning}
            onRun={handleRun}
            onStop={handleStopRun}
            theme={theme}
          />

          <div className="flex-1 flex overflow-hidden">
            {/* Flow Canvas */}
            <div className={`${isRunning ? 'w-1/2' : 'flex-1'} transition-all duration-300`}>
              <FlowCanvas
                nodes={nodes}
                edges={edges}
                onNodesChange={setNodes}
                onEdgesChange={setEdges}
                isRunning={isRunning}
                theme={theme}
              />
            </div>

            {/* Chat Panel (visible when running) */}
            {isRunning && (
              <div className="w-1/2" style={{ borderLeft: '1px solid var(--border-color)' }}>
                <ChatPanel
                  messages={chatMessages}
                  onSendMessage={(msg) => setChatMessages([...chatMessages, { type: 'user', content: msg }])}
                  theme={theme}
                />
              </div>
            )}

            {/* YAML Drawer */}
            {showYaml && !isRunning && (
              <div className="w-96" style={{ borderLeft: '1px solid var(--border-color)' }}>
                <YamlDrawer
                  content={yamlContent}
                  onChange={setYamlContent}
                  onClose={() => setShowYaml(false)}
                  theme={theme}
                />
              </div>
            )}
          </div>
        </div>
      </div>
    </ReactFlowProvider>
  )
}

export default App
