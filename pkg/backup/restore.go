package backup

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type RestorePlan struct {
	Archive       Summary            `json:"archive"`
	TargetBackend string             `json:"targetBackend"`
	TargetEmpty   bool               `json:"targetEmpty"`
	Scopes        []RestoreScopePlan `json:"scopes,omitempty"`
	Entries       []RestoreEntryPlan `json:"entries,omitempty"`
	Warnings      []string           `json:"warnings,omitempty"`
	Blockers      []string           `json:"blockers,omitempty"`
}

type RestoreScopePlan struct {
	Scope   Scope `json:"scope"`
	Entries int   `json:"entries"`
	Records int64 `json:"records"`
}

type RestoreEntryPlan struct {
	Path     string `json:"path"`
	Scope    Scope  `json:"scope"`
	Entity   string `json:"entity,omitempty"`
	Records  int64  `json:"records"`
	Action   string `json:"action"`
	Reason   string `json:"reason,omitempty"`
	Redacted bool   `json:"redacted,omitempty"`
}

type RestoreResult struct {
	Plan            RestorePlan `json:"plan"`
	RestoredEntries int         `json:"restoredEntries"`
	RestoredRecords int64       `json:"restoredRecords"`
	SkippedEntries  int         `json:"skippedEntries"`
	Warnings        []string    `json:"warnings,omitempty"`
}

type RecordScanner struct {
	scanner *bufio.Scanner
	entity  string
	line    int
}

func NewRecordScanner(r io.Reader, entity string) *RecordScanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	return &RecordScanner{scanner: scanner, entity: entity}
}

func (rs *RecordScanner) Next() bool {
	if rs.scanner.Scan() {
		rs.line++
		return true
	}
	return false
}

func (rs *RecordScanner) Record() (Record, error) {
	var record Record
	dec := json.NewDecoder(bytes.NewReader(rs.scanner.Bytes()))
	dec.UseNumber()
	if err := dec.Decode(&record); err != nil {
		return Record{}, fmt.Errorf("decode %s record line %d: %w", rs.entity, rs.line, err)
	}
	if record.Entity == "" {
		record.Entity = rs.entity
	}
	return record, nil
}

func (rs *RecordScanner) Err() error {
	if err := rs.scanner.Err(); err != nil {
		return fmt.Errorf("scan %s records: %w", rs.entity, err)
	}
	return nil
}

func DecodeRecordData(record Record) (map[string]any, error) {
	var data map[string]any
	dec := json.NewDecoder(bytes.NewReader(record.Data))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		return nil, fmt.Errorf("decode %s data: %w", record.Entity, err)
	}
	return data, nil
}
