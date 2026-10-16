package channels

import "testing"

func TestLinkIdentityBinding(t *testing.T) {
	for _, tt := range []struct {
		channel   LinkChannel
		identity  LinkIdentity
		principal string
		valid     bool
	}{
		{LinkTelegram, LinkIdentity{UserID: "12345"}, "12345", true},
		{LinkTelegram, LinkIdentity{UserID: "0123"}, "", false},
		{LinkTelegram, LinkIdentity{UserID: "-1"}, "", false},
		{LinkTelegram, LinkIdentity{UserID: "123", WorkspaceID: "T123"}, "", false},
		{LinkSlack, LinkIdentity{UserID: "U123", WorkspaceID: "T123"}, "T123:U123", true},
		{LinkSlack, LinkIdentity{UserID: "W8VGRP0S3"}, "W8VGRP0S3", true},
		{LinkSlack, LinkIdentity{UserID: "U123"}, "U123", true},
		{LinkSlack, LinkIdentity{UserID: "U123", WorkspaceID: "E7RBBBXHB"}, "", false},
		{LinkSlack, LinkIdentity{UserID: "U123:evil", WorkspaceID: "T123"}, "", false},
	} {
		got, err := tt.identity.Principal(tt.channel)
		if (err == nil) != tt.valid || got != tt.principal {
			t.Fatalf("%+v got %q, %v", tt, got, err)
		}
		if !tt.valid {
			continue
		}
		binding, err := LinkBinding(tt.channel, tt.identity)
		if err != nil || !MatchesLinkBinding(binding, tt.channel, got) {
			t.Fatalf("binding %q: %v", binding, err)
		}
		if MatchesLinkBinding(string(tt.channel), tt.channel, got) || MatchesLinkBinding(binding, tt.channel, got+"X") {
			t.Fatal("unbound/wrong principal accepted")
		}
	}
	memberBinding, err := LinkBinding(LinkSlack, LinkIdentity{UserID: "W8VGRP0S3"})
	if err != nil || !MatchesLinkBinding(memberBinding, LinkSlack, "T07CKTNKX:W8VGRP0S3") {
		t.Fatalf("member-only binding must match Slack's workspace-qualified event: %v %q", err, memberBinding)
	}
	if MatchesLinkBinding(memberBinding, LinkSlack, "T07CKTNKX:UOTHER") {
		t.Fatal("member-only binding accepted a different member")
	}
	qualifiedBinding, err := LinkBinding(LinkSlack, LinkIdentity{UserID: "U123", WorkspaceID: "T123"})
	if err != nil || MatchesLinkBinding(qualifiedBinding, LinkSlack, "T456:U123") {
		t.Fatalf("qualified binding must stay workspace-scoped: %v", err)
	}
}
