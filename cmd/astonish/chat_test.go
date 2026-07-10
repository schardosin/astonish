package astonish

import (
	"context"
	"strings"
	"testing"
)

type pinCall struct {
	SessionID string
	Provider  string
	Model     string
}

type fakePinStore struct {
	calls []pinCall
	err   error
}

func (f *fakePinStore) SetSessionPin(_ context.Context, sessionID, provider, model string) error {
	f.calls = append(f.calls, pinCall{SessionID: sessionID, Provider: provider, Model: model})
	return f.err
}

// runFlagsThroughPipeline exercises the same parse → plan → apply pipeline
// that handleChatCommand uses, but without spinning up the console. It is
// the seam the plan text calls out: "asserts on state (pin written / not
// written / cleared) instead of stdout strings."
//
// For new-session pins (pinActionPin), the sessionID comes from a fake
// (mirrors what RunChatConsole would produce via OnSessionCreated). For
// clear/no-action, the resumed ID from --resume is used.
func runFlagsThroughPipeline(t *testing.T, args []string, store SessionPinStore, newSessionID string) error {
	t.Helper()
	flags, err := parseChatFlags(args)
	if err != nil {
		return err
	}
	plan, err := planChatFlags(flags)
	if err != nil {
		return err
	}
	ctx := context.Background()

	switch plan.Action {
	case pinActionClear:
		return applyPinAction(ctx, store, plan, flags.Resume)
	case pinActionPin:
		return applyPinAction(ctx, store, plan, newSessionID)
	case pinActionNone:
		return nil
	default:
		t.Fatalf("unexpected plan action: %d", plan.Action)
		return nil
	}
}

func TestChatFlags_PinByDefault_NewSession(t *testing.T) {
	fake := &fakePinStore{}
	restore := SetChatPinStoreForTest(fake)
	t.Cleanup(restore)

	const newID = "sess-new-abc"
	if err := runFlagsThroughPipeline(t, []string{"-p", "openai", "-m", "gpt-4o"}, fake, newID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("expected exactly 1 SetSessionPin call, got %d: %+v", len(fake.calls), fake.calls)
	}
	got := fake.calls[0]
	want := pinCall{SessionID: newID, Provider: "openai", Model: "gpt-4o"}
	if got != want {
		t.Errorf("SetSessionPin call mismatch:\n  got:  %+v\n  want: %+v", got, want)
	}
}

func TestChatFlags_NoPin(t *testing.T) {
	fake := &fakePinStore{}
	restore := SetChatPinStoreForTest(fake)
	t.Cleanup(restore)

	if err := runFlagsThroughPipeline(t, []string{"-p", "openai", "-m", "gpt-4o", "--no-pin"}, fake, "sess-new-abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected 0 SetSessionPin calls with --no-pin, got %d: %+v", len(fake.calls), fake.calls)
	}
}

