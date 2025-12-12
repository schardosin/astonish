package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/tools"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// SimpleWebConfig contains configuration for the simple web launcher
type SimpleWebConfig struct {
	AgentConfig    *config.AgentConfig
	ProviderName   string
	ModelName      string
	SessionService session.Service
	Port           int
}

type chatServer struct {
	runner         *runner.Runner
	sessionService session.Service
	sessions       map[string]session.Session
	mu             sync.RWMutex
}

// RunSimpleWeb runs a simplified chat-only web interface
func RunSimpleWeb(ctx context.Context, cfg *SimpleWebConfig) error {
	// Initialize LLM
	llm, err := provider.GetProvider(ctx, cfg.ProviderName, cfg.ModelName, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize provider: %w", err)
	}

	// Initialize tools
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		return fmt.Errorf("failed to initialize internal tools: %w", err)
	}

	// Create Astonish agent
	astonishAgent := agent.NewAstonishAgent(cfg.AgentConfig, llm, internalTools)

	// Create ADK agent wrapper
	adkAgent, err := adkagent.New(adkagent.Config{
		Name:        "astonish_agent",
		Description: cfg.AgentConfig.Description,
		Run:         astonishAgent.Run,
	})
	if err != nil {
		return fmt.Errorf("failed to create ADK agent: %w", err)
	}

	// Create session service
	sessionService := cfg.SessionService
	if sessionService == nil {
		sessionService = session.InMemoryService()
	}

	// Create runner
	r, err := runner.New(runner.Config{
		AppName:        "astonish",
		Agent:          adkAgent,
		SessionService: sessionService,
	})
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}

	server := &chatServer{
		runner:         r,
		sessionService: sessionService,
		sessions:       make(map[string]session.Session),
	}

	router := mux.NewRouter()

	// API endpoints
	router.HandleFunc("/api/chat", server.handleChat).Methods("POST")
	router.HandleFunc("/", server.handleIndex).Methods("GET")

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Starting simple web UI on http://localhost:%d", cfg.Port)
	log.Printf("Open your browser to access the chat interface")

	return http.ListenAndServe(addr, router)
}

func (s *chatServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, htmlTemplate)
}

