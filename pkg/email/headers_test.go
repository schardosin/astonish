package email

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestGraphPreservesSecurityHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("$select"), "internetMessageHeaders") {
			t.Error("ReadMessage must explicitly select internetMessageHeaders")
		}
		json.NewEncoder(w).Encode(graphMessage{ID: "1", InternetMessageHeaders: []graphHeader{
			{Name: "From", Value: "first@example.com"}, {Name: "from", Value: "second@example.com"},
			{Name: "Authentication-Results", Value: "mx; dmarc=fail"}, {Name: "authentication-results", Value: "mx; dmarc=pass"},
		}})
	}))
	defer srv.Close()
	msg, err := newTestClient(srv, "token").ReadMessage(context.Background(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(msg.HeaderValues["From"], []string{"first@example.com", "second@example.com"}) {
		t.Errorf("From values: %#v", msg.HeaderValues)
	}
	if !reflect.DeepEqual(msg.HeaderValues["Authentication-Results"], []string{"mx; dmarc=fail", "mx; dmarc=pass"}) {
		t.Errorf("auth values: %#v", msg.HeaderValues)
	}
	if msg.Headers["From"] != "first@example.com" || msg.Headers["Authentication-Results"] != "mx; dmarc=fail" {
		t.Errorf("legacy headers must keep first value: %#v", msg.Headers)
	}
}
