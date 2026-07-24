package backup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type Record struct {
	Entity string          `json:"entity"`
	ID     string          `json:"id,omitempty"`
	Data   json.RawMessage `json:"data"`
}

type RecordWriter struct {
	w       io.Writer
	entity  string
	records int64
}

func NewRecordWriter(w io.Writer, entity string) *RecordWriter {
	return &RecordWriter{w: w, entity: entity}
}

func (rw *RecordWriter) Write(id string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s record: %w", rw.entity, err)
	}
	record := Record{
		Entity: rw.entity,
		ID:     id,
		Data:   data,
	}
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal %s record envelope: %w", rw.entity, err)
	}
	if _, err := rw.w.Write(append(line, '\n')); err != nil {
		return err
	}
	rw.records++
	return nil
}

func (rw *RecordWriter) Records() int64 {
	return rw.records
}

func EncodeRecords(entity string, values []RecordValue) ([]byte, int64, error) {
	var buf bytes.Buffer
	rw := NewRecordWriter(&buf, entity)
	for _, value := range values {
		if err := rw.Write(value.ID, value.Value); err != nil {
			return nil, 0, err
		}
	}
	return buf.Bytes(), rw.Records(), nil
}

type RecordValue struct {
	ID    string
	Value any
}
