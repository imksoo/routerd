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

func TestRuntimeWhenControllersSubscribeToStatusRefs(t *testing.T) {
	when := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
		"VirtualAddress/lan-vip.role": {Equals: "master"},
	}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		whenResource(api.ObservabilityAPIVersion, "ObservabilityPipeline", "otlp", api.ObservabilityPipelineSpec{When: when}),
		whenResource(api.SystemAPIVersion, "NTPServer", "lan-time", api.NTPServerSpec{When: when}),
		whenResource(api.NetAPIVersion, "TailscaleNode", "tailnet", api.TailscaleNodeSpec{When: when}),
		whenResource(api.NetAPIVersion, "VirtualAddress", "wan-vip", api.VirtualAddressSpec{When: when}),
		whenResource(api.NetAPIVersion, "BGPRouter", "lan", api.BGPRouterSpec{When: when}),
		whenResource(api.NetAPIVersion, "BGPPeer", "core", api.BGPPeerSpec{When: when}),
		whenResource(api.NetAPIVersion, "BFD", "core", api.BFDSpec{When: when}),
		whenResource(api.NetAPIVersion, "DHCPv4Client", "wan", api.DHCPv4ClientSpec{When: when}),
		whenResource(api.NetAPIVersion, "ClusterNetworkRoute", "default", api.ClusterNetworkRouteSpec{When: when}),
		whenResource(api.NetAPIVersion, "DHCPv4Server", "lan-v4", api.DHCPv4ServerSpec{When: when}),
		whenResource(api.NetAPIVersion, "DHCPv4Reservation", "printer", api.DHCPv4ReservationSpec{When: when}),
		whenResource(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-base", api.IPv6DelegatedAddressSpec{When: when}),
		whenResource(api.NetAPIVersion, "DHCPv6Server", "lan-v6", api.DHCPv6ServerSpec{When: when}),
		whenResource(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan6", api.DHCPv6PrefixDelegationSpec{When: when}),
		whenResource(api.NetAPIVersion, "IPv6RouterAdvertisement", "lan-ra", api.IPv6RouterAdvertisementSpec{When: when}),
		whenResource(api.NetAPIVersion, "DNSResolver", "lan-dns", api.DNSResolverSpec{When: when}),
		whenResource(api.NetAPIVersion, "DNSForwarder", "corp", api.DNSForwarderSpec{When: when}),
		whenResource(api.NetAPIVersion, "DNSUpstream", "corp", api.DNSUpstreamSpec{When: when}),
		whenResource(api.NetAPIVersion, "DHCPv4ServerLeaseSync", "lan-v4", api.DHCPv4ServerLeaseSyncSpec{When: when}),
		whenResource(api.NetAPIVersion, "DHCPv6ServerLeaseSync", "lan-v6", api.DHCPv6ServerLeaseSyncSpec{When: when}),
		whenResource(api.NetAPIVersion, "DHCPv6PrefixDelegationLeaseSync", "wan6-pd", api.DHCPv6PrefixDelegationLeaseSyncSpec{When: when}),
		whenResource(api.NetAPIVersion, "NAT44SessionSync", "conntrack", api.NAT44SessionSyncSpec{When: when}),
		whenResource(api.NetAPIVersion, "DSLiteTunnel", "ds-lite", api.DSLiteTunnelSpec{When: when}),
		whenResource(api.FederationAPIVersion, "EventGroup", "edge", api.EventGroupSpec{When: when}),
		whenResource(api.NetAPIVersion, "HealthCheck", "internet", api.HealthCheckSpec{When: when}),
		whenResource(api.NetAPIVersion, "EgressRoutePolicy", "wan", api.EgressRoutePolicySpec{When: when}),
		whenResource(api.FirewallAPIVersion, "PortForward", "web", api.PortForwardSpec{When: when}),
		whenResource(api.FirewallAPIVersion, "IngressService", "web", api.IngressServiceSpec{When: when}),
		whenResource(api.NetAPIVersion, "NAT44Rule", "lan-to-wan", api.NAT44RuleSpec{When: when}),
		whenResource(api.NetAPIVersion, "IPAddressSet", "dns", api.IPAddressSetSpec{When: when}),
		whenResource(api.FirewallAPIVersion, "LocalServiceRedirect", "dns", api.LocalServiceRedirectSpec{When: when}),
		whenResource(api.FirewallAPIVersion, "FirewallFlowPinhole", "atomcam", api.FirewallFlowPinholeSpec{When: when}),
	}}}
	event := statusChangedEvent("VirtualAddress", "lan-vip")

	tests := []struct {
		name string
		subs []bus.Subscription
	}{
		{name: "observability-pipeline", subs: observabilityPipelineStatusSubscriptions(router)},
		{name: "service-unit", subs: whenStatusSubscriptions(router, "TailscaleNode", "DHCPv4Client", "DHCPv6PrefixDelegation", "IPv6RouterAdvertisement", "DNSResolver", "EventGroup", "HealthCheck")},
		{name: "ntp-server", subs: statusSubscriptionsWithWhen(router, []string{"NTPServer"}, "DHCPv4Client", "DHCPv6Information", "IPv4StaticAddress", "IPv6DelegatedAddress")},
		{name: "dhcpv6-server", subs: allStatusChangedSubscriptions()},
		{name: "dns-resolver", subs: dnsResolverStatusSubscriptions(router)},
		{name: "dhcp-lease-sync", subs: statusSubscriptionsWithWhen(router, []string{"DHCPv4ServerLeaseSync", "DHCPv6ServerLeaseSync", "DHCPv6PrefixDelegationLeaseSync"}, "DHCPv4ServerLeaseSync", "DHCPv6ServerLeaseSync", "DHCPv6PrefixDelegationLeaseSync", "VirtualAddress", "RouterdCluster")},
		{name: "nat44-session-sync", subs: statusSubscriptionsWithWhen(router, []string{"NAT44SessionSync"}, "NAT44SessionSync", "NAT44Rule", "VirtualAddress", "RouterdCluster")},
		{name: "lan-address", subs: statusSubscriptionsWithWhen(router, []string{"DHCPv6PrefixDelegation", "IPv6DelegatedAddress"}, "DHCPv6PrefixDelegation", "Interface")},
		{name: "dslite", subs: statusSubscriptionsWithWhen(router, []string{"DSLiteTunnel"}, "DHCPv6Information", "IPv6DelegatedAddress", "DNSResolver")},
		{name: "ipv4-route", subs: statusSubscriptionsWithWhen(router, []string{"ClusterNetworkRoute"}, "DSLiteTunnel", "TunnelInterface", "EgressRoutePolicy", "VirtualAddress", "DHCPv4Client")},
		{name: "egress-route-policy", subs: statusSubscriptionsWithWhen(router, []string{"EgressRoutePolicy"}, "HealthCheck", "DSLiteTunnel", "Interface", "DHCPv4Client", "PPPoESession")},
		{name: "ingress-service", subs: statusSubscriptionsWithWhen(router, []string{"IngressService"})},
		{name: "nat44", subs: statusSubscriptionsWithWhen(router, []string{"NAT44Rule", "LocalServiceRedirect"}, "EgressRoutePolicy", "IngressService")},
		{name: "bfd", subs: statusSubscriptionsWithWhen(router, []string{"BFD"}, "BGPPeer", "BFD")},
		{name: "bgp", subs: statusSubscriptionsWithWhen(router, []string{"BGPRouter", "BGPPeer"}, "BFD", "BGPRouter", "BGPPeer")},
		{name: "vrrp", subs: statusSubscriptionsWithWhen(router, []string{"VirtualAddress"}, "BGPRouter", "BGPPeer", "IngressService")},
		{name: "ip-address-set", subs: statusSubscriptionsWithWhen(router, []string{"IPAddressSet", "LocalServiceRedirect", "FirewallFlowPinhole"}, "IPAddressSet", "LocalServiceRedirect", "FirewallFlowPinhole")},
		{name: "firewall", subs: firewallStatusSubscriptions(router)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !subscriptionSetAccepts(tt.subs, event) {
				t.Fatalf("%s subscriptions did not accept referenced when status change", tt.name)
			}
		})
	}
}