func TestChatFlags_ResumeOverride(t *testing.T) {
	fake := &fakePinStore{}
	restore := SetChatPinStoreForTest(fake)
	t.Cleanup(restore)

	flags, err := parseChatFlags([]string{"--resume", "sess-existing-123", "-m", "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if flags.Model != "gpt-4o-mini" {
		t.Errorf("expected Model=gpt-4o-mini for this-run override, got %q", flags.Model)
	}
	if flags.Resume != "sess-existing-123" {
		t.Errorf("expected Resume=sess-existing-123, got %q", flags.Resume)
	}

	if err := runFlagsThroughPipeline(t, []string{"--resume", "sess-existing-123", "-m", "gpt-4o-mini"}, fake, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected 0 SetSessionPin calls on --resume -m X (ephemeral override), got %d: %+v", len(fake.calls), fake.calls)
	}
}

func TestChatFlags_ClearModel(t *testing.T) {
	fake := &fakePinStore{}
	restore := SetChatPinStoreForTest(fake)
	t.Cleanup(restore)

	const resumedID = "sess-resumed-xyz"
	if err := runFlagsThroughPipeline(t, []string{"--resume", resumedID, "--clear-model"}, fake, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("expected exactly 1 SetSessionPin call, got %d: %+v", len(fake.calls), fake.calls)
	}
	got := fake.calls[0]
	want := pinCall{SessionID: resumedID, Provider: "", Model: ""}
	if got != want {
		t.Errorf("SetSessionPin clear call mismatch:\n  got:  %+v\n  want: %+v", got, want)
	}
}

func TestChatFlags_ClearModelWithoutResume(t *testing.T) {
	fake := &fakePinStore{}
	restore := SetChatPinStoreForTest(fake)
	t.Cleanup(restore)

	err := runFlagsThroughPipeline(t, []string{"--clear-model"}, fake, "")
	if err == nil {
		t.Fatal("expected error for --clear-model without --resume, got nil")
	}
	if !strings.Contains(err.Error(), "requires --resume") {
		t.Errorf("expected error containing 'requires --resume', got: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected 0 SetSessionPin calls when validation fails, got %d: %+v", len(fake.calls), fake.calls)
	}
}

func TestParseModelPin(t *testing.T) {
	cases := []struct {
		name         string
		in           string
		wantProvider string
		wantModel    string
		wantErr      bool
	}{
		{"empty clears", "", "", "", false},
		{"simple", "openai:gpt-4o", "openai", "gpt-4o", false},
		{"model with colon", "openai:gpt-4o:2024-08-06", "openai", "gpt-4o:2024-08-06", false},
		{"empty model after colon", "openai:", "openai", "", false},
		{"empty provider before colon", ":gpt-4o", "", "gpt-4o", false},
		{"no colon errors", "invalid", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, m, err := parseModelPin(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tc.in)
				}
				if !strings.Contains(err.Error(), "expected provider:model") {
					t.Errorf("expected error containing 'expected provider:model', got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if p != tc.wantProvider || m != tc.wantModel {
				t.Errorf("parseModelPin(%q) = (%q, %q), want (%q, %q)", tc.in, p, m, tc.wantProvider, tc.wantModel)
			}
		})
	}
}

func TestHandleChatModelCommand_MissingColon(t *testing.T) {
	fake := &fakePinStore{}
	restore := SetChatPinStoreForTest(fake)
	t.Cleanup(restore)

	err := handleChatModelCommand([]string{"invalid"})
	if err == nil {
		t.Fatal("expected error for missing colon, got nil")
	}
	if !strings.Contains(err.Error(), "expected provider:model") {
		t.Errorf("expected error containing 'expected provider:model', got: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected 0 SetSessionPin calls on parse error, got %d: %+v", len(fake.calls), fake.calls)
	}
}

func TestHandleChatModelCommand_NoArgs(t *testing.T) {
	fake := &fakePinStore{}
	restore := SetChatPinStoreForTest(fake)
	t.Cleanup(restore)

	err := handleChatModelCommand([]string{})
	if err == nil {
		t.Fatal("expected error for missing positional arg, got nil")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestHandleChatModelCommand_ExplicitSession_PersonalWithStore(t *testing.T) {
	fake := &fakePinStore{}
	restore := SetChatPinStoreForTest(fake)
	t.Cleanup(restore)

	if err := handleChatModelCommand([]string{"--session", "sess-123", "anthropic:claude-sonnet-4"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("expected 1 SetSessionPin call, got %d: %+v", len(fake.calls), fake.calls)
	}
	got := fake.calls[0]
	want := pinCall{SessionID: "sess-123", Provider: "anthropic", Model: "claude-sonnet-4"}
	if got != want {
		t.Errorf("SetSessionPin mismatch:\n  got:  %+v\n  want: %+v", got, want)
	}
}

func TestHandleChatModelCommand_ExplicitSession_ClearsPin(t *testing.T) {
	fake := &fakePinStore{}
	restore := SetChatPinStoreForTest(fake)
	t.Cleanup(restore)

	if err := handleChatModelCommand([]string{"--session", "sess-456", ""}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("expected 1 SetSessionPin call, got %d: %+v", len(fake.calls), fake.calls)
	}
	got := fake.calls[0]
	want := pinCall{SessionID: "sess-456", Provider: "", Model: ""}
	if got != want {
		t.Errorf("SetSessionPin clear mismatch:\n  got:  %+v\n  want: %+v", got, want)
	}
}

func TestHandleChatModelCommand_NoStore(t *testing.T) {
	restore := SetChatPinStoreForTest(nil)
	t.Cleanup(restore)

	err := handleChatModelCommand([]string{"--session", "sess-789", "openai:gpt-4o"})
	if err == nil {
		t.Fatal("expected error when chatPinStore is nil, got nil")
	}
	if !strings.Contains(err.Error(), "pin store not available") {
		t.Errorf("expected 'pin store not available' error, got: %v", err)
	}
}
