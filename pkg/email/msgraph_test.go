package email

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMSGraphClient_Connect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/me" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("unexpected auth header: %s", auth)
		}
		json.NewEncoder(w).Encode(map[string]string{"mail": "test@example.com"})
	}))
	defer srv.Close()

	// Override graphBaseURL for testing — we'll use a custom client
	client := newTestClient(srv, "test-token")

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
	if !client.IsConnected() {
		t.Error("expected IsConnected()=true after Connect()")
	}
}

func TestMSGraphClient_Connect_TokenError(t *testing.T) {
	client := &MSGraphClient{
		cfg:       &Config{Address: "test@example.com"},
		tokenFunc: func() (string, error) { return "", fmt.Errorf("token expired") },
		client:    http.DefaultClient,
	}

	err := client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error from Connect() when token fails")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("expected token error, got: %v", err)
	}
}

func TestMSGraphClient_ListMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/mailFolders/Inbox/messages") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := graphMessageList{
			Value: []graphMessage{
				{
					ID:               "msg-1",
					Subject:          "Hello",
					ReceivedDateTime: "2025-01-15T10:30:00Z",
					IsRead:           false,
					HasAttachments:   true,
					InternetMessageID: "<abc@example.com>",
					From: &graphEmailAddress{},
					ToRecipients: []graphRecipient{
						{},
					},
				},
			},
		}
		resp.Value[0].From.EmailAddress.Address = "sender@example.com"
		resp.Value[0].From.EmailAddress.Name = "Sender"
		resp.Value[0].ToRecipients[0].EmailAddress.Address = "test@example.com"
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := newTestClient(srv, "test-token")
	client.connected = true

	msgs, err := client.ListMessages(context.Background(), ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages() error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ID != "msg-1" {
		t.Errorf("expected ID=msg-1, got %s", msgs[0].ID)
	}
	if msgs[0].Subject != "Hello" {
		t.Errorf("expected Subject=Hello, got %s", msgs[0].Subject)
	}
	if !msgs[0].Unread {
		t.Error("expected Unread=true")
	}
	if !msgs[0].HasAttachments {
		t.Error("expected HasAttachments=true")
	}
}

func TestMSGraphClient_ReadMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/me/messages/msg-42") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := graphMessage{
			ID:               "msg-42",
			Subject:          "Test Email",
			ReceivedDateTime: "2025-06-01T12:00:00Z",
			IsRead:           true,
			From:             &graphEmailAddress{},
			Body: &graphBody{
				ContentType: "text",
				Content:     "Hello, this is a test.",
			},
			Attachments: []graphAttachment{
				{ID: "att-1", Name: "file.pdf", Size: 1024, ContentType: "application/pdf"},
			},
		}
		resp.From.EmailAddress.Address = "someone@example.com"
		resp.From.EmailAddress.Name = "Someone"
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := newTestClient(srv, "test-token")
	client.connected = true

	msg, err := client.ReadMessage(context.Background(), "msg-42")
	if err != nil {
		t.Fatalf("ReadMessage() error: %v", err)
	}
	if msg.ID != "msg-42" {
		t.Errorf("expected ID=msg-42, got %s", msg.ID)
	}
	if msg.Body != "Hello, this is a test." {
		t.Errorf("unexpected body: %s", msg.Body)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}
	if msg.Attachments[0].Name != "file.pdf" {
		t.Errorf("expected attachment name=file.pdf, got %s", msg.Attachments[0].Name)
	}
}

func TestMSGraphClient_Send(t *testing.T) {
	var receivedPayload map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/me/sendMail" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client := newTestClient(srv, "test-token")
	client.connected = true

	msgID, err := client.Send(context.Background(), OutgoingMessage{
		To:      []string{"recipient@example.com"},
		Subject: "Test Subject",
		Body:    "Hello!",
	})
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if msgID == "" {
		t.Error("expected non-empty message ID")
	}

	// Verify payload structure
	msg, ok := receivedPayload["message"].(map[string]any)
	if !ok {
		t.Fatal("expected message in payload")
	}
	if msg["subject"] != "Test Subject" {
		t.Errorf("unexpected subject: %v", msg["subject"])
	}
}

func TestMSGraphClient_MarkRead(t *testing.T) {
	var patchedIDs []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		// Extract message ID from path
		parts := strings.Split(r.URL.Path, "/")
		patchedIDs = append(patchedIDs, parts[len(parts)-1])

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["isRead"] != true {
			t.Errorf("expected isRead=true, got %v", body["isRead"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(srv, "test-token")
	client.connected = true

	err := client.MarkRead(context.Background(), []string{"msg-1", "msg-2"})
	if err != nil {
		t.Fatalf("MarkRead() error: %v", err)
	}
	if len(patchedIDs) != 2 {
		t.Errorf("expected 2 patches, got %d", len(patchedIDs))
	}
}

func TestMSGraphClient_Delete(t *testing.T) {
	var deletedPaths []string
	var movedPaths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletedPaths = append(deletedPaths, r.URL.Path)
		} else if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/move") {
			movedPaths = append(movedPaths, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(srv, "test-token")
	client.connected = true

	// Test soft delete (move to trash)
	err := client.Delete(context.Background(), []string{"msg-1"}, false)
	if err != nil {
		t.Fatalf("Delete(permanent=false) error: %v", err)
	}
	if len(movedPaths) != 1 {
		t.Errorf("expected 1 move, got %d", len(movedPaths))
	}

	// Test permanent delete
	err = client.Delete(context.Background(), []string{"msg-2"}, true)
	if err != nil {
		t.Fatalf("Delete(permanent=true) error: %v", err)
	}
	if len(deletedPaths) != 1 {
		t.Errorf("expected 1 delete, got %d", len(deletedPaths))
	}
}

func TestMSGraphClient_GraphError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"code":    "ErrorAccessDenied",
				"message": "Access is denied. Check credentials and try again.",
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(srv, "test-token")

	err := client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error from Connect() with 403")
	}
	if !strings.Contains(err.Error(), "ErrorAccessDenied") {
		t.Errorf("expected ErrorAccessDenied in error, got: %v", err)
	}
}

// --- Test helpers ---

// newTestClient creates an MSGraphClient pointing at a test server.
// It overrides the graphBaseURL by using a custom HTTP client transport.
func newTestClient(srv *httptest.Server, token string) *MSGraphClient {
	// We override the base URL by intercepting requests
	client := &MSGraphClient{
		cfg:       &Config{Address: "test@example.com", MaxBodyChars: 50000},
		tokenFunc: func() (string, error) { return token, nil },
		client:    srv.Client(),
	}
	// Replace the graphBaseURL in all method calls by using a redirect-based approach
	// Actually, we need to directly patch the URLs. Let's create a wrapper.
	// The simplest approach: override the test to use a URL-rewriting transport.
	client.client.Transport = &testTransport{
		base:     srv.Client().Transport,
		serverURL: srv.URL + "/v1.0",
	}
	return client
}

// testTransport rewrites Graph API URLs to point at the test server.
type testTransport struct {
	base      http.RoundTripper
	serverURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace https://graph.microsoft.com/v1.0 with test server URL
	if strings.HasPrefix(req.URL.String(), graphBaseURL) {
		newURL := strings.Replace(req.URL.String(), graphBaseURL, t.serverURL, 1)
		var err error
		req.URL, err = req.URL.Parse(newURL)
		if err != nil {
			return nil, err
		}
	}
	if t.base != nil {
		return t.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}
