// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventrule"
	"github.com/imksoo/routerd/pkg/pdclient"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func pdAtomicReviewRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"},
		Metadata: api.ObjectMeta{Name: "pd-transitions"},
		Spec: api.EventRuleSpec{
			Pattern: api.EventRulePatternSpec{Operator: eventrule.OperatorCount, Topic: "routerd.resource.status.changed", Threshold: 1},
			Emit:    api.EventRuleEmitSpec{Topic: "routerd.test.pd-transition"},
		},
	}}}}
}

func pdAtomicReviewConfig() routerstate.PDLeaseConfig {
	return routerstate.PDLeaseConfig{Client: api.IPv6PDClientRouterd, Profile: api.IPv6PDProfileDefault, UplinkIfname: "wan0", PrefixLength: 56, ClientDUID: "01", IAID: 7}
}

func pdAtomicReviewRouterWithPD(ifname, duid, iaid string) *api.Router {
	router := pdAtomicReviewRouter()
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
		Metadata: api.ObjectMeta{Name: "wan-pd"},
		Spec: api.DHCPv6PrefixDelegationSpec{Client: api.IPv6PDClientRouterd, Profile: api.IPv6PDProfileDefault,
			Interface: ifname, ClientDUID: duid, IAID: iaid, PrefixLength: 56},
	})
	return router
}

func pdAtomicReviewSnapshot(at time.Time) pdclient.Snapshot {
	return pdclient.Snapshot{
		Resource: "wan-pd", Interface: "wan0", ClientDUID: "01", ServerDUID: "02", IAID: 7,
		State: pdclient.StateBound, CurrentPrefix: "2001:db8:100::/56", UpdatedAt: at, LeaseUpdatedAt: at,
		T1Seconds: 600, T2Seconds: 900,
		Prefixes: []pdclient.PrefixSnapshot{{Prefix: "2001:db8:100::/56", AcquiredAt: at, Preferred: 1200, Valid: 1800}},
	}
}

func pdAtomicReviewLease(t *testing.T, store *routerstate.SQLiteStore) routerstate.PDLease {
	t.Helper()
	lease, found, err := store.ReadPDLease("wan-pd")
	if err != nil || !found {
		t.Fatalf("canonical observation: found=%t err=%v", found, err)
	}
	return lease
}

