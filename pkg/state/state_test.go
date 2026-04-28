package state

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestMigratePDLeasesMovesLegacyFieldsIntoLease(t *testing.T) {
	store := New()
	store.Set("ipv6PrefixDelegation.wan-pd.currentPrefix", "2001:db8:1200:1220::/60", "test")
	store.Set("ipv6PrefixDelegation.wan-pd.lastPrefix", "2001:db8:1200:1210::/60", "test")
	store.Set("ipv6PrefixDelegation.wan-pd.iaid", "0", "test")
	store.Set("ipv6PrefixDelegation.wan-pd.duidText", "00:03:00:01:02:00:5e:10:20:30", "test")
	store.Set("ipv6PrefixDelegation.wan-pd.uplinkIfname", "ens18", "test")

	if !MigratePDLeases(store) {
		t.Fatal("MigratePDLeases changed = false, want true")
	}
	lease, ok := DecodePDLease(store.Get("ipv6PrefixDelegation.wan-pd.lease").Value)
	if !ok {
		t.Fatal("lease was not written")
	}
	if lease.CurrentPrefix != "2001:db8:1200:1220::/60" || lease.LastPrefix != "2001:db8:1200:1210::/60" || lease.IAID != "0" {
		t.Fatalf("lease = %+v", lease)
	}
	if _, exists := store.Variables()["ipv6PrefixDelegation.wan-pd.lastPrefix"]; exists {
		t.Fatal("legacy lastPrefix key still exists")
	}
	if got := store.Get("ipv6PrefixDelegation.wan-pd.uplinkIfname").Value; got != "ens18" {
		t.Fatalf("uplinkIfname = %q, want preserved", got)
	}
}

func TestLoadMigratesPDLeases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	data := []byte(`{
  "variables": {
    "ipv6PrefixDelegation.wan-pd.lastPrefix": {
      "status": "set",
      "value": "2001:db8:1200:1210::/60",
      "since": "2026-04-28T00:00:00Z",
      "updatedAt": "2026-04-28T00:00:00Z"
    }
  }
}
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write state fixture: %v", err)
	}
	store, err := Load(path)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	lease, ok := DecodePDLease(store.Get("ipv6PrefixDelegation.wan-pd.lease").Value)
	if !ok || lease.LastPrefix != "2001:db8:1200:1210::/60" {
		t.Fatalf("lease = %+v ok=%v", lease, ok)
	}
	if _, exists := store.Variables()["ipv6PrefixDelegation.wan-pd.lastPrefix"]; exists {
		t.Fatal("legacy lastPrefix key still exists after Load")
	}
}
