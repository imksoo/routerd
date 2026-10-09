// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventrule"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// Match the daemon's wire representation: counters and observedClients are
// strings, and each repeated observation refreshes the client's seenAt.
func arpObserverTestStatus(sequence int) map[string]any {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(sequence) * time.Second).Format(time.RFC3339Nano)
	status := map[string]any{
		"phase":                       "Watching",
		"health":                      daemonapi.HealthOK,
		"interface":                   "lan",
		"ifname":                      "eth1",
		"prefix":                      "192.0.2.0/24",
		"sourceType":                  "arp-observer",
		"ignoredSenderMACsConfigured": "true",
		"ignoredSenderMACs":           "",
		"observedClients":             fmt.Sprintf(`[{"ip":"192.0.2.10","mac":"02:00:00:00:00:10","sourceType":"arp-observer","seenAt":%q}]`, at),
	}
	for _, field := range []string{"packetsSeen", "observedCount", "probeCount", "probeHitCount", "proactiveCount", "requestObservedCount", "commandProbeCount", "scanCount", "ignoredSenderMACObservationCount"} {
		status[field] = strconv.Itoa(sequence)
	}
	for _, field := range []string{"lastPacketAt", "lastEventAt", "lastScanAt"} {
		status[field] = at
	}
	return status
}

func TestARPObserverRoutineStatusUpdatesKeepDiagnosticsWithoutEvents(t *testing.T) {
	for _, history := range []bool{false, true} {
		for _, merge := range []bool{false, true} {
			t.Run(fmt.Sprintf("history=%t/merge=%t", history, merge), func(t *testing.T) {
				base, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer base.Close()
				events := bus.NewWithStore(base)
				store := eventedStore{Store: base, Bus: events}
				if history {
					store.Router = &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
						TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"},
						Metadata: api.ObjectMeta{Name: "status-count"},
						Spec: api.EventRuleSpec{
							Pattern: api.EventRulePatternSpec{Operator: eventrule.OperatorCount, Topic: "routerd.resource.status.changed", Threshold: 1},
							Emit:    api.EventRuleEmitSpec{Topic: "routerd.test.transition"},
						},
					}}}}
				}
				save := func(status map[string]any) {
					t.Helper()
					var err error
					if merge {
						err = store.MergeObjectStatus(api.MobilityAPIVersion, "ARPObserver", "lan", status)
					} else {
						err = store.SaveObjectStatus(api.MobilityAPIVersion, "ARPObserver", "lan", status)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				save(arpObserverTestStatus(0))
				for i := 1; i <= 100; i++ {
					save(arpObserverTestStatus(i))
				}
				stored := base.ObjectStatus(api.MobilityAPIVersion, "ARPObserver", "lan")
				for field, want := range arpObserverTestStatus(100) {
					if !reflect.DeepEqual(stored[field], want) {
						t.Errorf("diagnostic %s = %#v, want latest %#v", field, stored[field], want)
					}
				}
				journal, err := base.ListEvents(routerstate.EventQuery{Topic: "routerd.resource.status.changed", Limit: 1000})
				if err != nil {
					t.Fatal(err)
				}
				if len(journal) != 1 {
					t.Errorf("100 observation refreshes left %d journal events, want only the initial event", len(journal))
				}
				if got := len(events.Recent("routerd.resource.status.changed")); got != 1 {
					t.Errorf("100 observation refreshes delivered %d events, want only the initial event", got)
				}
				failed := arpObserverTestStatus(101)
				failed["phase"], failed["health"], failed["error"] = "Pending", daemonapi.HealthDegraded, "packet socket unavailable"
				save(failed)
				recovered := arpObserverTestStatus(102)
				recovered["error"] = ""
				save(recovered)
				journal, err = base.ListEvents(routerstate.EventQuery{Topic: "routerd.resource.status.changed", Limit: 1000, Ascending: true})
				if err != nil {
					t.Fatal(err)
				}
				if len(journal) != 3 {
					t.Fatalf("failure/recovery history has %d events, want initial, Pending, Watching", len(journal))
				}
				if journal[1].Attributes["phase"] != "Pending" || journal[2].Attributes["phase"] != "Watching" {
					t.Fatalf("failure/recovery phases = %q, %q", journal[1].Attributes["phase"], journal[2].Attributes["phase"])
				}
			})
		}
	}
}

