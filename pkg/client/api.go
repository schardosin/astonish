package client

import (
	"fmt"
	"net/http"
)

// --- Session API ---

// SessionMeta represents a chat session summary.
type SessionMeta struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	MessageCount int    `json:"messageCount"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
	ParentID     string `json:"parentId,omitempty"`
	FleetKey     string `json:"fleetKey,omitempty"`
	FleetName    string `json:"fleetName,omitempty"`
}

// ListSessions returns all chat sessions.
func (c *Client) ListSessions() ([]SessionMeta, error) {
	var sessions []SessionMeta
	if err := c.DoJSON("GET", "/api/studio/sessions", nil, &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

// GetSession returns the details of a single session.
func (c *Client) GetSession(id string) (map[string]any, error) {
	var detail map[string]any
	if err := c.DoJSON("GET", fmt.Sprintf("/api/studio/sessions/%s", id), nil, &detail); err != nil {
		return nil, err
	}
	return detail, nil
}

// TraceOpts holds query parameters for the session trace endpoint.
type TraceOpts struct {
	Recursive bool
	ToolsOnly bool
	Verbose   bool
	LastN     int
}

// GetSessionTrace returns the chronological trace of a session.
func (c *Client) GetSessionTrace(id string, opts TraceOpts) (map[string]any, error) {
	path := fmt.Sprintf("/api/studio/sessions/%s/trace?", id)
	params := ""
	if opts.Recursive {
		params += "recursive=true&"
	}
	if opts.ToolsOnly {
		params += "tools_only=true&"
	}
	if opts.Verbose {
		params += "verbose=true&"
	}
	if opts.LastN > 0 {
		params += fmt.Sprintf("last_n=%d&", opts.LastN)
	}
	// Trim trailing & or ?
	url := path + params
	if url[len(url)-1] == '&' || url[len(url)-1] == '?' {
		url = url[:len(url)-1]
	}

	var trace map[string]any
	if err := c.DoJSON("GET", url, nil, &trace); err != nil {
		return nil, err
	}
	return trace, nil
}

// DeleteSession deletes a chat session.
func (c *Client) DeleteSession(id string) error {
	resp, err := c.Do("DELETE", fmt.Sprintf("/api/studio/sessions/%s", id), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return parseErrorResponse(resp)
	}
	return nil
}

// --- Flow API ---

// FlowMeta represents a flow summary.
type FlowMeta struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path,omitempty"`
}

// ListFlows returns all available flows.
func (c *Client) ListFlows() ([]FlowMeta, error) {
	var flows []FlowMeta
	if err := c.DoJSON("GET", "/api/flows", nil, &flows); err != nil {
		return nil, err
	}
	return flows, nil
}

// RunFlow starts a flow execution and returns an SSE stream.
func (c *Client) RunFlow(flowName string, params map[string]string) (*SSEStream, error) {
	body := map[string]any{
		"flow": flowName,
	}
	if len(params) > 0 {
		body["params"] = params
	}
	return c.SSE("POST", "/api/run", body)
}

// --- Chat API ---

// ChatRequest represents a message to send to the chat.
type ChatRequest struct {
	SessionID   string `json:"sessionId,omitempty"`
	Message     string `json:"message"`
	AutoApprove bool   `json:"autoApprove,omitempty"`
}

// SendChatMessage sends a chat message and returns an SSE stream of events.
func (c *Client) SendChatMessage(req *ChatRequest) (*SSEStream, error) {
	return c.SSE("POST", "/api/studio/chat", req)
}

// GetSessionStatus checks if a session has an active runner.
func (c *Client) GetSessionStatus(sessionID string) (bool, error) {
	var status struct {
		Running bool `json:"running"`
	}
	if err := c.DoJSON("GET", fmt.Sprintf("/api/studio/sessions/%s/status", sessionID), nil, &status); err != nil {
		return false, err
	}
	return status.Running, nil
}

// ReconnectSession connects to an active session's SSE stream.
func (c *Client) ReconnectSession(sessionID string) (*SSEStream, error) {
	return c.SSE("GET", fmt.Sprintf("/api/studio/sessions/%s/stream", sessionID), nil)
}

// StopSession stops a running session.
func (c *Client) StopSession(sessionID string) error {
	resp, err := c.Do("POST", fmt.Sprintf("/api/studio/sessions/%s/stop", sessionID), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return parseErrorResponse(resp)
	}
	return nil
}

// --- Org/Team API ---

// OrgInfo represents an organization.
type OrgInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role string `json:"role"`
}

// TeamInfo represents a team.
type TeamInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// ListOrgs returns the organizations the user belongs to.
func (c *Client) ListOrgs() ([]OrgInfo, error) {
	var resp struct {
		Orgs      []OrgInfo `json:"orgs"`
		ActiveOrg string    `json:"active_org"`
	}
	if err := c.DoJSON("GET", "/api/orgs", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Orgs, nil
}

// ListTeams returns the teams in the current org.
func (c *Client) ListTeams() ([]TeamInfo, error) {
	var resp struct {
		Teams []TeamInfo `json:"teams"`
	}
	if err := c.DoJSON("GET", "/api/teams", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Teams, nil
}

// SwitchOrg changes the active org by re-authenticating.
func (c *Client) SwitchOrg(orgSlug string) error {
	resp, err := c.Do("POST", "/api/auth/switch-org", map[string]string{"org": orgSlug})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return parseErrorResponse(resp)
	}
	return nil
}

// --- Scheduler API ---

// SchedulerJob represents a scheduled job.
type SchedulerJob struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Enabled  bool   `json:"enabled"`
	FlowName string `json:"flow_name"`
}

// ListSchedulerJobs returns all scheduler jobs.
func (c *Client) ListSchedulerJobs() ([]SchedulerJob, error) {
	var jobs []SchedulerJob
	if err := c.DoJSON("GET", "/api/scheduler/jobs", nil, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

// --- Fleet API ---

// FleetPlan represents a fleet plan.
type FleetPlan struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// ListFleetPlans returns all fleet plans.
func (c *Client) ListFleetPlans() ([]FleetPlan, error) {
	var plans []FleetPlan
	if err := c.DoJSON("GET", "/api/fleet-plans", nil, &plans); err != nil {
		return nil, err
	}
	return plans, nil
}

// --- Drill API ---

// DrillSuite represents a drill test suite.
type DrillSuite struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ListDrillSuites returns all drill suites.
func (c *Client) ListDrillSuites() ([]DrillSuite, error) {
	var suites []DrillSuite
	if err := c.DoJSON("GET", "/api/drills", nil, &suites); err != nil {
		return nil, err
	}
	return suites, nil
}

// --- Utility ---

// Ping checks if the remote server is reachable and authenticated.
func (c *Client) Ping() error {
	resp, err := c.Do("GET", "/api/auth/me", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("not authenticated")
	}
	if resp.StatusCode >= 400 {
		return parseErrorResponse(resp)
	}
	return nil
}