func pdAtomicReviewEvents(t *testing.T, store *routerstate.SQLiteStore) []routerstate.StoredEvent {
	t.Helper()
	events, err := store.ListEvents(routerstate.EventQuery{Topic: "routerd.resource.status.changed", Ascending: true})
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func pdAtomicReviewReady(t *testing.T, lease routerstate.PDLease, want string) {
	t.Helper()
	if len(lease.Conditions) != 1 || lease.Conditions[0].Type != "LeaseReady" || lease.Conditions[0].Status != want {
		t.Fatalf("LeaseReady = %+v, want %s", lease.Conditions, want)
	}
}

func TestPDAtomicReviewRejectsOlderIncomingSnapshotWithFreshCanonicalRead(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	eventBus := bus.NewWithStore(store)
	router := pdAtomicReviewRouter()
	lease, _ := (routerstate.PDLease{}).Configured(pdAtomicReviewConfig())
	newer := pdAtomicReviewSnapshot(time.Now().UTC().Add(-time.Second))
	if err := RecordPDLeaseObservation(store, eventBus, router, "wan-pd", routerstate.PDLeaseConfig{}, lease, &newer, daemonapi.ResourceStatus{}); err != nil {
		t.Fatal(err)
	}
	// Apply reads the latest canonical lease first, then finds an older helper
	// file. Its captured lease watermark is current; the incoming file is not.
	current := pdAtomicReviewLease(t, store)
	older := newer
	older.UpdatedAt = newer.UpdatedAt.Add(-time.Second)
	older.State, older.CurrentPrefix, older.Prefixes = pdclient.StateReleased, "", nil
	if err := RecordPDLeaseObservation(store, eventBus, router, "wan-pd", current.Config(), current, &older, daemonapi.ResourceStatus{}); err != nil {
		t.Fatal(err)
	}
	got := pdAtomicReviewLease(t, store)
	if got.CurrentPrefix != newer.CurrentPrefix || !got.SnapshotUpdatedAt.Equal(newer.UpdatedAt) || got.Phase != daemonapi.ResourcePhaseBound {
		t.Fatalf("older incoming snapshot overwrote latest canonical lease: %+v", got)
	}
	pdAtomicReviewReady(t, got, daemonapi.ConditionTrue)
	if events := pdAtomicReviewEvents(t, store); len(events) != 1 {
		t.Fatalf("rejected old snapshot published a transition: %+v", events)
	}
	if events := eventBus.Recent("routerd.resource.status.changed"); len(events) != 1 {
		t.Fatalf("rejected old snapshot delivered locally: %+v", events)
	}
	// A display/profile change retains this source's lease watermark. Full
	// config inequality must not make the same old file authoritative again.
	profileConfig := current.Config()
	profileConfig.Profile = "updated-profile"
	profileUpdate, _ := current.Configured(profileConfig)
	if err := RecordPDLeaseObservation(store, eventBus, router, "wan-pd", current.Config(), profileUpdate, &older, daemonapi.ResourceStatus{}); err != nil {
		t.Fatal(err)
	}
	got = pdAtomicReviewLease(t, store)
	if got.CurrentPrefix != newer.CurrentPrefix || !got.SnapshotUpdatedAt.Equal(newer.UpdatedAt) || got.Phase != daemonapi.ResourcePhaseBound {
		t.Fatalf("metadata-only config change revived older snapshot: %+v", got)
	}
	if events := pdAtomicReviewEvents(t, store); len(events) != 1 {
		t.Fatalf("profile change with rejected old snapshot published stale transition: %+v", events)
	}
}

func TestPDAtomicReviewSourceChangeRejectsInflightLeaseAndRecordsTransition(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	eventBus := bus.NewWithStore(store)
	router := pdAtomicReviewRouter()
	lease, _ := (routerstate.PDLease{}).Configured(pdAtomicReviewConfig())
	snapshot := pdAtomicReviewSnapshot(time.Now().UTC().Add(-time.Second))
	if err := RecordPDLeaseObservation(store, eventBus, router, "wan-pd", routerstate.PDLeaseConfig{}, lease, &snapshot, daemonapi.ResourceStatus{}); err != nil {
		t.Fatal(err)
	}
	// Retain the old producer's captured source and complete observation while
	// a second producer commits the operator's new source in the same row.
	inflight := pdAtomicReviewLease(t, store)
	nextConfig := inflight.Config()
	nextConfig.UplinkIfname = "wan1"
	next, _ := inflight.Configured(nextConfig)
	if err := RecordPDLeaseObservation(store, eventBus, router, "wan-pd", inflight.Config(), next, nil, daemonapi.ResourceStatus{}); err != nil {
		t.Fatal(err)
	}
	if err := RecordPDLeaseObservation(store, eventBus, router, "wan-pd", inflight.Config(), inflight, nil, daemonapi.ResourceStatus{}); !errors.Is(err, routerstate.ErrStalePDLeaseSource) {
		t.Fatalf("inflight source-A observation admitted under source B: %v", err)
	}
	got := pdAtomicReviewLease(t, store)
	if got.Config() != nextConfig || got.CurrentPrefix != "" || got.Phase != daemonapi.ResourcePhasePending {
		t.Fatalf("source change lost its canonical empty authority: %+v", got)
	}
	pdAtomicReviewReady(t, got, daemonapi.ConditionFalse)
	events := pdAtomicReviewEvents(t, store)
	if len(events) != 2 || events[1].Attributes["previousPhase"] != daemonapi.ResourcePhaseBound || events[1].Attributes["phase"] != daemonapi.ResourcePhasePending {
		t.Fatalf("source change did not record exact committed transition: %+v", events)
	}
}

func TestPDAtomicReviewExpiryUsesCommittedPreviousPhaseAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	// Model an interrupted poll: disk still records the last successful Bound
	// observation, but its valid lifetime expires while the observer is down.
	lease, _ := (routerstate.PDLease{}).Configured(pdAtomicReviewConfig())
	lease.CurrentPrefix, lease.LastPrefix = "2001:db8:100::/56", "2001:db8:100::/56"
	lease.LastReplyAt = time.Now().UTC().Add(-2 * time.Minute)
	lease.SnapshotUpdatedAt, lease.LastObservedAt = lease.LastReplyAt, lease.LastReplyAt
	lease.ValidSeconds, lease.PreferredSeconds, lease.Phase = 60, 30, daemonapi.ResourcePhaseBound
	lease.Conditions = []daemonapi.Condition{{Type: "LeaseReady", Status: daemonapi.ConditionTrue, Reason: string(pdclient.StateBound), LastTransitionTime: lease.LastReplyAt}}
	if err := store.SavePDLease("wan-pd", lease); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd")
	if err != nil || status["phase"] != daemonapi.ResourcePhaseExpired || status["currentPrefix"] != "" {
		t.Fatalf("ordinary read must already expire authority: %+v err=%v", status, err)
	}
	eventBus := bus.NewWithStore(store)
	controller := DaemonStatusController{Router: pdAtomicReviewRouterWithPD("wan0", "01", "7"), Store: store, Bus: eventBus,
		DaemonSockets: map[string]string{"wan-pd": filepath.Join(t.TempDir(), "missing.sock")}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := pdAtomicReviewLease(t, store)
	if got.CurrentPrefix != "" || got.Phase != daemonapi.ResourcePhaseExpired {
		t.Fatalf("expiry not committed: %+v", got)
	}
	pdAtomicReviewReady(t, got, daemonapi.ConditionFalse)
	events := pdAtomicReviewEvents(t, store)
	if len(events) != 1 || events[0].Attributes["previousPhase"] != daemonapi.ResourcePhaseBound || events[0].Attributes["phase"] != daemonapi.ResourcePhaseExpired {
		t.Fatalf("expiry history compared two already-expired views: %+v", events)
	}
	if events[0].ResourceKind != "DHCPv6PrefixDelegation" || events[0].ResourceName != "wan-pd" {
		t.Fatalf("expiry transition lost resource identity: %+v", events[0])
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if events := pdAtomicReviewEvents(t, store); len(events) != 1 {
		t.Fatalf("repeated missing socket re-published known expiry: %+v", events)
	}
}

func TestPDAtomicReviewMissingSocketResetsChangedSourceOnly(t *testing.T) {
	for _, changedSource := range []bool{false, true} {
		name := "same-source"
		if changedSource {
			name = "changed-source"
		}
		t.Run(name, func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			eventBus := bus.NewWithStore(store)
			router := pdAtomicReviewRouterWithPD("wan0", "01", "7")
			lease, _ := (routerstate.PDLease{}).Configured(pdAtomicReviewConfig())
			snapshot := pdAtomicReviewSnapshot(time.Now().UTC().Add(-time.Second))
			snapshot.AFTRName, snapshot.DNSServers = "aftr.old.example", []string{"2001:db8::53"}
			if err := RecordPDLeaseObservation(store, eventBus, router, "wan-pd", routerstate.PDLeaseConfig{}, lease, &snapshot, daemonapi.ResourceStatus{}); err != nil {
				t.Fatal(err)
			}
			if changedSource {
				router = pdAtomicReviewRouterWithPD("wan1", "03", "9")
			}
			controller := DaemonStatusController{Router: router, Store: store, Bus: eventBus,
				DaemonSockets: map[string]string{"wan-pd": filepath.Join(t.TempDir(), "missing.sock")}}
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			current := pdAtomicReviewLease(t, store)
			status, err := store.ReadObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd")
			if err != nil {
				t.Fatal(err)
			}
			wantEvents := 1
			if changedSource {
				wantEvents = 2
				if current.CurrentPrefix != "" || current.Phase != daemonapi.ResourcePhasePending || current.UplinkIfname != "wan1" || current.ConfiguredClientDUID != "03" || current.ConfiguredIAID != 9 {
					t.Fatalf("missing new worker retained old source authority: %+v", current)
				}
				pdAtomicReviewReady(t, current, daemonapi.ConditionFalse)
				if status["aftrName"] != nil || status["dnsServers"] != nil || status["observed"] != nil {
					t.Fatalf("source reset retained old DHCP information: %+v", status)
				}
			} else {
				if current.CurrentPrefix != snapshot.CurrentPrefix || current.Phase != daemonapi.ResourcePhaseBound || status["aftrName"] != snapshot.AFTRName {
					t.Fatalf("socket error revoked a still-valid unchanged source: %+v status=%+v", current, status)
				}
				pdAtomicReviewReady(t, current, daemonapi.ConditionTrue)
			}
			events := pdAtomicReviewEvents(t, store)
			if len(events) != wantEvents {
				t.Fatalf("missing-socket transition count = %d, want %d: %+v", len(events), wantEvents, events)
			}
			if changedSource && (events[1].Attributes["previousPhase"] != daemonapi.ResourcePhaseBound || events[1].Attributes["phase"] != daemonapi.ResourcePhasePending) {
				t.Fatalf("source reset did not journal exact transition: %+v", events[1])
			}
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if events := pdAtomicReviewEvents(t, store); len(events) != wantEvents {
				t.Fatalf("repeated missing worker duplicated source transition: %+v", events)
			}
		})
	}
}
