// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

type refreshedLeaseStore struct {
	mapStore
	writes int
}

func (s *refreshedLeaseStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	s.writes++
	return s.mapStore.SaveObjectStatus(version, kind, name, status)
}

func TestReconcileAllForRouterRefreshesPreparedInterfaceAndOwnedEffects(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "uplink"}, Spec: api.InterfaceSpec{IfName: "old0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.DHCPv4ClientSpec{Interface: "uplink", RouteMetric: 100, UseDNS: boolPtr(false)}},
	}}}
	statuses := mapStore{}
	oldStore := &refreshedLeaseStore{mapStore: statuses}
	newStore := &refreshedLeaseStore{mapStore: statuses}
	oldAddress, newAddress, oldRoute, newRoute := false, false, false, false
	var effects []string
	c := &Controller{DaemonSockets: map[string]string{"wan": filepath.Join(t.TempDir(), "unavailable.sock")}, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		switch line {
		case "ip -4 addr show dev old0":
			if oldAddress {
				return []byte("inet 192.0.2.10/24 scope global old0\n"), nil
			}
		case "ip -4 addr show dev new0":
			if newAddress {
				return []byte("inet 192.0.2.10/24 scope global new0\n"), nil
			}
		case "ip -4 route show default":
			if oldRoute {
				return []byte("default via 192.0.2.1 dev old0 metric 100\n"), nil
			}
			if newRoute {
				return []byte("default via 192.0.2.1 dev new0 metric 100\n"), nil
			}
		case "ip -4 addr add 192.0.2.10/24 dev old0":
			oldAddress = true
			effects = append(effects, line)
		case "ip -4 route add default via 192.0.2.1 dev old0 metric 100":
			oldRoute = true
			effects = append(effects, line)
		case "ip -4 route del default via 192.0.2.1 dev old0 metric 100":
			oldRoute = false
			effects = append(effects, line)
		case "ip -4 addr del 192.0.2.10/24 dev old0":
			oldAddress = false
			effects = append(effects, line)
		case "ip -4 addr add 192.0.2.10/24 dev new0":
			newAddress = true
			effects = append(effects, line)
		case "ip -4 route add default via 192.0.2.1 dev new0 metric 100":
			newRoute = true
			effects = append(effects, line)
		default:
			return nil, fmt.Errorf("unexpected native command: %s", line)
		}
		return nil, nil
	}}
	// A stopped worker must not prevent the actor from refreshing preparation,
	// nor make its last successfully applied lease lose ownership.
	if err := c.ReconcileAllForRouter(t.Context(), router, oldStore); err != nil {
		t.Fatalf("first-start worker should wait without failing apply: %v", err)
	}
	if mapString(statuses[api.NetAPIVersion+"/DHCPv4Client/wan"], "phase") != daemonapi.ResourcePhasePending {
		t.Fatal("first-start worker was not reported pending")
	}
	status := func(ifname string) daemonapi.DaemonStatus {
		return daemonapi.DaemonStatus{Resources: []daemonapi.ResourceStatus{{
			Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: "wan"}, Phase: daemonapi.ResourcePhaseBound,
			Observed: map[string]string{"interface": ifname, "currentAddress": "192.0.2.10", "prefixLength": "24", "defaultGateway": "192.0.2.1"},
		}}}
	}
	if err := c.applyDaemonStatus(t.Context(), "wan", status("old0")); err != nil {
		t.Fatal(err)
	}
	prepared := &c.leases[0]
	if err := c.ReconcileAllForRouter(t.Context(), router, oldStore); err == nil {
		t.Fatal("unavailable worker was reported healthy")
	}
	if &c.leases[0] != prepared {
		t.Fatal("same immutable declaration rebuilt static lease preparation")
	}
	next := router.Clone()
	next.Spec.Resources[0].Spec = api.InterfaceSpec{IfName: "new0"}
	effects = nil
	before := oldStore.writes
	if err := c.ReconcileAllForRouter(t.Context(), &next, newStore); err == nil {
		t.Fatal("unavailable worker was reported healthy")
	}
	if len(c.leases) != 1 || c.leases[0].ifname != "new0" || len(effects) != 0 || !oldAddress || !oldRoute {
		t.Fatalf("refresh lost old effects or retained old mapping: leases=%+v effects=%v", c.leases, effects)
	}
	if err := c.applyDaemonStatus(t.Context(), "wan", status("old0")); err == nil {
		t.Fatal("stale worker interface was accepted after configuration change")
	}
	if len(effects) != 0 || mapString(statuses[api.NetAPIVersion+"/DHCPv4Client/wan"], "ifname") != "old0" {
		t.Fatal("stale observation changed an owned effect")
	}
	if err := c.applyDaemonStatus(t.Context(), "wan", status("new0")); err != nil {
		t.Fatal(err)
	}
	want := "ip -4 route del default via 192.0.2.1 dev old0 metric 100\nip -4 addr del 192.0.2.10/24 dev old0\nip -4 addr add 192.0.2.10/24 dev new0\nip -4 route add default via 192.0.2.1 dev new0 metric 100"
	if strings.Join(effects, "\n") != want || oldAddress || oldRoute || !newAddress || !newRoute {
		t.Fatalf("configuration transition did not withdraw before replacement: %v", effects)
	}
	if oldStore.writes != before || newStore.writes == 0 {
		t.Fatal("updated actor wrote through the previous declaration's status store")
	}
	if got := statuses[api.NetAPIVersion+"/DHCPv4Client/wan"]; mapString(got, "ifname") != "new0" || mapString(got, "appliedAddress") != "192.0.2.10/24" || mapString(got, "pendingAddress") != "" {
		t.Fatalf("updated ownership not published: %+v", got)
	}
	effects = nil
	if err := c.applyDaemonStatus(t.Context(), "wan", status("new0")); err != nil || len(effects) != 0 {
		t.Fatalf("unchanged new lease repeated native effects: %v %v", effects, err)
	}
}

func TestReconcileAllForRouterSerializesRefreshWithLeaseActor(t *testing.T) {
	first, second := &api.Router{}, &api.Router{}
	store := mapStore{}
	c := &Controller{Router: first, Store: store}
	var workers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for i := 0; i < 30; i++ {
				var err error
				switch worker {
				case 0:
					err = c.ReconcileAllForRouter(t.Context(), second, store)
				case 1:
					err = c.ReconcileAll(t.Context())
				case 2:
					err = c.Reconcile(t.Context(), "absent")
				case 3:
					_ = c.NextExpiryAfter()
				}
				if err != nil {
					t.Errorf("lease actor reconcile: %v", err)
				}
			}
		}(worker)
	}
	workers.Wait()
}
