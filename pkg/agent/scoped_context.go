package agent

import (
	"context"
	"iter"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool/toolconfirmation"
	"google.golang.org/genai"
)

// minimalReadonlyContext implements agent.ReadonlyContext for fetching tools from toolsets
type minimalReadonlyContext struct {
	context.Context
	actions   *session.EventActions
	state     session.State
	sessionID string
}

func (m *minimalReadonlyContext) AgentName() string                    { return "astonish-agent" }
func (m *minimalReadonlyContext) AppName() string                      { return "astonish" }
func (m *minimalReadonlyContext) UserContent() *genai.Content          { return nil }
func (m *minimalReadonlyContext) InvocationID() string                 { return "" }
func (m *minimalReadonlyContext) ReadonlyState() session.ReadonlyState { return nil }
func (m *minimalReadonlyContext) UserID() string                       { return "" }
func (m *minimalReadonlyContext) SessionID() string                    { return m.sessionID }
func (m *minimalReadonlyContext) Branch() string                       { return "" }
func (m *minimalReadonlyContext) Actions() *session.EventActions {
	if m.actions == nil {
		return &session.EventActions{}
	}
	return m.actions
}
func (m *minimalReadonlyContext) SearchMemory(ctx context.Context, query string) (*memory.SearchResponse, error) {
	return nil, nil
}
func (m *minimalReadonlyContext) FunctionCallID() string     { return "" }
func (m *minimalReadonlyContext) Artifacts() agent.Artifacts { return nil }
func (m *minimalReadonlyContext) State() session.State       { return m.state }
func (m *minimalReadonlyContext) RequestConfirmation(hint string, payload any) error {
	return nil
}
func (m *minimalReadonlyContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return nil }

// ScopedState wraps a parent state and allows local overrides
type ScopedState struct {
	Parent session.State
	Local  map[string]any
}

func (s *ScopedState) Get(key string) (any, error) {
	if v, ok := s.Local[key]; ok {
		return v, nil
	}
	return s.Parent.Get(key)
}

func (s *ScopedState) Set(key string, value any) error {
	s.Local[key] = value
	return nil
}

func (s *ScopedState) Delete(key string) error {
	delete(s.Local, key)
	return nil
}

func (s *ScopedState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		// Yield local keys
		for k, v := range s.Local {
			if !yield(k, v) {
				return
			}
		}
		// Yield parent keys if not in local
		for k, v := range s.Parent.All() {
			if _, ok := s.Local[k]; !ok {
				if !yield(k, v) {
					return
				}
			}
		}
	}
}

// ScopedContext wraps an InvocationContext and overrides the state
type ScopedContext struct {
	agent.InvocationContext
	state   session.State
	session session.Session
	agent   agent.Agent
}

func (s *ScopedContext) SessionID() string {
	return s.session.ID()
}

func (s *ScopedContext) Session() session.Session {
	return &ScopedSession{
		Session: s.session,
		state:   s.state,
	}
}

func (s *ScopedContext) State() session.State {
	return s.state
}

func (s *ScopedContext) Agent() agent.Agent {
	if s.agent != nil {
		return s.agent
	}
	return s.InvocationContext.Agent()
}

// ScopedSession wraps a Session and overrides the state
type ScopedSession struct {
	session.Session
	state session.State
}

func (s *ScopedSession) State() session.State {
	return s.state
}
