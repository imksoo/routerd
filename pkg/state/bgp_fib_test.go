// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgp"
)

func fixtureFIBJournal() bgp.FIBJournal {
	return bgp.FIBJournal{KernelScope: "linux:fixture:network", Applied: []bgp.NativeFIBRoute{{Prefix: "10.77.60.11/32", NextHops: []bgp.FIBNextHop{{Gateway: "192.0.2.1", InterfaceIndex: 7}}, Table: 254, Protocol: 186, Metric: 200}}}
}
func TestBGPFIBJournalTypedPersistenceAndRejectedWritePreserveReceipts(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	if _, found, err := store.ReadBGPFIB(t.Context(), fixtureFIBJournal().KernelScope); err != nil || found {
		t.Fatalf("missing journal=%v %v", found, err)
	}
	original := fixtureFIBJournal()
	if err := store.SaveBGPFIB(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	got, found, err := store.ReadBGPFIB(t.Context(), fixtureFIBJournal().KernelScope)
	if err != nil || !found || !bgp.EqualNativeFIBRoute(got.Applied[0], original.Applied[0]) {
		t.Fatalf("typed journal roundtrip=%+v %v %v", got, found, err)
	}
	got.Applied[0].NextHops[0].InterfaceIndex = 0
	if err := store.SaveBGPFIB(t.Context(), got); err == nil {
		t.Fatal("unusable native identity was persisted")
	}
	reread, _, err := store.ReadBGPFIB(t.Context(), fixtureFIBJournal().KernelScope)
	if err != nil || reread.Applied[0].NextHops[0].InterfaceIndex != 7 {
		t.Fatalf("rejected write changed ownership: %+v %v", reread, err)
	}
}
func TestBGPFIBJournalCorruptReceiptIsUnknownRatherThanEmpty(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	key, err := bgpFIBJournalName(fixtureFIBJournal().KernelScope)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`null`, `{"kernelScope":"linux:fixture:network","applied":null}`, `{"kernelScope":"linux:fixture:network","applied":[{"prefix":"10.77.60.11/32","nextHops":[{"gateway":"192.0.2.1","interfaceIndex":0}],"table":254,"protocol":186,"metric":200}]}`} {
		if err := store.SaveBGPFIB(t.Context(), fixtureFIBJournal()); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec("UPDATE objects SET status=? WHERE api_version=? AND kind='Router' AND name=?", raw, api.NetAPIVersion, key); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.ReadBGPFIB(t.Context(), fixtureFIBJournal().KernelScope); err == nil {
			t.Fatalf("corrupt FIB receipt became empty ownership: %s", raw)
		}
	}
}
func TestBGPFIBJournalClosedStoreAndCanceledReadPreserveErrors(t *testing.T) {
	store := mustOpenStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := store.ReadBGPFIB(ctx, fixtureFIBJournal().KernelScope); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read=%v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReadBGPFIB(t.Context(), fixtureFIBJournal().KernelScope); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("closed read=%v", err)
	}
	if err := store.SaveBGPFIB(t.Context(), fixtureFIBJournal()); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("closed write=%v", err)
	}
}

func TestBGPFIBJournalStorageKeyBindsOneKernelWorld(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	old := fixtureFIBJournal()
	next := fixtureFIBJournal()
	next.KernelScope = "linux:new-boot:network"
	if err := store.SaveBGPFIB(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.ReadBGPFIB(t.Context(), next.KernelScope); err != nil || found {
		t.Fatalf("another world leaked into current key: %v %v", found, err)
	}
	if err := store.SaveBGPFIB(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	oldKey, err := bgpFIBJournalName(old.KernelScope)
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := bgpFIBJournalName(next.KernelScope)
	if err != nil {
		t.Fatal(err)
	}
	if oldKey == newKey {
		t.Fatal("two worlds shared a receipt key")
	}
	if _, err := store.db.Exec("UPDATE objects SET status=(SELECT status FROM objects WHERE api_version=? AND kind='Router' AND name=?) WHERE api_version=? AND kind='Router' AND name=?", api.NetAPIVersion, newKey, api.NetAPIVersion, oldKey); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReadBGPFIB(t.Context(), old.KernelScope); err == nil {
		t.Fatal("wrong-key payload was accepted")
	}
	current, found, err := store.ReadBGPFIB(t.Context(), next.KernelScope)
	if err != nil || !found || current.KernelScope != next.KernelScope {
		t.Fatalf("wrong-key failure corrupted unrelated world: %+v %v %v", current, found, err)
	}
}

func TestBGPFIBJournalDoesNotReadUnscopedPrivateFormat(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	journal := fixtureFIBJournal()
	if err := store.SaveBGPFIB(t.Context(), journal); err != nil {
		t.Fatal(err)
	}
	key, err := bgpFIBJournalName(journal.KernelScope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("UPDATE objects SET name='bgp-fib' WHERE api_version=? AND kind='Router' AND name=?", api.NetAPIVersion, key); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.ReadBGPFIB(t.Context(), journal.KernelScope); err != nil || found {
		t.Fatalf("old unscoped row was imported: %v %v", found, err)
	}
	var count int
	if err := store.db.QueryRow("SELECT count(*) FROM objects WHERE api_version=? AND kind='Router' AND name='bgp-fib'", api.NetAPIVersion).Scan(&count); err != nil || count != 1 {
		t.Fatalf("old private row was deleted: %d %v", count, err)
	}
}

func TestBGPFIBJournalFreeBSDFIBScopeAlsoBindsNativeTable(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	first := bgp.FIBJournal{KernelScope: "freebsd:boot:jail:fib:0", Applied: []bgp.NativeFIBRoute{{Prefix: "10.77.60.11/32", NextHops: []bgp.FIBNextHop{{Gateway: "192.0.2.1", InterfaceIndex: 7}}, PreferredSource: "192.0.2.10", Protocol: 0x8000, Table: 0}}}
	second := first
	second.KernelScope = "freebsd:boot:jail:fib:1"
	second.Applied = append([]bgp.NativeFIBRoute(nil), first.Applied...)
	second.Applied[0].Table = 1
	if err := store.SaveBGPFIB(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveBGPFIB(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	for _, journal := range []bgp.FIBJournal{first, second} {
		got, found, err := store.ReadBGPFIB(t.Context(), journal.KernelScope)
		if err != nil || !found || got.Applied[0].Table != journal.Applied[0].Table {
			t.Fatalf("FreeBSD table isolation=%+v %v %v", got, found, err)
		}
	}
	second.Applied[0].Table = 0
	if err := store.SaveBGPFIB(t.Context(), second); err == nil {
		t.Fatal("another native FIB table was stored under this scope")
	}
}
