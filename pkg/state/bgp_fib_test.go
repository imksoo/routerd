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
	if _, found, err := store.ReadBGPFIB(t.Context()); err != nil || found {
		t.Fatalf("missing journal=%v %v", found, err)
	}
	original := fixtureFIBJournal()
	if err := store.SaveBGPFIB(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	got, found, err := store.ReadBGPFIB(t.Context())
	if err != nil || !found || !bgp.EqualNativeFIBRoute(got.Applied[0], original.Applied[0]) {
		t.Fatalf("typed journal roundtrip=%+v %v %v", got, found, err)
	}
	got.Applied[0].NextHops[0].InterfaceIndex = 0
	if err := store.SaveBGPFIB(t.Context(), got); err == nil {
		t.Fatal("unusable native identity was persisted")
	}
	reread, _, err := store.ReadBGPFIB(t.Context())
	if err != nil || reread.Applied[0].NextHops[0].InterfaceIndex != 7 {
		t.Fatalf("rejected write changed ownership: %+v %v", reread, err)
	}
}
func TestBGPFIBJournalCorruptReceiptIsUnknownRatherThanEmpty(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	for _, raw := range []string{`null`, `{"kernelScope":"linux:fixture:network","applied":null}`, `{"kernelScope":"linux:fixture:network","applied":[{"prefix":"10.77.60.11/32","nextHops":[{"gateway":"192.0.2.1","interfaceIndex":0}],"table":254,"protocol":186,"metric":200}]}`} {
		if err := store.SaveBGPFIB(t.Context(), fixtureFIBJournal()); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec("UPDATE objects SET status=? WHERE api_version=? AND kind='Router' AND name=?", raw, api.NetAPIVersion, bgpFIBJournalName); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.ReadBGPFIB(t.Context()); err == nil {
			t.Fatalf("corrupt FIB receipt became empty ownership: %s", raw)
		}
	}
}
func TestBGPFIBJournalClosedStoreAndCanceledReadPreserveErrors(t *testing.T) {
	store := mustOpenStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := store.ReadBGPFIB(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read=%v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReadBGPFIB(t.Context()); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("closed read=%v", err)
	}
	if err := store.SaveBGPFIB(t.Context(), fixtureFIBJournal()); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("closed write=%v", err)
	}
}
