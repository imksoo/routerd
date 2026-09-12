// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	mobilitycontroller "github.com/imksoo/routerd/pkg/controller/mobility"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/lifecycle"
)

func TestContractFrameworkRegistrationAndScheduledSelection(t *testing.T) {
	// Construction with unused optional features must continue to support a
	// status-only Store. No controller or auxiliary worker is executed here.
	runner := &Runner{Router: &api.Router{}, Bus: bus.New(), Store: mapStore{}, Opts: Options{SuperviseClientDaemons: true}}
	controllers, _, err := runner.frameworkControllers(context.Background(), slog.Default(), eventedStore{Store: runner.Store}, false, ha.Decision{})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Fields(`observability-pipeline daemon-status dhcp-lease-sync nat44-session-sync
		package kernel-module sysctl network-adoption bridge vxlan-tunnel service-unit log-retention
		ntp-client ntp-server link sam-enrollment-client sam-transport tunnel wireguard ipv4-static-address
		dhcpv6-information lan-address dslite ipv4-policy-route ipv4-route hybrid-route sam path-mtu
		dhcpv6-server dhcpv4-lease pppoe-session dns-resolver event-federation event-subscription
		mobility-discovery mobility-arp-request mobility mobility-shard provider-action-execution
		egress-route-policy ingress-service nat44 bfd bgp vrrp daemon-supervisor ip-address-set
		firewall daemon-supervisor-reconcile`)
	var got []string
	byName := map[string]framework.Controller{}
	for _, controller := range controllers {
		got = append(got, controller.Name())
		if byName[controller.Name()] != nil {
			t.Errorf("duplicate controller %q", controller.Name())
		}
		byName[controller.Name()] = controller
	}
	slices.Sort(got)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registered controllers=%v, want %v", got, want)
	}
	// bridge and vxlan-tunnel intentionally remain in the scheduled pass. The
	// other registered controllers use their event-loop cadence instead.
	for _, controller := range controllers {
		wantSkip := controller.Name() != "bridge" && controller.Name() != "vxlan-tunnel"
		if got := scheduledReconcileSkipsController(controller.Name()); got != wantSkip {
			t.Errorf("scheduled skip %s=%t, want %t", controller.Name(), got, wantSkip)
		}
	}
	filtered := filterScheduledReconcileControllers(controllers)
	if len(filtered) != 2 || filtered[0].Name() != "bridge" || filtered[1].Name() != "vxlan-tunnel" {
		t.Fatalf("scheduled controllers=%v", filtered)
	}

	// One pool feeds several effectors. Declaration ownership remains the
	// resource identity, independent of these controller names or generations.
	declaration, ok := lifecycle.Lookup(api.MobilityAPIVersion, "MobilityPool")
	if !ok || declaration.Class != lifecycle.ClassDynamicSource || declaration.NoHostTeardownReason == "" {
		t.Fatalf("pool lifecycle declaration=%+v", declaration)
	}
	pool := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"}, Metadata: api.ObjectMeta{Name: "pool"}}
	status := statusWithLifecycle(pool.APIVersion, pool.Kind, pool.Metadata.Name, pool, true, map[string]any{"phase": "Projected"})
	if status["ownerKey"] != api.MobilityAPIVersion+"/MobilityPool/pool" || status["lifecycleClass"] != string(lifecycle.ClassDynamicSource) {
		t.Fatalf("pool ownership=%v", status)
	}
	event := daemonapi.DaemonEvent{Type: mobilitycontroller.PoolPlanChangedEvent,
		Resource:   &daemonapi.ResourceRef{APIVersion: pool.APIVersion, Kind: pool.Kind, Name: pool.Metadata.Name},
		Attributes: map[string]string{"source": "MobilityPool/pool/node/node-a", "digest": "sha256:plan"}}
	for _, name := range []string{"bgp", "ipv4-route", "ipv4-static-address", "path-mtu", "firewall", "sam"} {
		if !subscriptionSetAccepts(byName[name].Subscriptions(), event) {
			t.Errorf("registered controller %s misses pool plan subscription", name)
		}
	}
}

func TestContractOwnershipNamesAreNotFrameworkAliases(t *testing.T) {
	// These existing owner/display groups intentionally cover multiple actual
	// workers. Keep their names stable without registering fictitious workers.
	for _, tc := range []struct{ kind, owner string }{
		{"IPv4StaticAddress", "address"}, {"IPv6DelegatedAddress", "address"},
		{"DHCPv4Client", "dhcpv4client"}, {"DHCPv6Server", "dhcpv6"},
		{"NAT44Rule", "nat"}, {"IPv4Route", "route"}, {"SAMTransportProfile", "route"},
		{"PPPoESession", "pppoesession"},
	} {
		if got := resourceOwnerController(tc.kind); got != tc.owner {
			t.Errorf("%s owner=%q, want existing group %q", tc.kind, got, tc.owner)
		}
		if scheduledReconcileSkipsController(tc.owner) {
			t.Errorf("display owner %q unexpectedly treated as a scheduled framework worker", tc.owner)
		}
	}
}
