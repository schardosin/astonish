package email

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/channels"
	emailpkg "github.com/SAP/astonish/pkg/email"
)

func TestRejectFlattenedAuthEvidence(t *testing.T) {
	msg := &emailpkg.Message{Headers: map[string]string{"From": "alice@example.com", "Authentication-Results": "mx.example.com; dmarc=pass header.from=example.com"}}
	if _, ok := authenticatedSender(msg, "mx.example.com"); ok {
		t.Fatal("flattened evidence authorized sender")
	}
}

func receiverCases() []struct {
	name       string
	from, auth []string
	want       bool
} {
	pass := "mx.example.com; dmarc=pass header.from=example.com"
	return []struct {
		name       string
		from, auth []string
		want       bool
	}{
		{"valid", []string{"Alice <alice@example.com>"}, []string{pass}, true},
		{"folded comments", []string{"Alice (owner) <ALICE@Example.com>"}, []string{"mx.example.com (receiver (nested));\r\n\tdkim=pass (good) header.d=example.com;\r\n dmarc=pass (p=NONE sp=NONE dis=NONE) header.from=Example.com"}, true},
		{"quoted property", []string{"alice@example.com"}, []string{`mx.example.com 1; dmarc/1=pass reason="aligned sender" header.from="example.com"`}, true},
		{"domain suffix", []string{"alice@example.com"}, []string{pass + ".evil.test"}, false},
		{"pass suffix", []string{"alice@example.com"}, []string{"mx.example.com; dmarc=passfake header.from=example.com"}, false},
		{"dot fake", []string{"alice@example.com"}, []string{"mx.example.com; dmarc=pass header.from.example.com"}, false},
		{"split method", []string{"alice@example.com"}, []string{"mx.example.com; dmarc=pass; spf=pass header.from=example.com"}, false},
		{"comment fake", []string{"alice@example.com"}, []string{"mx.example.com; dmarc=fail (dmarc=pass header.from=example.com)"}, false},
		{"untrusted receiver", []string{"alice@example.com"}, []string{"evil.test; dmarc=pass header.from=example.com"}, false},
		{"older forged", []string{"alice@example.com"}, []string{"mx.example.com; dmarc=fail header.from=example.com", pass}, false},
		{"duplicate trusted", []string{"alice@example.com"}, []string{pass, pass}, false},
		{"older trusted", []string{"alice@example.com"}, []string{"other.test; dmarc=fail header.from=example.com", pass}, false},
		{"older untrusted", []string{"alice@example.com"}, []string{pass, "other.test; dmarc=fail header.from=example.com"}, true},
		{"duplicate from", []string{"alice@example.com", "alice@example.com"}, []string{pass}, false},
		{"group from", []string{"Owners: alice@example.com;"}, []string{pass}, false},
		{"multi mailbox", []string{"alice@example.com, eve@evil.test"}, []string{pass}, false},
		{"bad comment", []string{"alice@example.com"}, []string{pass + " (unclosed"}, false},
		{"duplicate property", []string{"alice@example.com"}, []string{pass + " header.from=evil.test"}, false},
		{"duplicate method", []string{"alice@example.com"}, []string{pass + "; dmarc=pass header.from=example.com"}, false},
		{"bare newline", []string{"alice@example.com"}, []string{pass + "\n"}, false},
		{"token comment split", []string{"alice@example.com"}, []string{"mx.example.com; dmarc=pa(comment)ss header.from=example.com"}, false},
		{"quoted equals", []string{"alice@example.com"}, []string{`mx.example.com; dmarc "=" pass header.from=example.com`}, false},
		{"quoted separator", []string{"alice@example.com"}, []string{`mx.example.com ";" dmarc=pass header.from=example.com`}, false},
		{"quoted property equals", []string{"alice@example.com"}, []string{`mx.example.com; dmarc=pass header.from "=" example.com`}, false},
		{"missing evidence", nil, nil, false},
	}
}

func TestReceiverAuthenticationGrammar(t *testing.T) {
	for _, tc := range receiverCases() {
		t.Run(tc.name, func(t *testing.T) {
			msg := &emailpkg.Message{HeaderValues: map[string][]string{"From": tc.from, "Authentication-Results": tc.auth}}
			addr, ok := authenticatedSender(msg, "mx.example.com")
			if ok != tc.want || ok && addr != "alice@example.com" {
				t.Fatalf("sender=%q ok=%v want=%v", addr, ok, tc.want)
			}
		})
	}
}

type pollingClient struct {
	emailpkg.Client
	msg    *emailpkg.Message
	marked int
}

