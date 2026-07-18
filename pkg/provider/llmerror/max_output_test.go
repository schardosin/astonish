package llmerror

import "testing"

func TestParseMaxOutputTokensLimit_ExclusiveRange(t *testing.T) {
	t.Parallel()
	body := `{"error":{"code":400,"message":"Unable to submit request because it has a maxOutputTokens value of 64000 but the supported range is from 1 (inclusive) to 32769 (exclusive). Update the value and try again.","status":"INVALID_ARGUMENT"}}`
	got, ok := ParseMaxOutputTokensLimit(body)
	if !ok {
		t.Fatal("expected parse success")
	}
	if got != 32768 {
		t.Errorf("got %d, want 32768", got)
	}
}

func TestParseMaxOutputTokensLimit_InclusiveUpper(t *testing.T) {
	t.Parallel()
	body := `maxOutputTokens value of 100000 but the supported range is from 1 (inclusive) to 8192 (inclusive)`
	got, ok := ParseMaxOutputTokensLimit(body)
	if !ok {
		t.Fatal("expected parse success")
	}
	if got != 8192 {
		t.Errorf("got %d, want 8192", got)
	}
}

func TestParseMaxOutputTokensLimit_Unrelated400(t *testing.T) {
	t.Parallel()
	body := `{"error":{"message":"Unknown name \"additionalProperties\" at 'tools'"}}`
	if _, ok := ParseMaxOutputTokensLimit(body); ok {
		t.Error("expected parse failure for unrelated 400")
	}
}
