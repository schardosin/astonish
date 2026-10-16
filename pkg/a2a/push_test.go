package a2a

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func publicTestNotifier(t *testing.T, server *httptest.Server) *PushNotifier {
	t.Helper()
	notifier := NewPushNotifier(nil)
	notifier.baseDelay = time.Millisecond
	notifier.resolveHost = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	notifier.client = server.Client()
	notifier.client.Timeout = 30 * time.Second
	notifier.client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return notifier
}

func TestIsPublicPushIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "zero start", ip: "0.0.0.0", want: false},
		{name: "zero interior", ip: "0.1.2.3", want: false},
		{name: "zero end", ip: "0.255.255.255", want: false},
		{name: "after zero", ip: "1.0.0.0", want: true},
		{name: "before benchmark", ip: "198.17.255.255", want: true},
		{name: "benchmark start", ip: "198.18.0.0", want: false},
		{name: "benchmark interior", ip: "198.19.1.1", want: false},
		{name: "benchmark end", ip: "198.19.255.255", want: false},
		{name: "after benchmark", ip: "198.20.0.0", want: true},
		{name: "before NAT64", ip: "64:ff9a:ffff:ffff:ffff:ffff:ffff:ffff", want: true},
		{name: "NAT64 start", ip: "64:ff9b::", want: false},
		{name: "NAT64 loopback", ip: "64:ff9b::7f00:1", want: false},
		{name: "NAT64 public IPv4", ip: "64:ff9b::808:808", want: false},
		{name: "NAT64 end", ip: "64:ff9b::ffff:ffff", want: false},
		{name: "after NAT64", ip: "64:ff9b::1:0:0", want: true},
		{name: "before CGNAT range", ip: "100.63.255.255", want: true},
		{name: "CGNAT range start", ip: "100.64.0.0", want: false},
		{name: "CGNAT range interior", ip: "100.100.100.100", want: false},
		{name: "CGNAT range end", ip: "100.127.255.255", want: false},
		{name: "after CGNAT range", ip: "100.128.0.0", want: true},
	}

	for _, tt := range tests {
		if net.ParseIP(tt.ip).To4() != nil {
			tests = append(tests, struct {
				name, ip string
				want     bool
			}{tt.name + " mapped", "::ffff:" + tt.ip, tt.want})
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if got := isPublicPushIP(ip); got != tt.want {
				t.Errorf("isPublicPushIP(%s) = %t, want %t", tt.ip, got, tt.want)
			}
			resolver := func(context.Context, string) ([]net.IP, error) { return []net.IP{ip}, nil }
			notifier := NewPushNotifier(nil)
			notifier.resolveHost = resolver
			if err := notifier.ValidatePushURL(context.Background(), "https://webhook.example/hook"); (err == nil) != tt.want {
				t.Errorf("ValidatePushURL resolver %s: %v, want public=%t", tt.ip, err, tt.want)
			}
			if !tt.want {
				// Exercise the actual DialContext with a live context as well as a
				// canceled one; both must fail on policy before any network dial.
				for _, canceled := range []bool{false, true} {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					if canceled {
						cancel()
					}
					transport := safePushTransport(resolver)
					conn, err := transport.DialContext(ctx, "tcp", "webhook.example:443")
					cancel()
					if conn != nil {
						conn.Close()
						t.Error("unexpected connection")
					}
					if err == nil || !strings.Contains(err.Error(), "non-public") {
						t.Errorf("DialContext resolver %s (canceled=%t): %v, want non-public rejection", tt.ip, canceled, err)
					}
				}
			}
		})
	}
}

func TestPushNotifier_Success(t *testing.T) {
	var received []byte
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		received = buf
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := publicTestNotifier(t, server)
	cfg := &PushNotificationConfig{URL: server.URL, Token: "test-token"}
	event := TaskStatusUpdateEvent{TaskID: "task-123", Status: TaskStatus{State: TaskStateCompleted, Timestamp: time.Now()}}
	if err := notifier.NotifyStatusUpdate(cfg, event); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	var got TaskStatusUpdateEvent
	if err := json.Unmarshal(received, &got); err == nil && got.TaskID != "task-123" {
		t.Fatalf("expected taskId 'task-123', got %q", got.TaskID)
	}
}

func TestPushNotifier_NilConfig(t *testing.T) {
	if err := NewPushNotifier(nil).NotifyStatusUpdate(nil, TaskStatusUpdateEvent{}); err != nil {
		t.Fatalf("expected nil for nil config, got: %v", err)
	}
}

func TestPushNotifier_Retries(t *testing.T) {
	attempts := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := publicTestNotifier(t, server)
	if err := notifier.NotifyStatusUpdate(&PushNotificationConfig{URL: server.URL}, TaskStatusUpdateEvent{TaskID: "t1"}); err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestPushNotifierRejectsNonPublicAndNonHTTPSURLs(t *testing.T) {
	notifier := NewPushNotifier(nil)
	notifier.resolveHost = func(_ context.Context, host string) ([]net.IP, error) {
		if host == "localhost" {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		}
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	for _, rawURL := range []string{"http://example.com/hook", "https://localhost/hook", "https://user@example.com/hook"} {
		if err := notifier.ValidatePushURL(context.Background(), rawURL); err == nil {
			t.Fatalf("expected %q to be rejected", rawURL)
		}
	}
	notifier.resolveHost = func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("127.0.0.1")}, nil }
	if err := notifier.ValidatePushURL(context.Background(), "https://example.com/hook"); err == nil {
		t.Fatal("expected loopback target to be rejected")
	}
}

func TestSafePushTransportRejectsNonPublicAddressFromResolver(t *testing.T) {
	transport := safePushTransport(func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	})

	conn, err := transport.DialContext(context.Background(), "tcp", "example.com:443")
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatal("expected dial-time resolver result to reject a loopback address")
	}
}

func TestPushTransportRechecksDNSAndPinsDial(t *testing.T) {
	answers := []net.IP{net.ParseIP("93.184.216.34")}
	resolver := func(context.Context, string) ([]net.IP, error) { return answers, nil }
	notifier := NewPushNotifier(nil)
	notifier.resolveHost = resolver
	if err := notifier.ValidatePushURL(context.Background(), "https://webhook.example/hook"); err != nil {
		t.Fatal(err)
	}
	transport := safePushTransport(resolver)
	for _, ip := range []string{"0.1.2.3", "198.19.1.1", "64:ff9b::808:808", "127.0.0.1", "100.64.0.1"} {
		answers = []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP(ip)}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		conn, err := transport.DialContext(ctx, "tcp", "webhook.example:443")
		cancel()
		if conn != nil {
			conn.Close()
			t.Fatal("unexpected connection")
		}
		if err == nil || !strings.Contains(err.Error(), "non-public") {
			t.Fatalf("rebound/mixed answer %s: %v", ip, err)
		}
	}
	answers = []net.IP{net.ParseIP("93.184.216.34")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := transport.DialContext(ctx, "tcp", "webhook.example:443")
	if err == nil || !strings.Contains(err.Error(), "93.184.216.34:443") {
		t.Fatalf("dial was not pinned to resolved literal IP: %v", err)
	}
}

func TestPushNotifierRejectsRedirects(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, nil, "https://example.com/next", http.StatusFound)
	}))
	defer server.Close()
	notifier := publicTestNotifier(t, server)
	if err := notifier.NotifyStatusUpdate(&PushNotificationConfig{URL: server.URL}, TaskStatusUpdateEvent{}); err == nil {
		t.Fatal("expected redirect to be rejected")
	}
}
