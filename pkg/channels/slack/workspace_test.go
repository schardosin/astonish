package slack

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/SAP/astonish/pkg/channels"
	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

func TestSlackConfiguredBareAllowlistDefaultWorkspaceOnly(t *testing.T) {
	ch := New(&Config{AllowFrom: []string{"U123"}}, log.Default())
	ch.defaultTeamID = "T123"
	for _, teamID := range []string{"T123", "T456", ""} {
		if got := ch.isAllowedSender("U123", teamID); got != (teamID == "T123") {
			t.Errorf("team=%q allowed=%v", teamID, got)
		}
	}
	if teamID, destination, err := ch.parseTarget("U123"); err != nil || teamID != "T123" || destination != "U123" {
		t.Fatalf("legacy configured target=%s/%s err=%v", teamID, destination, err)
	}
}

func TestSlackQualifiedDelivery(t *testing.T) {
	for _, member := range []string{"U123", "W123"} {
		t.Run(member, func(t *testing.T) {
			var mu sync.Mutex
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				mu.Lock()
				requests = append(requests, r.URL.Path+" "+r.Form.Get("token")+" "+r.Form.Get("users")+" "+r.Form.Get("channel"))
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/conversations.open":
					fmt.Fprint(w, `{"ok":true,"channel":{"id":"D123"}}`)
				case "/chat.postMessage":
					fmt.Fprint(w, `{"ok":true,"channel":"D123","ts":"1.000001"}`)
				default:
					t.Errorf("unexpected API: %s", r.URL.Path)
					fmt.Fprint(w, `{"ok":false,"error":"unexpected"}`)
				}
			}))
			defer server.Close()
			ch := New(&Config{AllowFrom: []string{"T123:" + member}}, log.Default())
			ch.api = slackapi.New("wrong-default", slackapi.OptionAPIURL(server.URL+"/"))
			ch.workspaces = map[string]*slackapi.Client{
				"T123": slackapi.New("right-token", slackapi.OptionAPIURL(server.URL+"/")),
				"T456": slackapi.New("wrong-team", slackapi.OptionAPIURL(server.URL+"/")),
			}
			targets := ch.BroadcastTargets()
			if len(targets) != 1 {
				t.Fatalf("targets = %v", targets)
			}
			if err := ch.Send(context.Background(), targets[0], channels.OutboundMessage{Text: "scheduled result"}); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			want := []string{"/conversations.open right-token " + member + " ", "/chat.postMessage right-token  D123"}
			if !reflect.DeepEqual(requests, want) {
				t.Fatalf("requests = %q, want %q", requests, want)
			}
		})
	}
}

func TestSlackWorkspaceIsolation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unsafe target made API request %s", r.URL.Path)
		fmt.Fprint(w, `{"ok":true,"channel":"D123","ts":"1.000001"}`)
	}))
	defer server.Close()
	ch := New(&Config{}, log.Default())
	ch.api = slackapi.New("default", slackapi.OptionAPIURL(server.URL+"/"))
	ch.workspaces = map[string]*slackapi.Client{"T123": ch.api, "T456": ch.api}
	for _, target := range []string{"T999:U123", "T999:D123", "U123", "D123", "T123:U123:extra"} {
		if err := ch.Send(context.Background(), channels.Target{ChatID: target}, channels.OutboundMessage{Text: "secret"}); err == nil {
			t.Errorf("unsafe target %s accepted", target)
		}
	}
	if ch.getAPIForTeam("T999") != nil {
		t.Error("unknown workspace fell back to default client")
	}
}

func TestSlackInboundReplyWorkspace(t *testing.T) {
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("token") != "right-token" {
			t.Errorf("wrong workspace token: %s", r.Form.Get("token"))
		}
		if r.URL.Path == "/users.info" {
			fmt.Fprint(w, `{"ok":true,"user":{"id":"U123"}}`)
			return
		}
		if r.URL.Path != "/chat.postMessage" || r.Form.Get("channel") != "D123" {
			t.Errorf("unexpected reply: %s %v", r.URL.Path, r.Form)
		}
		posts++
		fmt.Fprint(w, `{"ok":true,"channel":"D123","ts":"1.000001"}`)
	}))
	defer server.Close()
	ch := New(&Config{AllowFrom: []string{"T123:U123"}}, log.Default())
	ch.workspaces = map[string]*slackapi.Client{"T123": slackapi.New("right-token", slackapi.OptionAPIURL(server.URL+"/"))}
	ch.handler = func(ctx context.Context, msg channels.InboundMessage) error {
		if msg.SenderID != "T123:U123" || msg.ChatID != "T123:D123" {
			t.Errorf("lost workspace: %+v", msg)
		}
		return ch.Send(ctx, channels.Target{ChatID: msg.ChatID, ThreadID: msg.ThreadID}, channels.OutboundMessage{Text: "reply"})
	}
	ch.handleMessage(context.Background(), &slackevents.MessageEvent{User: "U123", Channel: "D123", ChannelType: "im", Text: "hello", TimeStamp: "1.000001"}, "T123")
	if posts != 1 {
		t.Fatalf("reply posts = %d", posts)
	}
}