func (s *chatServer) handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message   string `json:"message"`
		SessionID string `json:"sessionId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Get or create session
	s.mu.Lock()
	sess, exists := s.sessions[req.SessionID]
	if !exists {
		resp, err := s.sessionService.Create(ctx, &session.CreateRequest{
			AppName: "astonish",
			UserID:  req.SessionID,
		})
		if err != nil {
			s.mu.Unlock()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sess = resp.Session
		s.sessions[req.SessionID] = sess
	}
	s.mu.Unlock()

	// Create user message
	var userMsg *genai.Content
	if req.Message != "" {
		userMsg = genai.NewContentFromText(req.Message, genai.RoleUser)
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Run agent and stream response
	for event, err := range s.runner.Run(ctx, req.SessionID, sess.ID(), userMsg, adkagent.RunConfig{}) {
		if err != nil {
			fmt.Fprintf(w, "data: {\"error\": %q}\n\n", err.Error())
			flusher.Flush()
			return
		}

		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.Text != "" {
					data, _ := json.Marshal(map[string]string{"text": part.Text})
					fmt.Fprintf(w, "data: %s\n\n", data)
					flusher.Flush()
				}
			}
		}
	}

	// Send completion signal
	fmt.Fprintf(w, "data: {\"done\": true}\n\n")
	flusher.Flush()
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Astonish Chat</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            background: linear-gradient(135deg, #0f0f1e 0%, #1a1a2e 100%);
            color: #e8eaed;
            height: 100vh;
            display: flex;
            flex-direction: column;
            overflow: hidden;
        }
        
        .header {
            background: rgba(26, 26, 46, 0.95);
            backdrop-filter: blur(10px);
            padding: 1rem 2rem;
            border-bottom: 1px solid rgba(255, 255, 255, 0.1);
            display: flex;
            align-items: center;
            gap: 1rem;
            box-shadow: 0 2px 10px rgba(0, 0, 0, 0.3);
        }
        
        .logo {
            width: 40px;
            height: 40px;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        
        .logo svg {
            width: 100%;
            height: 100%;
            filter: drop-shadow(0 4px 12px rgba(102, 126, 234, 0.4));
        }
        
        .header h1 {
            font-size: 1.5rem;
            font-weight: 600;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            background-clip: text;
        }
        
        .chat-container {
            flex: 1;
            display: flex;
            flex-direction: column;
            max-width: 1000px;
            width: 100%;
            margin: 0 auto;
            padding: 2rem;
            overflow: hidden;
        }
        
        .messages {
            flex: 1;
            overflow-y: auto;
            margin-bottom: 1.5rem;
            display: flex;
            flex-direction: column;
            gap: 1.25rem;
            padding-right: 0.5rem;
        }
        
        .messages::-webkit-scrollbar {
            width: 6px;
        }
        
        .messages::-webkit-scrollbar-track {
            background: rgba(255, 255, 255, 0.05);
            border-radius: 3px;
        }
        
        .messages::-webkit-scrollbar-thumb {
            background: rgba(102, 126, 234, 0.5);
            border-radius: 3px;
        }
        
        .messages::-webkit-scrollbar-thumb:hover {
            background: rgba(102, 126, 234, 0.7);
        }
        
        .message {
            padding: 1.25rem 1.5rem;
            border-radius: 16px;
            max-width: 75%;
            word-wrap: break-word;
            line-height: 1.6;
            animation: slideIn 0.3s ease-out;
            box-shadow: 0 2px 8px rgba(0, 0, 0, 0.2);
        }
        
        @keyframes slideIn {
            from {
                opacity: 0;
                transform: translateY(10px);
            }
            to {
                opacity: 1;
                transform: translateY(0);
            }
        }
        
        .message.user {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            align-self: flex-end;
            margin-left: auto;
            color: white;
            border-bottom-right-radius: 4px;
        }
        
        .message.agent {
            background: rgba(255, 255, 255, 0.08);
            backdrop-filter: blur(10px);
            align-self: flex-start;
            border: 1px solid rgba(255, 255, 255, 0.1);
            border-bottom-left-radius: 4px;
        }
        
        .message.agent pre {
            background: rgba(0, 0, 0, 0.3);
            padding: 0.75rem;
            border-radius: 8px;
            overflow-x: auto;
            margin: 0.5rem 0;
            border: 1px solid rgba(255, 255, 255, 0.1);
        }
        
        .input-container {
            display: flex;
            gap: 0.75rem;
            padding: 1.25rem;
            background: rgba(255, 255, 255, 0.08);
            backdrop-filter: blur(10px);
            border-radius: 20px;
            border: 1px solid rgba(255, 255, 255, 0.1);
            box-shadow: 0 4px 20px rgba(0, 0, 0, 0.3);
        }
        
        .input-container input {
            flex: 1;
            padding: 0.875rem 1.25rem;
            border: 1px solid rgba(255, 255, 255, 0.15);
            border-radius: 14px;
            background: rgba(255, 255, 255, 0.05);
            color: #e8eaed;
            font-size: 1rem;
            transition: all 0.2s ease;
        }
        
        .input-container input:focus {
            outline: none;
            border-color: #667eea;
            background: rgba(255, 255, 255, 0.08);
            box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.2);
        }
        
        .input-container input::placeholder {
            color: rgba(232, 234, 237, 0.5);
        }
        
        .input-container button {
            padding: 0.875rem 2rem;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            border: none;
            border-radius: 14px;
            cursor: pointer;
            font-size: 1rem;
            font-weight: 600;
            transition: all 0.2s ease;
            box-shadow: 0 4px 12px rgba(102, 126, 234, 0.4);
        }
        
        .input-container button:hover {
            transform: translateY(-2px);
            box-shadow: 0 6px 16px rgba(102, 126, 234, 0.5);
        }
        
        .input-container button:active {
            transform: translateY(0);
        }
        
        .input-container button:disabled {
            background: rgba(255, 255, 255, 0.1);
            cursor: not-allowed;
            transform: none;
            box-shadow: none;
        }
        
        .loading {
            display: none;
            padding: 1rem;
            text-align: center;
            color: rgba(232, 234, 237, 0.6);
            font-size: 0.9rem;
        }
        
        .loading.active {
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 0.5rem;
        }
        
        .loading::before {
            content: '';
            width: 16px;
            height: 16px;
            border: 2px solid rgba(102, 126, 234, 0.3);
            border-top-color: #667eea;
            border-radius: 50%;
            animation: spin 0.8s linear infinite;
        }
        
        @keyframes spin {
            to { transform: rotate(360deg); }
        }
    </style>
</head>
<body>
    <div class="header">
        <div class="logo">
            <svg xmlns="http://www.w3.org/2000/svg" version="1.1" viewBox="0.00 0.00 335.00 335.00">
                <g stroke-width="2.00" fill="none" stroke-linecap="butt">
                    <path stroke="#734bb4" vector-effect="non-scaling-stroke" d="M 262.46 334.70 Q 261.84 334.28 261.58 333.66 C 251.75 311.03 219.34 303.79 197.77 302.30 Q 169.69 300.37 139.05 302.32 C 120.42 303.50 97.77 308.82 84.18 321.18 Q 78.14 326.67 74.99 333.75 Q 74.76 334.28 74.33 334.70"/>
                    <path stroke="#734bb4" vector-effect="non-scaling-stroke" d="M 186.24 36.13 A 17.47 17.47 0.0 0 0 168.77 18.66 A 17.47 17.47 0.0 0 0 151.30 36.13 A 17.47 17.47 0.0 0 0 168.77 53.60 A 17.47 17.47 0.0 0 0 186.24 36.13"/>
                    <path stroke="#734bb4" vector-effect="non-scaling-stroke" d="M 168.89 275.57 Q 173.55 275.56 208.70 275.43 Q 218.71 275.40 222.75 274.62 C 251.26 269.12 272.27 240.60 272.16 212.01 Q 272.04 181.67 272.16 174.32 Q 272.38 160.55 270.56 152.38 C 265.21 128.35 247.07 109.63 222.92 104.06 Q 215.61 102.37 196.58 102.35 Q 182.48 102.34 168.44 102.37 Q 154.40 102.41 140.30 102.50 Q 121.27 102.62 113.97 104.35 C 89.85 110.04 71.81 128.86 66.58 152.92 Q 64.81 161.10 65.10 174.86 Q 65.26 182.21 65.30 212.55 C 65.34 241.14 86.50 269.55 115.04 274.90 Q 119.08 275.66 129.09 275.64 Q 164.24 275.58 168.89 275.57"/>
                    <path stroke="#734bb4" vector-effect="non-scaling-stroke" d="M 50.50 213.05 L 50.42 164.83 A 0.04 0.04 0.0 0 0 50.38 164.79 L 50.29 164.79 A 13.92 6.95 89.9 0 0 43.36 178.73 L 43.40 199.19 A 13.92 6.95 89.9 0 0 50.37 213.09 L 50.46 213.09 A 0.04 0.04 0.0 0 0 50.50 213.05"/>
                    <path stroke="#734bb4" vector-effect="non-scaling-stroke" d="M 287.14 165.68 L 287.14 212.32 A 0.12 0.12 0.0 0 0 287.26 212.44 L 287.28 212.44 A 13.52 6.38 -90.0 0 0 293.66 198.92 L 293.66 179.08 A 13.52 6.38 90.0 0 0 287.28 165.56 L 287.26 165.56 A 0.12 0.12 0.0 0 0 287.14 165.68"/>
                    <path stroke="#734bb4" vector-effect="non-scaling-stroke" d="M 122.9677 200.7387 A 28.25 27.50 86.9 0 0 148.8998 171.0428 A 28.25 27.50 86.9 0 0 119.9123 144.3213 A 28.25 27.50 86.9 0 0 93.9802 174.0172 A 28.25 27.50 86.9 0 0 122.9677 200.7387"/>
                    <path stroke="#734bb4" vector-effect="non-scaling-stroke" d="M 211.9690 200.6974 A 28.19 27.73 93.5 0 0 241.3683 174.2529 A 28.19 27.73 93.5 0 0 215.4110 144.4226 A 28.19 27.73 93.5 0 0 186.0117 170.8671 A 28.19 27.73 93.5 0 0 211.9690 200.6974"/>
                    <path stroke="#734bb4" vector-effect="non-scaling-stroke" d="M 168.16 249.07 C 177.86 249.08 187.51 246.91 196.03 242.13 C 202.52 238.48 212.13 230.38 206.92 222.49 A 3.54 3.52 -87.7 0 0 206.14 221.64 Q 202.50 218.71 198.39 220.17 Q 197.01 220.66 193.52 224.38 C 187.30 230.98 177.29 233.79 168.18 233.78 C 159.07 233.77 149.07 230.92 142.87 224.30 Q 139.39 220.57 138.01 220.08 Q 133.90 218.61 130.25 221.53 A 3.54 3.52 87.9 0 0 129.47 222.37 C 124.24 230.25 133.82 238.38 140.30 242.04 C 148.81 246.86 158.45 249.05 168.16 249.07"/>
                </g>
                <path fill="#342268" d="M 274.60 335.00 L 264.05 335.00 Q 263.38 334.70 262.46 334.70 Q 261.84 334.28 261.58 333.66 C 251.75 311.03 219.34 303.79 197.77 302.30 Q 169.69 300.37 139.05 302.32 C 120.42 303.50 97.77 308.82 84.18 321.18 Q 78.14 326.67 74.99 333.75 Q 74.76 334.28 74.33 334.70 Q 73.33 334.67 72.59 335.00 L 67.27 335.00 Q 65.77 334.65 64.33 335.00 L 62.82 335.00 C 60.23 334.44 60.50 332.04 60.52 329.53 A 3.70 3.57 54.6 0 1 60.68 328.46 Q 62.24 323.34 64.71 320.19 C 71.56 311.46 79.72 305.89 90.71 300.97 Q 100.72 296.49 115.65 293.39 C 117.81 292.94 119.19 292.25 121.52 292.62 A 0.39 0.38 -26.7 0 0 121.76 291.90 C 119.16 290.59 116.08 290.61 113.76 290.09 Q 99.84 287.00 87.34 278.93 C 82.25 275.64 79.11 272.68 74.48 268.09 Q 60.78 254.51 55.03 235.23 Q 54.63 233.89 54.37 232.01 A 1.68 1.67 86.7 0 0 52.76 230.57 Q 51.06 230.51 49.67 230.09 Q 32.43 224.83 28.50 206.99 A 0.24 0.08 63.4 0 1 28.49 206.92 L 28.49 171.31 A 1.01 0.86 -40.3 0 1 28.54 171.01 C 29.41 168.49 29.57 166.32 30.97 163.61 Q 38.01 149.94 53.23 147.27 A 1.17 1.15 2.3 0 0 54.15 146.43 C 61.75 117.90 82.64 97.72 111.24 90.56 C 115.00 89.62 118.47 89.58 121.97 88.78 A 10.28 10.19 38.4 0 1 124.22 88.53 L 159.80 88.53 A 0.78 0.77 90.0 0 0 160.57 87.75 L 160.57 68.27 A 1.16 1.15 -83.8 0 0 159.67 67.14 C 148.23 64.59 139.45 54.32 136.75 43.31 Q 136.24 41.25 136.41 34.39 Q 136.54 29.07 137.75 25.59 Q 142.83 10.90 158.01 5.55 Q 164.57 3.24 171.28 3.56 A 3.45 3.42 -34.9 0 1 172.14 3.71 C 174.68 4.46 176.63 4.45 179.15 5.42 Q 200.33 13.64 201.32 37.21 C 201.39 38.89 200.72 41.05 200.41 42.79 C 198.39 54.36 189.03 63.63 178.12 67.25 A 1.24 1.24 0.0 0 0 177.27 68.43 L 177.27 87.35 A 1.18 1.18 0.0 0 0 178.45 88.53 Q 202.53 88.51 210.02 88.51 C 213.59 88.51 217.40 89.25 220.74 89.82 Q 236.85 92.58 249.15 100.11 C 254.87 103.61 261.65 108.81 266.38 114.38 Q 278.54 128.69 283.66 146.88 A 0.82 0.82 0.0 0 0 284.38 147.48 Q 287.62 147.81 290.32 148.99 Q 302.66 154.35 307.04 166.60 Q 308.50 170.68 308.50 176.99 Q 308.50 196.23 308.38 207.28 A 4.24 4.08 -39.2 0 1 308.26 208.27 Q 304.07 225.58 287.12 230.47 A 1.86 1.83 -50.8 0 1 286.49 230.54 L 284.37 230.42 A 0.77 0.77 0.0 0 0 283.58 230.99 Q 279.10 248.61 267.66 262.68 Q 266.01 264.71 260.12 270.46 C 249.74 280.60 236.30 287.53 222.35 290.46 A 3.84 3.67 36.9 0 1 221.68 290.54 Q 218.79 290.62 215.80 291.71 A 0.33 0.33 0.0 0 0 215.85 292.35 Q 222.76 293.65 231.46 295.85 Q 250.14 300.58 265.49 313.23 Q 273.20 319.59 275.39 327.63 Q 276.51 331.73 274.60 335.00 Z M 186.24 36.13 A 17.47 17.47 0.0 0 0 168.77 18.66 A 17.47 17.47 0.0 0 0 151.30 36.13 A 17.47 17.47 0.0 0 0 168.77 53.60 A 17.47 17.47 0.0 0 0 186.24 36.13 Z M 168.89 275.57 Q 173.55 275.56 208.70 275.43 Q 218.71 275.40 222.75 274.62 C 251.26 269.12 272.27 240.60 272.16 212.01 Q 272.04 181.67 272.16 174.32 Q 272.38 160.55 270.56 152.38 C 265.21 128.35 247.07 109.63 222.92 104.06 Q 215.61 102.37 196.58 102.35 Q 182.48 102.34 168.44 102.37 Q 154.40 102.41 140.30 102.50 Q 121.27 102.62 113.97 104.35 C 89.85 110.04 71.81 128.86 66.58 152.92 Q 64.81 161.10 65.10 174.86 Q 65.26 182.21 65.30 212.55 C 65.34 241.14 86.50 269.55 115.04 274.90 Q 119.08 275.66 129.09 275.64 Q 164.24 275.58 168.89 275.57 Z M 50.50 213.05 L 50.42 164.83 A 0.04 0.04 0.0 0 0 50.38 164.79 L 50.29 164.79 A 13.92 6.95 89.9 0 0 43.36 178.73 L 43.40 199.19 A 13.92 6.95 89.9 0 0 50.37 213.09 L 50.46 213.09 A 0.04 0.04 0.0 0 0 50.50 213.05 Z M 287.14 165.68 L 287.14 212.32 A 0.12 0.12 0.0 0 0 287.26 212.44 L 287.28 212.44 A 13.52 6.38 -90.0 0 0 293.66 198.92 L 293.66 179.08 A 13.52 6.38 90.0 0 0 287.28 165.56 L 287.26 165.56 A 0.12 0.12 0.0 0 0 287.14 165.68 Z"/>
                <circle fill="#b173ff" cx="168.77" cy="36.13" r="17.47"/>
                <path fill="#b173ff" d="M 168.44 102.37 Q 182.48 102.34 196.58 102.35 Q 215.61 102.37 222.92 104.06 C 247.07 109.63 265.21 128.35 270.56 152.38 Q 272.38 160.55 272.16 174.32 Q 272.04 181.67 272.16 212.01 C 272.27 240.60 251.26 269.12 222.75 274.62 Q 218.71 275.40 208.70 275.43 Q 173.55 275.56 168.89 275.57 Q 164.24 275.58 129.09 275.64 Q 119.08 275.66 115.04 274.90 C 86.50 269.55 65.34 241.14 65.30 212.55 Q 65.26 182.21 65.10 174.86 Q 64.81 161.10 66.58 152.92 C 71.81 128.86 89.85 110.04 113.97 104.35 Q 121.27 102.62 140.30 102.50 Q 154.40 102.41 168.44 102.37 Z M 122.9677 200.7387 A 28.25 27.50 86.9 0 0 148.8998 171.0428 A 28.25 27.50 86.9 0 0 119.9123 144.3213 A 28.25 27.50 86.9 0 0 93.9802 174.0172 A 28.25 27.50 86.9 0 0 122.9677 200.7387 Z M 211.9690 200.6974 A 28.19 27.73 93.5 0 0 241.3683 174.2529 A 28.19 27.73 93.5 0 0 215.4110 144.4226 A 28.19 27.73 93.5 0 0 186.0117 170.8671 A 28.19 27.73 93.5 0 0 211.9690 200.6974 Z M 168.16 249.07 C 177.86 249.08 187.51 246.91 196.03 242.13 C 202.52 238.48 212.13 230.38 206.92 222.49 A 3.54 3.52 -87.7 0 0 206.14 221.64 Q 202.50 218.71 198.39 220.17 Q 197.01 220.66 193.52 224.38 C 187.30 230.98 177.29 233.79 168.18 233.78 C 159.07 233.77 149.07 230.92 142.87 224.30 Q 139.39 220.57 138.01 220.08 Q 133.90 218.61 130.25 221.53 A 3.54 3.52 87.9 0 0 129.47 222.37 C 124.24 230.25 133.82 238.38 140.30 242.04 C 148.81 246.86 158.45 249.05 168.16 249.07 Z"/>
                <ellipse fill="#342268" cx="0.00" cy="0.00" transform="translate(121.44,172.53) rotate(86.9)" rx="28.25" ry="27.50"/>
                <ellipse fill="#342268" cx="0.00" cy="0.00" transform="translate(213.69,172.56) rotate(93.5)" rx="28.19" ry="27.73"/>
                <path fill="#b173ff" d="M 50.50 213.05 A 0.04 0.04 0.0 0 1 50.46 213.09 L 50.37 213.09 A 13.92 6.95 89.9 0 1 43.40 199.19 L 43.36 178.73 A 13.92 6.95 89.9 0 1 50.29 164.79 L 50.38 164.79 A 0.04 0.04 0.0 0 1 50.42 164.83 L 50.50 213.05 Z"/>
                <path fill="#b173ff" d="M 287.14 165.68 A 0.12 0.12 0.0 0 1 287.26 165.56 L 287.28 165.56 A 13.52 6.38 90.0 0 1 293.66 179.08 L 293.66 198.92 A 13.52 6.38 -90.0 0 1 287.28 212.44 L 287.26 212.44 A 0.12 0.12 0.0 0 1 287.14 212.32 L 287.14 165.68 Z"/>
                <path fill="#342268" d="M 168.18 233.78 C 177.29 233.79 187.30 230.98 193.52 224.38 Q 197.01 220.66 198.39 220.17 Q 202.50 218.71 206.14 221.64 A 3.54 3.52 -87.7 0 1 206.92 222.49 C 212.13 230.38 202.52 238.48 196.03 242.13 C 187.51 246.91 177.86 249.08 168.16 249.07 C 158.45 249.05 148.81 246.86 140.30 242.04 C 133.82 238.38 124.24 230.25 129.47 222.37 A 3.54 3.52 87.9 0 1 130.25 221.53 Q 133.90 218.61 138.01 220.08 Q 139.39 220.57 142.87 224.30 C 149.07 230.92 159.07 233.77 168.18 233.78 Z"/>
                <path fill="#b173ff" d="M 262.46 334.70 Q 262.30 334.82 262.23 335.00 L 74.78 335.00 Q 74.52 334.90 74.33 334.70 Q 74.76 334.28 74.99 333.75 Q 78.14 326.67 84.18 321.18 C 97.77 308.82 120.42 303.50 139.05 302.32 Q 169.69 300.37 197.77 302.30 C 219.34 303.79 251.75 311.03 261.58 333.66 Q 261.84 334.28 262.46 334.70 Z"/>
            </svg>
        </div>
        <h1>Astonish</h1>
    </div>
    <div class="chat-container">
        <div class="messages" id="messages"></div>
        <div class="loading" id="loading">Agent is thinking...</div>
        <div class="input-container">
            <input type="text" id="messageInput" placeholder="Type your message..." />
            <button id="sendButton">Send</button>
        </div>
    </div>
    <script>
        const messagesDiv = document.getElementById('messages');
        const messageInput = document.getElementById('messageInput');
        const sendButton = document.getElementById('sendButton');
        const loadingDiv = document.getElementById('loading');
        let sessionId = 'user-' + Date.now();
        let currentAgentMessage = null;

        function addMessage(text, isUser) {
            const messageDiv = document.createElement('div');
            messageDiv.className = 'message ' + (isUser ? 'user' : 'agent');
            let formattedText = text.replace(/\n/g, '<br>');
            messageDiv.innerHTML = formattedText;
            messagesDiv.appendChild(messageDiv);
            messagesDiv.scrollTop = messagesDiv.scrollHeight;
            return messageDiv;
        }

        function updateMessage(messageDiv, text) {
            let formattedText = text.replace(/\n/g, '<br>');
            messageDiv.innerHTML = formattedText;
            messagesDiv.scrollTop = messagesDiv.scrollHeight;
        }

        async function sendMessage() {
            const message = messageInput.value.trim();
            if (!message) return;
            
            addMessage(message, true);
            messageInput.value = '';
            sendButton.disabled = true;
            loadingDiv.classList.add('active');
            
            // Create empty agent message for streaming
            currentAgentMessage = addMessage('', false);
            let accumulatedText = '';
            
            try {
                const response = await fetch('/api/chat', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ message: message, sessionId: sessionId })
                });

                const reader = response.body.getReader();
                const decoder = new TextDecoder();

                while (true) {
                    const { done, value } = await reader.read();
                    if (done) break;

                    const chunk = decoder.decode(value);
                    const lines = chunk.split('\n');

                    for (const line of lines) {
                        if (line.startsWith('data: ')) {
                            const data = line.slice(6);
                            try {
                                const parsed = JSON.parse(data);
                                if (parsed.text) {
                                    accumulatedText += parsed.text;
                                    updateMessage(currentAgentMessage, accumulatedText);
                                } else if (parsed.error) {
                                    updateMessage(currentAgentMessage, 'Error: ' + parsed.error);
                                } else if (parsed.done) {
                                    // Stream complete
                                }
                            } catch (e) {
                                // Ignore parse errors for incomplete chunks
                            }
                        }
                    }
                }
            } catch (error) {
                updateMessage(currentAgentMessage, 'Error: ' + error.message);
            } finally {
                sendButton.disabled = false;
                loadingDiv.classList.remove('active');
                messageInput.focus();
                currentAgentMessage = null;
            }
        }

        sendButton.addEventListener('click', sendMessage);
        messageInput.addEventListener('keypress', (e) => { if (e.key === 'Enter') sendMessage(); });
        messageInput.focus();
    </script>
</body>
</html>`
