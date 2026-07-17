package agent

// TutorialBlueprintPending holds a blueprint awaiting creator approval in chat.
type TutorialBlueprintPending struct {
	YAML   string
	Title  string
	Suite  string
	Scenes []TutorialBlueprintSceneView
}

// TutorialBlueprintSceneView is the card-facing scene row.
type TutorialBlueprintSceneView struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Voiceover     string `json:"voiceover"`
	VisualKind    string `json:"visual_kind"`
	VisualDesc    string `json:"visual_description"`
	DurationHintS int    `json:"duration_hint_s,omitempty"`
}

// SetPendingTutorialBlueprint stores a blueprint for Approve / Revise / Cancel.
func (c *ChatAgent) SetPendingTutorialBlueprint(sessionID string, bp *TutorialBlueprintPending) {
	c.traceMu.Lock()
	defer c.traceMu.Unlock()
	if c.pendingTutorialBP == nil {
		c.pendingTutorialBP = make(map[string]*TutorialBlueprintPending)
	}
	c.pendingTutorialBP[sessionID] = bp
}

// GetPendingTutorialBlueprint returns the pending blueprint, or nil.
func (c *ChatAgent) GetPendingTutorialBlueprint(sessionID string) *TutorialBlueprintPending {
	c.traceMu.Lock()
	defer c.traceMu.Unlock()
	return c.pendingTutorialBP[sessionID]
}

// HasPendingTutorialBlueprint reports whether a blueprint awaits approval.
func (c *ChatAgent) HasPendingTutorialBlueprint(sessionID string) bool {
	return c.GetPendingTutorialBlueprint(sessionID) != nil
}

// CancelPendingTutorialBlueprint clears pending blueprint state.
func (c *ChatAgent) CancelPendingTutorialBlueprint(sessionID string) {
	c.traceMu.Lock()
	defer c.traceMu.Unlock()
	delete(c.pendingTutorialBP, sessionID)
}
