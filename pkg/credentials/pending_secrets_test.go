package credentials

import (
	"testing"
)

func TestPendingVault_Extract_NoTags(t *testing.T) {
	v := NewPendingVault(nil)
	input := "connect to SSH 192.168.1.100 as root"
	got := v.Extract(input)
	if got != input {
		t.Errorf("Extract with no tags should return input unchanged, got %q", got)
	}
}

func TestPendingVault_Extract_SingleSecret(t *testing.T) {
	v := NewPendingVault(nil)
	got := v.Extract("password <<<hunter2>>>")
	want := "password <<<SECRET_1>>>"
	if got != want {
		t.Errorf("Extract = %q, want %q", got, want)
	}

	// Verify the secret was stored
	val, ok := v.Resolve("<<<SECRET_1>>>")
	if !ok || val != "hunter2" {
		t.Errorf("Resolve(SECRET_1) = %q, %v; want %q, true", val, ok, "hunter2")
	}
}

func TestPendingVault_Extract_MultipleSecrets(t *testing.T) {
	v := NewPendingVault(nil)
	got := v.Extract("user <<<root>>> pass <<<hunter2>>>")
	want := "user <<<SECRET_1>>> pass <<<SECRET_2>>>"
	if got != want {
		t.Errorf("Extract = %q, want %q", got, want)
	}

	val1, ok1 := v.Resolve("<<<SECRET_1>>>")
	val2, ok2 := v.Resolve("<<<SECRET_2>>>")
	if !ok1 || val1 != "root" {
		t.Errorf("SECRET_1 = %q, %v; want root, true", val1, ok1)
	}
	if !ok2 || val2 != "hunter2" {
		t.Errorf("SECRET_2 = %q, %v; want hunter2, true", val2, ok2)
	}
}

func TestPendingVault_Extract_DuplicateValue(t *testing.T) {
	v := NewPendingVault(nil)
	// Same value used twice should get the same token
	got := v.Extract("first <<<abc>>> second <<<abc>>>")
	want := "first <<<SECRET_1>>> second <<<SECRET_1>>>"
	if got != want {
		t.Errorf("Extract with duplicates = %q, want %q", got, want)
	}
}

func TestPendingVault_Extract_EmptyValue(t *testing.T) {
	v := NewPendingVault(nil)
	// Empty tags should be left as-is
	got := v.Extract("password <<<>>>")
	want := "password <<<>>>"
	if got != want {
		t.Errorf("Extract with empty = %q, want %q", got, want)
	}
}

func TestPendingVault_Extract_RegistersWithRedactor(t *testing.T) {
	r := NewRedactor()
	v := NewPendingVault(r)
	v.Extract("password <<<supersecretpassword>>>")

	// The redactor should now know about the raw value
	redacted := r.Redact("my password is supersecretpassword")
	if redacted == "my password is supersecretpassword" {
		t.Error("Redactor should have caught the transient secret")
	}
}

func TestPendingVault_Resolve_Unknown(t *testing.T) {
	v := NewPendingVault(nil)
	_, ok := v.Resolve("<<<SECRET_99>>>")
	if ok {
		t.Error("Resolve of unknown token should return false")
	}
}

func TestContainsPendingSecret(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"hello world", false},
		{"<<<SECRET_1>>>", true},
		{"prefix <<<SECRET_42>>> suffix", true},
		{"<<<SECRET_>>>", false},  // no number
		{"<<<secret_1>>>", false}, // lowercase
		{"<<<hunter2>>>", false},  // user tag, not internal token
	}

	for _, tt := range tests {
		if got := ContainsPendingSecret(tt.input); got != tt.want {
			t.Errorf("ContainsPendingSecret(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestPendingVault_SubstituteString(t *testing.T) {
	v := NewPendingVault(nil)
	v.Extract("<<<hunter2>>>")
	v.Extract("<<<token123>>>")

	got := v.SubstituteString("password: <<<SECRET_1>>>, token: <<<SECRET_2>>>")
	want := "password: hunter2, token: token123"
	if got != want {
		t.Errorf("SubstituteString = %q, want %q", got, want)
	}
}

func TestPendingVault_SubstituteString_NoTokens(t *testing.T) {
	v := NewPendingVault(nil)
	input := "no tokens here"
	got := v.SubstituteString(input)
	if got != input {
		t.Errorf("SubstituteString with no tokens should return unchanged, got %q", got)
	}
}

func TestPendingVault_SubstituteString_UnknownToken(t *testing.T) {
	v := NewPendingVault(nil)
	input := "password: <<<SECRET_99>>>"
	got := v.SubstituteString(input)
	if got != input {
		t.Errorf("Unknown token should be left as-is, got %q", got)
	}
}

func TestPendingVault_SubstituteAndRestore(t *testing.T) {
	v := NewPendingVault(nil)
	v.Extract("<<<hunter2>>>")

	args := map[string]any{
		"input":      "<<<SECRET_1>>>\n",
		"session_id": "abc",
	}

	restore := v.SubstituteAndRestore(args)

	// After substitution, tool should see real value
	if args["input"] != "hunter2\n" {
		t.Errorf("after substitute, input = %q, want %q", args["input"], "hunter2\n")
	}
	if args["session_id"] != "abc" {
		t.Errorf("session_id should be unchanged")
	}

	// Restore
	restore()

	if args["input"] != "<<<SECRET_1>>>\n" {
		t.Errorf("after restore, input = %q, want %q", args["input"], "<<<SECRET_1>>>\n")
	}
}

func TestPendingVault_SubstituteAndRestore_NilVault(t *testing.T) {
	var v *PendingVault
	args := map[string]any{"input": "<<<SECRET_1>>>"}
	restore := v.SubstituteAndRestore(args)
	restore() // should not panic
	if args["input"] != "<<<SECRET_1>>>" {
		t.Error("nil vault should leave args unchanged")
	}
}

func TestPendingVault_SubstituteAndRestore_NoTokens(t *testing.T) {
	v := NewPendingVault(nil)
	args := map[string]any{"key": "hello"}
	restore := v.SubstituteAndRestore(args)
	restore()
	if args["key"] != "hello" {
		t.Error("no tokens should leave args unchanged")
	}
}

func TestPendingVault_Extract_AcrossMultipleCalls(t *testing.T) {
	v := NewPendingVault(nil)

	// First message
	got1 := v.Extract("password <<<pass1>>>")
	if got1 != "password <<<SECRET_1>>>" {
		t.Errorf("first Extract = %q", got1)
	}

	// Second message — counter continues
	got2 := v.Extract("token <<<tok2>>>")
	if got2 != "token <<<SECRET_2>>>" {
		t.Errorf("second Extract = %q", got2)
	}

	// Both resolve
	v1, _ := v.Resolve("<<<SECRET_1>>>")
	v2, _ := v.Resolve("<<<SECRET_2>>>")
	if v1 != "pass1" || v2 != "tok2" {
		t.Errorf("Resolve: SECRET_1=%q, SECRET_2=%q", v1, v2)
	}
}
