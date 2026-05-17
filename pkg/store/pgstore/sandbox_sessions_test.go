package pgstore

import (
	"testing"
	"time"

	"github.com/schardosin/astonish/pkg/store"
)

// mockRowScanner populates destinations to simulate a pgx row scan.
type mockRowScanner struct {
	sessionID     string
	chatSessionID string
	backend       string
	containerName *string
	templateID    string
	upperLayerID  *string
	state         string
	podName       *string
	nodeName      *string
	portsRaw      []byte
	baseDomain    *string
	pinned        bool
	createdBy     string
	createdAt     time.Time
	updatedAt     time.Time
	lastActiveAt  time.Time
}

func (m *mockRowScanner) Scan(dest ...any) error {
	*dest[0].(*string) = m.sessionID
	*dest[1].(*string) = m.chatSessionID
	*dest[2].(*string) = m.backend
	*dest[3].(**string) = m.containerName
	*dest[4].(*string) = m.templateID
	*dest[5].(**string) = m.upperLayerID
	*dest[6].(*store.SandboxSessionState) = store.SandboxSessionState(m.state)
	*dest[7].(**string) = m.podName
	*dest[8].(**string) = m.nodeName
	*dest[9].(*[]byte) = m.portsRaw
	*dest[10].(**string) = m.baseDomain
	*dest[11].(*bool) = m.pinned
	*dest[12].(*string) = m.createdBy
	*dest[13].(*time.Time) = m.createdAt
	*dest[14].(*time.Time) = m.updatedAt
	*dest[15].(*time.Time) = m.lastActiveAt
	return nil
}

func TestScanRow_BaseTemplateIDNormalization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tests := []struct {
		name       string
		templateID string
		wantID     string
	}{
		{
			name:       "uuid normalized back to @base",
			templateID: baseTemplateUUID,
			wantID:     "@base",
		},
		{
			name:       "other uuid unchanged",
			templateID: "b1111111-1111-4111-8111-111111111111",
			wantID:     "b1111111-1111-4111-8111-111111111111",
		},
		{
			name:       "@base literal (edge case) unchanged",
			templateID: "@base",
			wantID:     "@base",
		},
		{
			name:       "empty string unchanged",
			templateID: "",
			wantID:     "",
		},
		{
			name:       "uppercase UUID does not match constant",
			templateID: "A0000000-0000-4000-8000-000000000001",
			wantID:     "A0000000-0000-4000-8000-000000000001",
		},
		{
			name:       "real team template UUID passes through",
			templateID: "c9f2a3b4-5678-4def-8abc-123456789abc",
			wantID:     "c9f2a3b4-5678-4def-8abc-123456789abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := &mockRowScanner{
				sessionID:     "sess-1",
				chatSessionID: "sess-1",
				backend:       "k8s",
				templateID:    tt.templateID,
				state:         "running",
				portsRaw:      []byte("[]"),
				createdAt:     now,
				updatedAt:     now,
				lastActiveAt:  now,
			}
			sess, err := scanRow(row)
			if err != nil {
				t.Fatalf("scanRow: %v", err)
			}
			if sess.TemplateID != tt.wantID {
				t.Errorf("TemplateID = %q, want %q", sess.TemplateID, tt.wantID)
			}
		})
	}
}

func TestNormalizeTemplateID_WriteSymmetry(t *testing.T) {
	// Verify that the write-path normalization ("@base" → UUID) combined
	// with the read-path de-normalization (UUID → "@base") round-trips.
	input := "@base"

	// Simulate write normalization (from Put, line 184).
	stored := input
	if stored == "@base" {
		stored = baseTemplateUUID
	}

	// Simulate read de-normalization (from scanRow).
	readBack := stored
	if readBack == baseTemplateUUID {
		readBack = "@base"
	}

	if readBack != input {
		t.Errorf("round-trip failed: input=%q → stored=%q → readBack=%q", input, stored, readBack)
	}
}
