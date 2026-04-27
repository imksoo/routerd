package state

import "testing"

func TestSetEmptyNormalizesToUnset(t *testing.T) {
	store := New()
	got := store.Set("wan.ipv6.pd", "", "no prefix")
	if got.Status != StatusUnset {
		t.Fatalf("status = %s, want %s", got.Status, StatusUnset)
	}
	if got.Value != "" {
		t.Fatalf("value = %q, want empty", got.Value)
	}
}

func TestUnknownIsDefaultAndForget(t *testing.T) {
	store := New()
	if got := store.Get("missing"); got.Status != StatusUnknown {
		t.Fatalf("missing status = %s, want %s", got.Status, StatusUnknown)
	}
	store.Set("wan.ipv6.mode", "pd-ready", "test")
	got := store.Forget("wan.ipv6.mode", "reset")
	if got.Status != StatusUnknown {
		t.Fatalf("forgotten status = %s, want %s", got.Status, StatusUnknown)
	}
}
