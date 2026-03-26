package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	adksession "google.golang.org/adk/session"
)

// Transcript handles reading and writing JSONL session transcript files.
// Each line in the file is a JSON-serialized TranscriptEntry.
type Transcript struct {
	path string
}

// TranscriptEntry is a single line in the JSONL transcript file.
type TranscriptEntry struct {
	Type  string            `json:"type"`            // "header" or "event"
	Event *adksession.Event `json:"event,omitempty"` // For "event" type
	// Header fields
	SessionID string `json:"sessionId,omitempty"` // For "header" type
	Version   int    `json:"version,omitempty"`   // For "header" type
}

// NewTranscript creates a Transcript for the given file path.
func NewTranscript(path string) *Transcript {
	return &Transcript{path: path}
}

// WriteHeader writes the initial header line to a new transcript file.
func (t *Transcript) WriteHeader(sessionID string) error {
	dir := filepath.Dir(t.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create transcript directory: %w", err)
	}

	entry := TranscriptEntry{
		Type:      "header",
		SessionID: sessionID,
		Version:   1,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to serialize header: %w", err)
	}

	return os.WriteFile(t.path, append(data, '\n'), 0644)
}

// AppendEvent appends a single event to the transcript file.
func (t *Transcript) AppendEvent(event *adksession.Event) error {
	return t.appendEventData(event, nil)
}

// AppendEventRedacted appends a single event to the transcript file,
// applying a redaction function to the serialized JSON before writing.
// This ensures credential values are never written to disk.
func (t *Transcript) AppendEventRedacted(event *adksession.Event, redactFunc func(string) string) error {
	return t.appendEventData(event, redactFunc)
}

func (t *Transcript) appendEventData(event *adksession.Event, redactFunc func(string) string) error {
	entry := TranscriptEntry{
		Type:  "event",
		Event: event,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	// Apply redaction to the serialized JSON if configured
	if redactFunc != nil {
		data = []byte(redactFunc(string(data)))
	}

	data = append(data, '\n')

	f, err := os.OpenFile(t.path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to open transcript for append: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to append event: %w", err)
	}

	return f.Sync()
}

// ReadEvents reads all events from the transcript file (skipping the header).
// Returns events in chronological order.
func (t *Transcript) ReadEvents() ([]*adksession.Event, error) {
	f, err := os.Open(t.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open transcript: %w", err)
	}
	defer f.Close()

	var events []*adksession.Event
	scanner := bufio.NewScanner(f)
	// Increase buffer size for large tool responses
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // up to 10MB per line

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry TranscriptEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			// Skip malformed lines
			continue
		}

		if entry.Type == "event" && entry.Event != nil {
			events = append(events, entry.Event)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading transcript: %w", err)
	}

	return events, nil
}

// EventCount returns the number of events in the transcript (excluding header).
func (t *Transcript) EventCount() int {
	events, err := t.ReadEvents()
	if err != nil {
		return 0
	}
	return len(events)
}

// Exists checks if the transcript file exists.
func (t *Transcript) Exists() bool {
	_, err := os.Stat(t.path)
	return err == nil
}

// Rewrite atomically replaces the transcript with a new header and events.
// Used after compaction to persist the compacted conversation on disk.
func (t *Transcript) Rewrite(sessionID string, events []*adksession.Event) error {
	// Build new content in memory
	var lines [][]byte

	// Header
	header := TranscriptEntry{
		Type:      "header",
		SessionID: sessionID,
		Version:   1,
	}
	headerData, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("failed to serialize header: %w", err)
	}
	lines = append(lines, headerData)

	// Events
	for _, event := range events {
		entry := TranscriptEntry{
			Type:  "event",
			Event: event,
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("failed to serialize event: %w", err)
		}
		lines = append(lines, data)
	}

	// Build the final content
	var content []byte
	for _, line := range lines {
		content = append(content, line...)
		content = append(content, '\n')
	}

	// Atomic write to prevent corruption
	return atomicWrite(t.path, content, 0644)
}

// RedactTranscript retroactively applies a redaction function to every line
// of the transcript file and atomically rewrites it. This is used after new
// credential values are registered (e.g. after save_credential) to scrub
// secrets from user messages that were persisted before the secret was known.
func (t *Transcript) RedactTranscript(redactFunc func(string) string) error {
	if redactFunc == nil {
		return nil
	}

	f, err := os.Open(t.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to open transcript for redaction: %w", err)
	}
	defer f.Close()

	var content []byte
	changed := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // up to 10MB per line

	for scanner.Scan() {
		line := scanner.Text()
		redacted := redactFunc(line)
		if redacted != line {
			changed = true
		}
		content = append(content, []byte(redacted)...)
		content = append(content, '\n')
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading transcript for redaction: %w", err)
	}

	// Only rewrite if something actually changed
	if !changed {
		return nil
	}

	return atomicWrite(t.path, content, 0644)
}
