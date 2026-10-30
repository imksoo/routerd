// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"testing"
)

func TestMalformedStoredVariableCannotBeReadOrOverwrittenAsUnknown(t *testing.T) {
	for _, raw := range []string{`{"value":{"status":"set","value":7,"since":"2026-10-29T00:00:00Z","updatedAt":"2026-10-29T00:00:00Z"}}`, `{"value":{"status":"set","value":"keep","since":"bad-time","updatedAt":"2026-10-29T00:00:00Z"}}`, `{"value":null}`, `null`, `{`} {
		t.Run(raw, func(t *testing.T) {
			store := mustOpenStore(t)
			defer store.Close()
			if _, err := store.Set("manual.mode", "keep", "fixture"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(`UPDATE objects SET status=? WHERE api_version=? AND kind=? AND name=?`, raw, stateAPIVersion, stateKind, "manual.mode"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ReadVariable("manual.mode"); err == nil {
				t.Fatal("malformed observation became an absent/unknown value")
			}
			if _, err := store.Set("manual.mode", "replace", "fixture"); err == nil {
				t.Fatal("corruption silently replaced by a successful write")
			}
			var got string
			if err := store.db.QueryRow(`SELECT status FROM objects WHERE api_version=? AND kind=? AND name=?`, stateAPIVersion, stateKind, "manual.mode").Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != raw {
				t.Fatalf("failed write destroyed source evidence: %q", got)
			}
		})
	}
}

func TestNullNativeReceiptCannotBecomeSuccessfulEmptyStatus(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	if err := store.SaveObjectStatus("routerd.net/v1alpha1", "Router", "sam-dataplane", map[string]any{"pendingProxyNeighbors": []string{"owned"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE objects SET status='null' WHERE name='sam-dataplane'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadObjectStatus("routerd.net/v1alpha1", "Router", "sam-dataplane"); err == nil {
		t.Fatal("null receipt was read as absent")
	}
	if err := store.SaveObjectStatus("routerd.net/v1alpha1", "Router", "sam-dataplane", map[string]any{}); err == nil {
		t.Fatal("null receipt was silently erased")
	}
	if _, err := store.ListObjectStatuses(); err == nil {
		t.Fatal("null receipt was listed as a successful observation")
	}
}

func TestNullDelegationAuthorityIsAReadFailure(t *testing.T) {
	for _, raw := range []string{`null`, `{"observation":null}`} {
		t.Run(raw, func(t *testing.T) {
			store := mustOpenStore(t)
			defer store.Close()
			if err := store.SaveObjectStatus("net.routerd.net/v1alpha1", "DHCPv6PrefixDelegation", "wan", map[string]any{"phase": "Pending"}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(`UPDATE objects SET status=? WHERE kind='DHCPv6PrefixDelegation'`, raw); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.ReadPDLease("wan"); err == nil {
				t.Fatal("malformed delegation authority treated as absent lease")
			}
			if _, err := store.ReadObjectStatus("net.routerd.net/v1alpha1", "DHCPv6PrefixDelegation", "wan"); err == nil {
				t.Fatal("malformed delegation authority became a successful status projection")
			}
		})
	}
}

func TestMalformedPersistedEventCannotBecomeAnEmptyMatch(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	if err := store.RecordEvent("v1", "Fixture", "event", "Normal", "Observed", "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE events SET attributes='{'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListEvents(EventQuery{}); err == nil {
		t.Fatal("broken attributes became a successful empty event")
	}
}
