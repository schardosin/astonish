import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@synergycodes/overflow-ui/tokens.css'
import './index.css'
import App from './App.jsx'

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
