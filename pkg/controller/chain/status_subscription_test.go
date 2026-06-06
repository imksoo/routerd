// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestSAMRouteControllersSubscribeToVirtualAddressStatus(t *testing.T) {
	event := daemonapi.DaemonEvent{
		Type: "routerd.resource.status.changed",
		Resource: &daemonapi.ResourceRef{
			APIVersion: api.NetAPIVersion,
			Kind:       "VirtualAddress",
			Name:       "onprem-vip",
		},
		Attributes: map[string]string{"changedFields": "role,lastRoleTransitionAt"},
	}
	tests := []struct {
		name string
		subs []bus.Subscription
	}{
		{name: "ipv4-route", subs: ipv4RouteStatusSubscriptions()},
		{name: "hybrid-route", subs: hybridRouteStatusSubscriptions()},
		{name: "sam", subs: samStatusSubscriptions()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !subscriptionSetAccepts(tt.subs, event) {
				t.Fatalf("%s subscriptions did not accept VirtualAddress status change", tt.name)
			}
		})
	}
}

func TestSAMControllerSubscribesToBGPRouterStatus(t *testing.T) {
	event := daemonapi.DaemonEvent{
		Type: "routerd.resource.status.changed",
		Resource: &daemonapi.ResourceRef{
			APIVersion: api.NetAPIVersion,
			Kind:       "BGPRouter",
			Name:       "lan",
		},
		Attributes: map[string]string{"changedFields": "installedNextHops,peers,phase"},
	}
	if !subscriptionSetAccepts(samStatusSubscriptions(), event) {
		t.Fatal("sam subscriptions did not accept BGPRouter status change")
	}
}

func TestSAMRouteControllersSubscribeToDHCPv4ClientStatus(t *testing.T) {
	event := daemonapi.DaemonEvent{
		Type: "routerd.resource.status.changed",
		Resource: &daemonapi.ResourceRef{
			APIVersion: api.NetAPIVersion,
			Kind:       "DHCPv4Client",
			Name:       "svnet1-source",
		},
		Attributes: map[string]string{"changedFields": "currentAddress,phase"},
	}
	tests := []struct {
		name string
		subs []bus.Subscription
	}{
		{name: "ipv4-route", subs: ipv4RouteStatusSubscriptions()},
		{name: "sam", subs: samStatusSubscriptions()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !subscriptionSetAccepts(tt.subs, event) {
				t.Fatalf("%s subscriptions did not accept DHCPv4Client status change", tt.name)
			}
		})
	}
}

func TestWhenStatusSubscriptionsFollowResourceWhenRefs(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan-resolver"},
			Spec: api.DNSResolverSpec{
				When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
					"VirtualAddress/lan-vip.role": {Equals: "master"},
				}},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPServer"},
			Metadata: api.ObjectMeta{Name: "lan-time"},
			Spec: api.NTPServerSpec{
				When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
					"${VirtualAddress/lan-vip.status.role}": {Equals: "master"},
				}},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec: api.EgressRoutePolicySpec{
				Candidates: []api.EgressRoutePolicyCandidate{
					{
						Name: "dslite",
						When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
							"HealthCheck/internet.phase": {Equals: "Healthy"},
						}},
					},
				},
			},
		},
	}}}

	dnsSubs := whenStatusSubscriptions(router, "DNSResolver")
	if !subscriptionSetAccepts(dnsSubs, statusChangedEvent("VirtualAddress", "lan-vip")) {
		t.Fatal("DNSResolver when subscription did not accept referenced VirtualAddress")
	}
	if subscriptionSetAccepts(dnsSubs, statusChangedEvent("VirtualAddress", "other-vip")) {
		t.Fatal("DNSResolver when subscription accepted unrelated VirtualAddress")
	}
	if subscriptionSetAccepts(dnsSubs, statusChangedEvent("DHCPv4Client", "wan")) {
		t.Fatal("DNSResolver when subscription accepted unrelated kind")
	}

	ntpSubs := whenStatusSubscriptions(router, "NTPServer")
	if !subscriptionSetAccepts(ntpSubs, statusChangedEvent("VirtualAddress", "lan-vip")) {
		t.Fatal("NTPServer when subscription did not accept braced status reference")
	}

	egressSubs := whenStatusSubscriptions(router, "EgressRoutePolicy")
	if !subscriptionSetAccepts(egressSubs, statusChangedEvent("HealthCheck", "internet")) {
		t.Fatal("EgressRoutePolicy when subscription did not accept candidate when reference")
	}
}

func TestStatusSubscriptionsWithWhenMergesStaticAndWhenRefs(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44SessionSync"},
			Metadata: api.ObjectMeta{Name: "conntrack"},
			Spec: api.NAT44SessionSyncSpec{
				When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
					"VirtualAddress/lan-vip.role": {Equals: "master"},
				}},
			},
		},
	}}}

	subs := statusSubscriptionsWithWhen(router, []string{"NAT44SessionSync"}, "NAT44Rule", "RouterdCluster")
	if len(subs) != 1 {
		t.Fatalf("subscriptions = %d, want one merged subscription", len(subs))
	}
	if !subscriptionSetAccepts(subs, statusChangedEvent("NAT44Rule", "lan-to-wan")) {
		t.Fatal("merged subscription did not accept static dependency")
	}
	if !subscriptionSetAccepts(subs, statusChangedEvent("VirtualAddress", "lan-vip")) {
		t.Fatal("merged subscription did not accept when dependency")
	}
	if subscriptionSetAccepts(subs, statusChangedEvent("VirtualAddress", "other-vip")) {
		t.Fatal("merged subscription accepted unrelated when dependency")
	}
}

func TestSAMControllerIgnoresBGPRouterPeerOnlyStatus(t *testing.T) {
	event := daemonapi.DaemonEvent{
		Type: "routerd.resource.status.changed",
		Resource: &daemonapi.ResourceRef{
			APIVersion: api.NetAPIVersion,
			Kind:       "BGPRouter",
			Name:       "lan",
		},
		Attributes: map[string]string{"changedFields": "peers,observedAt"},
	}
	if subscriptionSetAccepts(samStatusSubscriptions(), event) {
		t.Fatal("sam subscriptions accepted BGPRouter peer-only status change")
	}
}

func statusChangedEvent(kind, name string) daemonapi.DaemonEvent {
	return daemonapi.DaemonEvent{
		Type: "routerd.resource.status.changed",
		Resource: &daemonapi.ResourceRef{
			APIVersion: api.NetAPIVersion,
			Kind:       kind,
			Name:       name,
		},
	}
}

func subscriptionSetAccepts(subs []bus.Subscription, event daemonapi.DaemonEvent) bool {
	for _, sub := range subs {
		if sub.Filter == nil || sub.Filter(event) {
			return true
		}
	}
	return false
}