func TestPeriodicOnlyControllersUseBootstrapSubscriptions(t *testing.T) {
	bootstrap := daemonapi.DaemonEvent{Type: "routerd.controller.bootstrap"}
	status := statusChangedEvent("HealthCheck", "internet")
	for _, tt := range []struct {
		name string
		subs []bus.Subscription
	}{
		{name: "package", subs: bootstrapSubscriptions()},
		{name: "kernel-module", subs: bootstrapSubscriptions()},
		{name: "sysctl", subs: bootstrapSubscriptions()},
		{name: "network-adoption", subs: bootstrapSubscriptions()},
		{name: "log-retention", subs: bootstrapSubscriptions()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !subscriptionSetAccepts(tt.subs, bootstrap) {
				t.Fatalf("%s subscriptions did not accept bootstrap", tt.name)
			}
			if subscriptionSetAccepts(tt.subs, status) {
				t.Fatalf("%s subscriptions accepted unrelated status change", tt.name)
			}
		})
	}
}

func TestHighChurnStatusDoesNotWakeDNSFirewallOrRetention(t *testing.T) {
	event := daemonapi.DaemonEvent{
		Type: "routerd.resource.status.changed",
		Resource: &daemonapi.ResourceRef{
			APIVersion: api.NetAPIVersion,
			Kind:       "BGPRouter",
			Name:       "lan",
		},
		Attributes: map[string]string{"changedFields": "acceptedPrefixes,withdrawnPrefixes,observedAt"},
	}
	for _, tt := range []struct {
		name string
		subs []bus.Subscription
	}{
		{name: "dns-resolver", subs: dnsResolverStatusSubscriptions(&api.Router{})},
		{name: "firewall", subs: firewallStatusSubscriptions(&api.Router{})},
		{name: "log-retention", subs: bootstrapSubscriptions()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if subscriptionSetAccepts(tt.subs, event) {
				t.Fatalf("%s accepted unrelated BGPRouter status change", tt.name)
			}
		})
	}
}

