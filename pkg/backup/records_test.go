package backup

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRecordWriterWritesJSONL(t *testing.T) {
	var sb strings.Builder
	writer := NewRecordWriter(&sb, "users")
	if err := writer.Write("u1", map[string]string{"email": "alice@example.com"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if writer.Records() != 1 {
		t.Fatalf("Records() = %d, want 1", writer.Records())
	}

	lines := strings.Split(strings.TrimSpace(sb.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	var record Record
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("Unmarshal record error = %v", err)
	}
	if record.Entity != "users" || record.ID != "u1" {
		t.Fatalf("record = %+v, want users/u1", record)
	}
	var data map[string]string
	if err := json.Unmarshal(record.Data, &data); err != nil {
		t.Fatalf("Unmarshal data error = %v", err)
	}
	if data["email"] != "alice@example.com" {
		t.Fatalf("email = %q, want alice@example.com", data["email"])
	}
}

func TestEncodeRecords(t *testing.T) {
	data, count, err := EncodeRecords("orgs", []RecordValue{
		{ID: "o1", Value: map[string]string{"slug": "acme"}},
		{ID: "o2", Value: map[string]string{"slug": "globex"}},
	})
	if err != nil {
		t.Fatalf("EncodeRecords() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if got := strings.Count(string(data), "\n"); got != 2 {
		t.Fatalf("newline count = %d, want 2", got)
	}
}
