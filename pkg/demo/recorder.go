package demo

import (
	"time"
)

// EventType defines the type of terminal event
type EventType string

const (
	EventCommand   EventType = "command"    // Typed character-by-character with cursor
	EventOutput    EventType = "output"     // Appears instantly or with fade
	EventPrompt    EventType = "prompt"     // Input prompt from the agent
	EventUserInput EventType = "user_input" // User's typed response
)

// Event represents a single terminal event with timing information
type Event struct {
	Type      EventType     `yaml:"type"`
	Text      string        `yaml:"text"`
	Delay     time.Duration `yaml:"delay"`      // Delay before this event starts
	TypeSpeed int           `yaml:"type_speed"` // Typing speed in ms per character (0 = instant)
}

// DemoScript defines the structure of a demo recording script
type DemoScript struct {
	Title  string  `yaml:"title"`
	Width  int     `yaml:"width"`
	Height int     `yaml:"height"`
	Events []Event `yaml:"events"`
}

// Recorder captures terminal events with timing for demo generation
type Recorder struct {
	events    []Event
	startTime time.Time
	lastEvent time.Time
}

// NewRecorder creates a new event recorder
func NewRecorder() *Recorder {
	return &Recorder{
		events: make([]Event, 0),
	}
}

// Start begins recording, setting the start time
func (r *Recorder) Start() {
	r.startTime = time.Now()
	r.lastEvent = r.startTime
}

// AddEvent records a new event with automatic delay calculation
func (r *Recorder) AddEvent(eventType EventType, text string, typeSpeed int) {
	now := time.Now()
	delay := now.Sub(r.lastEvent)
	r.lastEvent = now

	r.events = append(r.events, Event{
		Type:      eventType,
		Text:      text,
		Delay:     delay,
		TypeSpeed: typeSpeed,
	})
}

// AddCommand records a command event (typed with cursor)
func (r *Recorder) AddCommand(text string) {
	r.AddEvent(EventCommand, text, 30) // Default typing speed
}

// AddOutput records an output event (instant display)
func (r *Recorder) AddOutput(text string) {
	r.AddEvent(EventOutput, text, 0)
}

// AddPrompt records a prompt event
func (r *Recorder) AddPrompt(text string) {
	r.AddEvent(EventPrompt, text, 0)
}

// AddUserInput records user input (typed with different pacing)
func (r *Recorder) AddUserInput(text string) {
	r.AddEvent(EventUserInput, text, 50) // Slightly slower for user input
}

// Events returns all recorded events
func (r *Recorder) Events() []Event {
	return r.events
}

// ToScript converts recorded events to a DemoScript
func (r *Recorder) ToScript(title string, width, height int) *DemoScript {
	return &DemoScript{
		Title:  title,
		Width:  width,
		Height: height,
		Events: r.events,
	}
}