func TestDNSResolverSubscriptionsKeepLeaseAndDNSEvents(t *testing.T) {
	subs := dnsResolverStatusSubscriptions(&api.Router{})
	if !subscriptionSetAccepts(subs, statusChangedEvent("DNSResolver", "lan-resolver")) {
		t.Fatal("dns-resolver did not accept DNSResolver status change")
	}
	if !subscriptionSetAccepts(subs, daemonapi.DaemonEvent{Type: "routerd.dhcp.lease.add"}) {
		t.Fatal("dns-resolver did not accept DHCP lease event")
	}
}

func TestFirewallSubscriptionsKeepFirewallEvents(t *testing.T) {
	subs := firewallStatusSubscriptions(&api.Router{})
	if !subscriptionSetAccepts(subs, daemonapi.DaemonEvent{
		Type: "routerd.resource.status.changed",
		Resource: &daemonapi.ResourceRef{
			APIVersion: api.FirewallAPIVersion,
			Kind:       "FirewallRule",
			Name:       "allow-dns",
		},
	}) {
		t.Fatal("firewall did not accept FirewallRule status change")
	}
	if !subscriptionSetAccepts(subs, daemonapi.DaemonEvent{Type: "routerd.firewall.rules.applied"}) {
		t.Fatal("firewall did not accept firewall event")
	}
}

func TestServiceUnitIgnoresHealthCheckTimestampOnlyStatus(t *testing.T) {
	subs := serviceUnitStatusSubscriptions(&api.Router{})
	event := statusChangedEvent("HealthCheck", "internet")
	event.Attributes = map[string]string{"changedFields": "lastSuccessTime"}
	if subscriptionSetAccepts(subs, event) {
		t.Fatal("service-unit accepted HealthCheck lastSuccessTime-only status change")
	}
	event.Attributes = map[string]string{"changedFields": "phase,lastSuccessTime"}
	if !subscriptionSetAccepts(subs, event) {
		t.Fatal("service-unit did not accept HealthCheck phase status change")
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
		if len(sub.Topics) > 0 {
			matched := false
			for _, topic := range sub.Topics {
				if bus.MatchTopic(topic, event.Type) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if sub.Filter == nil || sub.Filter(event) {
			return true
		}
	}
	return false
}

func whenResource(apiVersion, kind, name string, spec any) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: apiVersion, Kind: kind},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     spec,
	}
}

func allStatusChangedSubscriptions() []bus.Subscription {
	return []bus.Subscription{{Topics: []string{"routerd.resource.status.changed"}}}
}
