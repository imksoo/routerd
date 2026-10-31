// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/conntrack"
	"github.com/imksoo/routerd/pkg/controller/conntrackobserver"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestReviewFlowLogFailureCannotRemainObserved(t *testing.T) {
	for _, failure := range []string{"configured database", "required public status"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			statePath := filepath.Join(dir, "state.db")
			store, err := routerstate.OpenSQLite(statePath)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			countPath, maxPath := filepath.Join(dir, "count"), filepath.Join(dir, "max")
			if err := os.WriteFile(countPath, []byte("1\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(maxPath, []byte("10\n"), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "flows.db")
			router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "flow-review"}, Spec: api.RouterSpec{Resources: []api.Resource{{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TrafficFlowLog"},
				Metadata: api.ObjectMeta{Name: "flows"},
				Spec:     api.TrafficFlowLogSpec{Enabled: true, Source: "conntrack", Path: path},
			}}}}
			if err := config.ValidateForOS(router, platform.OSLinux); err != nil {
				t.Fatalf("actual configuration admission: %v", err)
			}
			calls := 0
			events := bus.New(bus.WithJournal(store))
			controller := &conntrackobserver.Controller{Router: router, Store: eventedStore{Store: store, Bus: events, Router: router}, Bus: events,
				Paths: conntrack.Paths{Count: countPath, Max: maxPath},
				Connections: func(ctx context.Context, limit int) (*observe.ConnectionTable, error) {
					calls++
					if limit != -1 {
						t.Fatalf("flow writer requested a truncated observation: %d", limit)
					}
					return &observe.ConnectionTable{Count: 1, Entries: []observe.ConnectionEntry{{
						Protocol: "tcp",
						Original: observe.ConntrackTuple{Source: "192.0.2.10", SourcePort: "12345", Destination: "198.51.100.20", DestinationPort: "443"},
						Reply:    observe.ConntrackTuple{Source: "198.51.100.20", SourcePort: "443", Destination: "192.0.2.10", DestinationPort: "12345"},
					}}}, ctx.Err()
				},
			}
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatalf("positive flow recording: %v", err)
			}
			good, err := store.ReadObjectStatus(api.NetAPIVersion, "TrafficFlowLog", "flows")
			if err != nil || good["phase"] != "Observed" {
				t.Fatalf("positive public observation: %+v %v", good, err)
			}
			flowLog, err := logstore.OpenTrafficFlowLog(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := flowLog.List(t.Context(), logstore.TrafficFlowFilter{Limit: 10})
			if err != nil || len(rows) != 1 {
				t.Fatalf("positive actual flow database: %+v %v", rows, err)
			}
			if err := flowLog.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(countPath, []byte("9\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if failure == "configured database" {
				if err := os.Rename(path, filepath.Join(dir, "preserved-flow-database")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				fault, err := sql.Open("sqlite", statePath)
				if err != nil {
					t.Fatal(err)
				}
				defer fault.Close()
				if _, err := fault.Exec(`CREATE TRIGGER reject_flow_public_status BEFORE INSERT ON objects
WHEN NEW.kind='TrafficFlowLog' BEGIN SELECT RAISE(ABORT,'fixture required flow status failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			reconcileErr := controller.Reconcile(t.Context())
			public, statusErr := store.ReadObjectStatus(api.NetAPIVersion, "TrafficFlowLog", "flows")
			count, countErr := store.ReadObjectStatus(api.NetAPIVersion, "ConntrackObserver", "default")
			t.Logf("failure=%q calls=%d err=%v flow=%+v count=%+v statusErr=%v/%v threshold=%d",
				failure, calls, reconcileErr, public, count, statusErr, countErr, len(events.Recent("routerd.conntrack.threshold.exceeded")))
			if calls != 2 || statusErr != nil || countErr != nil || count["count"] != float64(9) {
				t.Fatal("fixture did not reach the configured flow effect after a successful aggregate observation")
			}
			if len(events.Recent("routerd.conntrack.threshold.exceeded")) != 1 {
				t.Fatal("flow log failure disabled the independent conntrack threshold event")
			}
			if failure == "configured database" {
				if reconcileErr != nil || public["phase"] == "Observed" || public["reason"] == nil {
					t.Fatalf("failed diagnostic destination remained healthy or blocked the observer: err=%v status=%+v", reconcileErr, public)
				}
			} else if reconcileErr == nil {
				t.Fatalf("required public status failure was acknowledged: status=%+v", public)
			}
		})
	}
}
