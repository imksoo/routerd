// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/bus"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestUnavailablePDWorkerDoesNotInventDefaultIdentityFromMissingLink(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := pdAtomicReviewConfig()
	config.UplinkIfname = "pd-missing-9z"
	lease, _, err := store.ConfigurePDLease("wan-pd", config)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := pdAtomicReviewSnapshot(time.Now().UTC())
	snapshot.Interface = config.UplinkIfname
	if err := store.SavePDLease("wan-pd", routerstate.ObservePDLease(lease, snapshot, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	before := pdAtomicReviewLease(t, store)
	controller := DaemonStatusController{Router: pdAtomicReviewRouterWithPD(config.UplinkIfname, "", "7"), Store: store, Bus: bus.NewWithStore(store), DaemonSockets: map[string]string{"wan-pd": "missing.sock"}}
	err = controller.resetPDLeaseSourceForUnavailableSocket(t.Context(), "missing.sock")
	if err == nil || !strings.Contains(err.Error(), "default DUID") {
		t.Fatalf("missing identity observation=%v", err)
	}
	after := pdAtomicReviewLease(t, store)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("missing native identity changed lease: before=%+v after=%+v", before, after)
	}
	events, err := store.ListEvents(routerstate.EventQuery{Topic: "routerd.resource.status.changed"})
	if err != nil || len(events) != 0 {
		t.Fatalf("unknown identity published transition: %+v %v", events, err)
	}
	// An explicit configured DUID does not depend on a currently present link.
	controller.Router = pdAtomicReviewRouterWithPD(config.UplinkIfname, "01", "7")
	if err := controller.resetPDLeaseSourceForUnavailableSocket(t.Context(), "missing.sock"); err != nil {
		t.Fatalf("explicit identity depended on native lookup: %v", err)
	}
	if got := pdAtomicReviewLease(t, store); !reflect.DeepEqual(before, got) {
		t.Fatalf("explicit same identity changed valid lease: %+v", got)
	}
}
