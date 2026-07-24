package backup

import (
	"strings"
	"testing"
)

func TestRecordScanner(t *testing.T) {
	scanner := NewRecordScanner(strings.NewReader(`{"entity":"users","id":"u1","data":{"email":"alice@example.com","count":3}}
`), "users")
	if !scanner.Next() {
		t.Fatal("Next() = false, want record")
	}
	record, err := scanner.Record()
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if record.Entity != "users" || record.ID != "u1" {
		t.Fatalf("record = %+v, want users/u1", record)
	}
	data, err := DecodeRecordData(record)
	if err != nil {
		t.Fatalf("DecodeRecordData() error = %v", err)
	}
	if data["email"] != "alice@example.com" {
		t.Fatalf("email = %v, want alice@example.com", data["email"])
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Err() error = %v", err)
	}
}