func TestARPObserverStatusEventsPreserveOperationalChanges(t *testing.T) {
	for _, test := range []struct {
		name, field string
		update      func(map[string]any)
	}{
		{"phase", "phase", func(s map[string]any) { s["phase"] = "Pending" }},
		{"health", "health", func(s map[string]any) { s["health"] = daemonapi.HealthDegraded }},
		{"error", "error", func(s map[string]any) { s["error"] = "ARP table read failed" }},
		{"interface", "ifname", func(s map[string]any) { s["ifname"] = "eth2" }},
		{"prefix", "prefix", func(s map[string]any) { s["prefix"] = "198.51.100.0/24" }},
		{"ignored MACs", "ignoredSenderMACs", func(s map[string]any) { s["ignoredSenderMACs"] = "02:00:00:00:00:01" }},
		{"ignored MAC readiness", "ignoredSenderMACsConfigured", func(s map[string]any) { s["ignoredSenderMACsConfigured"] = "false" }},
		{"client IP", "observedClients", func(s map[string]any) {
			s["observedClients"] = strings.ReplaceAll(s["observedClients"].(string), "192.0.2.10", "192.0.2.11")
		}},
		{"client MAC", "observedClients", func(s map[string]any) {
			s["observedClients"] = strings.ReplaceAll(s["observedClients"].(string), "02:00:00:00:00:10", "02:00:00:00:00:11")
		}},
		{"client source", "observedClients", func(s map[string]any) {
			s["observedClients"] = strings.ReplaceAll(s["observedClients"].(string), "arp-observer", "pve-svnet")
		}},
		{"client added", "observedClients", func(s map[string]any) {
			s["observedClients"] = strings.TrimSuffix(s["observedClients"].(string), "]") + `,{"ip":"192.0.2.11","mac":"02:00:00:00:00:11","sourceType":"arp-observer"}]`
		}},
		{"client removed", "observedClients", func(s map[string]any) { s["observedClients"] = "[]" }},
		{"future client field", "observedClients", func(s map[string]any) {
			s["observedClients"] = strings.ReplaceAll(s["observedClients"].(string), `"ip":`, `"reachable":false,"ip":`)
		}},
		{"invalid client JSON", "observedClients", func(s map[string]any) { s["observedClients"] = "invalid" }},
		{"future diagnostic", "captureErrors", func(s map[string]any) { s["captureErrors"] = "1" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			events := bus.New()
			store := eventedStore{Store: mapStore{}, Bus: events}
			if err := store.SaveObjectStatus(api.MobilityAPIVersion, "ARPObserver", "lan", arpObserverTestStatus(0)); err != nil {
				t.Fatal(err)
			}
			next := arpObserverTestStatus(1)
			test.update(next)
			if err := store.SaveObjectStatus(api.MobilityAPIVersion, "ARPObserver", "lan", next); err != nil {
				t.Fatal(err)
			}
			recent := events.Recent("routerd.resource.status.changed")
			if len(recent) != 2 {
				t.Fatalf("events = %d, want initial and operational change", len(recent))
			}
			event := recent[len(recent)-1]
			if event.Severity != daemonapi.SeverityInfo || event.Attributes["changedFields"] != test.field {
				t.Fatalf("event = %#v, want info with changedFields=%s", event, test.field)
			}
		})
	}
}

func TestARPObserverEventProjectionDoesNotAffectOtherResourceGroups(t *testing.T) {
	if !statusChangedForEvent(api.NetAPIVersion, "ARPObserver", arpObserverTestStatus(0), arpObserverTestStatus(1)) {
		t.Fatal("mobility observer projection suppressed another API group's changes")
	}
}
