package fleet

import "testing"

func TestIsBundledKey(t *testing.T) {
	t.Parallel()
	if !IsBundledKey("software-dev") {
		t.Fatal("expected software-dev to be a bundled key")
	}
	if IsBundledKey("my-custom-fleet") {
		t.Fatal("expected my-custom-fleet not to be bundled")
	}
}

func TestBundledKeys(t *testing.T) {
	t.Parallel()
	keys := BundledKeys()
	if _, ok := keys["software-dev"]; !ok {
		t.Fatal("BundledKeys missing software-dev")
	}
}