func (c *pollingClient) ListMessages(context.Context, emailpkg.ListOpts) ([]emailpkg.MessageSummary, error) {
	return []emailpkg.MessageSummary{{ID: "1", From: "forged-summary@evil.test"}}, nil
}
func (c *pollingClient) ReadMessage(context.Context, string) (*emailpkg.Message, error) {
	return c.msg, nil
}
func (c *pollingClient) MarkRead(context.Context, []string) error { c.marked++; return nil }

func TestCheckNewEmailsReceiverTrust(t *testing.T) {
	for _, tc := range receiverCases() {
		t.Run(tc.name, func(t *testing.T) {
			ch := New(&Config{Address: "bot@example.com", AllowFrom: []string{"alice@example.com"}, TrustedAuthservID: "mx.example.com", MarkRead: true}, log.New(&bytes.Buffer{}, "", 0))
			client := &pollingClient{msg: &emailpkg.Message{From: "forged-presentation@evil.test", Body: "hello", Headers: map[string]string{"Message-ID": "<1@example.com>"}, HeaderValues: map[string][]string{"From": tc.from, "Authentication-Results": tc.auth}}}
			ch.client = client
			calls := 0
			ch.handler = func(_ context.Context, in channels.InboundMessage) error {
				calls++
				if in.SenderID != "alice@example.com" || in.ChatID != "alice@example.com" || in.ThreadID != "" {
					t.Fatalf("sender ownership: %#v", in)
				}
				return nil
			}
			ch.checkNewEmails(context.Background())
			want := 0
			if tc.want {
				want = 1
			}
			if calls != want || client.marked != want || ch.msgCount.Load() != int64(want) {
				t.Fatalf("dispatch=%d mark=%d count=%d want=%d", calls, client.marked, ch.msgCount.Load(), want)
			}
		})
	}
}

func TestSenderAuthStartup(t *testing.T) {
	for _, id := range []string{"", "mx;evil", " mx.example.com"} {
		ch := New(&Config{TrustedAuthservID: id}, log.Default())
		err := ch.Start(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "trusted_authserv_id") {
			t.Fatalf("missing actionable trust error: %v", err)
		}
	}
	var logs bytes.Buffer
	disabled := false
	ch := New(&Config{Provider: "invalid", RequireSenderAuth: &disabled}, log.New(&logs, "", 0))
	_ = ch.Start(context.Background(), nil)
	if !strings.Contains(logs.String(), "WARNING") || !strings.Contains(logs.String(), "spoofed") {
		t.Fatal("explicit false must warn")
	}

	logs.Reset()
	ch = New(&Config{Provider: "invalid", SenderAuthMode: "provider-trust"}, log.New(&logs, "", 0))
	_ = ch.Start(context.Background(), nil)
	if !strings.Contains(logs.String(), "WARNING") || !strings.Contains(logs.String(), "provider-trust") {
		t.Fatal("provider-trust must warn")
	}
}

func TestProviderTrustedSender(t *testing.T) {
	addr, ok := providerTrustedSender(&emailpkg.Message{HeaderValues: map[string][]string{"From": {"Alice <alice@example.com>"}}})
	if !ok || addr != "alice@example.com" {
		t.Fatalf("valid From rejected: addr=%q ok=%v", addr, ok)
	}
	if _, ok := providerTrustedSender(&emailpkg.Message{HeaderValues: map[string][]string{"From": {"alice@example.com", "eve@evil.test"}}}); ok {
		t.Fatal("multi-From must fail closed")
	}
	if _, ok := providerTrustedSender(&emailpkg.Message{}); ok {
		t.Fatal("missing From must fail closed")
	}
}

func TestCheckNewEmailsProviderTrust(t *testing.T) {
	ch := New(&Config{Address: "bot@example.com", AllowFrom: []string{"alice@example.com"}, SenderAuthMode: "provider-trust", MarkRead: true}, log.New(&bytes.Buffer{}, "", 0))
	client := &pollingClient{msg: &emailpkg.Message{From: "forged-presentation@evil.test", Body: "hello", Headers: map[string]string{"Message-ID": "<1@example.com>"}, HeaderValues: map[string][]string{"From": {"Alice <alice@example.com>"}}}}
	ch.client = client
	calls := 0
	ch.handler = func(_ context.Context, in channels.InboundMessage) error {
		calls++
		if in.SenderID != "alice@example.com" {
			t.Fatalf("sender ownership: %#v", in)
		}
		return nil
	}
	ch.checkNewEmails(context.Background())
	if calls != 1 || client.marked != 1 {
		t.Fatalf("provider-trust should dispatch unambiguous From without DMARC evidence: dispatch=%d mark=%d", calls, client.marked)
	}
}
