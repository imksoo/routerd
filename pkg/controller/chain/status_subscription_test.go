// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	mobilitycontroller "github.com/imksoo/routerd/pkg/controller/mobility"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
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

func TestVRRPFastTransitionWakesRoleObservation(t *testing.T) {
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "keepalived", Kind: "keepalived"}, daemonapi.EventVRRPRoleTransition, daemonapi.SeverityInfo)
	if !subscriptionSetAccepts(vrrpStatusSubscriptions(&api.Router{}), event) {
		t.Fatal("vrrp controller did not subscribe to the keepalived transition fast path")
	}
}

func TestVRRPGracefulActivationSubscribesToReadinessDependencies(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
		Metadata: api.ObjectMeta{Name: "lan-gw-v4"},
		Spec: api.VirtualAddressSpec{VRRP: api.VirtualAddressVRRPSpec{GracefulActivation: &api.VirtualAddressVRRPGracefulActivationSpec{
			ReadyWhen: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
				"DHCPv6PrefixDelegation/wan-pd.phase":          {Equals: "Bound"},
				"DSLiteTunnel/dslite-a.phase":                  {Equals: "Up"},
				"EgressRoutePolicy/ipv4-default.datapathState": {Equals: "Ready"},
			}},
		}}},
	}}}}
	subs := vrrpStatusSubscriptions(router)
	for _, ref := range [][2]string{{"DHCPv6PrefixDelegation", "wan-pd"}, {"DSLiteTunnel", "dslite-a"}, {"EgressRoutePolicy", "ipv4-default"}} {
		if !subscriptionSetAccepts(subs, statusChangedEvent(ref[0], ref[1])) {
			t.Fatalf("vrrp graceful activation did not subscribe to %s/%s", ref[0], ref[1])
		}
	}
	if subscriptionSetAccepts(subs, statusChangedEvent("DSLiteTunnel", "unrelated")) {
		t.Fatal("vrrp graceful activation subscribed to an unrelated DS-Lite tunnel")
	}
}

func TestVRRPRoleStatusWakesHADatapathControllers(t *testing.T) {
	masterWhen := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
		"VirtualAddress/lan-gw.role": {Equals: "master"},
	}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "dslite"}, Spec: api.DSLiteTunnelSpec{When: masterWhen}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "internet"}, Spec: api.HealthCheckSpec{When: masterWhen}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{Mode: "priority", Candidates: []api.EgressRoutePolicyCandidate{{Name: "dslite", When: masterWhen}}}},
	}}}
	event := statusChangedEvent("VirtualAddress", "lan-gw")
	tests := map[string][]bus.Subscription{
		"dslite":              statusSubscriptionsWithWhen(router, []string{"DSLiteTunnel"}, "DHCPv6Information", "IPv6DelegatedAddress", "DNSResolver"),
		"healthcheck":         serviceUnitStatusSubscriptions(router),
		"egress-route-policy": statusSubscriptionsWithWhen(router, []string{"EgressRoutePolicy"}, "HealthCheck", "DSLiteTunnel", "Interface", "DHCPv4Client", "PPPoESession"),
		"ipv4-policy-route":   statusSubscriptions("DSLiteTunnel", "HealthCheck", "IPv4StaticAddress", "Interface", "VirtualAddress"),
	}
	for name, subscriptions := range tests {
		if !subscriptionSetAccepts(subscriptions, event) {
			t.Fatalf("%s did not accept the VRRP role status transition", name)
		}
	}
}

func TestLocalMobilityEffectorsSubscribeToTypedMobilityPoolPlan(t *testing.T) {
	event := daemonapi.DaemonEvent{
		Type: mobilitycontroller.PoolPlanChangedEvent,
		Resource: &daemonapi.ResourceRef{
			APIVersion: api.MobilityAPIVersion,
			Kind:       "MobilityPool",
			Name:       "cloudedge",
		},
		Attributes: map[string]string{"source": "MobilityPool/cloudedge/node/router-a", "digest": "sha256:plan"},
	}
	router := &api.Router{}
	for name, subscriptions := range map[string][]bus.Subscription{
		"bgp":                 bgpStatusSubscriptions(router),
		"ipv4-route":          ipv4RouteStatusSubscriptions(),
		"ipv4-static-address": ipv4StaticAddressStatusSubscriptions(),
		"path-mtu":            pathMTUStatusSubscriptions(router),
		"firewall":            firewallStatusSubscriptions(router),
		"sam":                 samStatusSubscriptions(),
	} {
		if !subscriptionSetAccepts(subscriptions, event) {
			t.Fatalf("%s subscriptions did not accept typed MobilityPool plan update", name)
		}
	}
}

func TestSAMControllerIgnoresBGPRouterStatus(t *testing.T) {
	event := daemonapi.DaemonEvent{
		Type: "routerd.resource.status.changed",
		Resource: &daemonapi.ResourceRef{
			APIVersion: api.NetAPIVersion,
			Kind:       "BGPRouter",
			Name:       "lan",
		},
		Attributes: map[string]string{"changedFields": "installedNextHops,prefixes,phase,fibRoutes,fibUnsupportedRoutes"},
	}
	if subscriptionSetAccepts(samStatusSubscriptions(), event) {
		t.Fatal("sam subscriptions accepted legacy BGPRouter status wake-up")
	}
}

func TestDirectMeshRecoverySubscriptionsFollowEnrollmentToTransportToBGP(t *testing.T) {
	enrollment := statusChangedEvent("SAMEnrollmentClient", "svnet1")
	if !subscriptionSetAccepts(samTransportStatusSubscriptions(), enrollment) {
		t.Fatal("sam-transport did not accept SAMEnrollmentClient topology handoff")
	}
	transport := statusChangedEvent("SAMTransportProfile", "svnet1")
	if !subscriptionSetAccepts(bgpStatusSubscriptions(&api.Router{}), transport) {
		t.Fatal("bgp did not accept SAMTransportProfile direct-peer handoff")
	}
}

func TestDynamicConfigPartChangeWakesSAMConsumers(t *testing.T) {
	event := daemonapi.DaemonEvent{
		Type: dynamicconfig.PartChangedEvent,
		Attributes: map[string]string{
			"source": "SAMTransportProfile/svnet1/node/pve-rt-01",
			"digest": "sha256:changed",
		},
	}
	router := &api.Router{}
	for name, subscriptions := range map[string][]bus.Subscription{
		"sam-transport": samTransportStatusSubscriptions(),
		"tunnel":        withDynamicConfigPartSubscriptions(statusSubscriptions("TunnelInterface", "SAMTransportProfile")),
		"ipv4-route":    ipv4RouteControllerStatusSubscriptions(router),
		"hybrid-route":  hybridRouteStatusSubscriptions(),
		"sam":           samStatusSubscriptions(),
		"bfd":           withDynamicConfigPartSubscriptions(statusSubscriptionsWithWhen(router, []string{"BFD"}, "BGPPeer", "BFD", "SAMTransportProfile")),
		"bgp":           bgpStatusSubscriptions(router),
	} {
		if !subscriptionSetAccepts(subscriptions, event) {
			t.Fatalf("%s subscriptions did not accept DynamicConfigPart change", name)
		}
	}

	invalid := event
	invalid.Attributes = map[string]string{"source": "SAMTransportProfile/svnet1/node/pve-rt-01"}
	if subscriptionSetAccepts(samTransportStatusSubscriptions(), invalid) {
		t.Fatal("sam-transport accepted DynamicConfigPart event without a digest")
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
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"},
			Metadata: api.ObjectMeta{Name: "overlay"},
			Spec: api.VXLANTunnelSpec{When: api.ResourceWhenSpec{All: []api.ResourceWhenSpec{
				{State: map[string]api.StateMatchSpec{"${VirtualAddress/overlay-vip.status.role}": {Equals: "master"}}},
				{State: map[string]api.StateMatchSpec{"${RouterdCluster/overlay-ha.status.phase}": {Equals: "Leader"}}},
			}}},
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

	vxlanSubs := statusSubscriptionsWithWhen(router, []string{"VXLANTunnel"}, "Bridge", "WireGuardInterface")
	if !subscriptionSetAccepts(vxlanSubs, statusChangedEvent("VirtualAddress", "overlay-vip")) ||
		!subscriptionSetAccepts(vxlanSubs, statusChangedEvent("RouterdCluster", "overlay-ha")) {
		t.Fatal("VXLANTunnel did not subscribe to both role and witness dependencies")
	}
	if subscriptionSetAccepts(vxlanSubs, statusChangedEvent("VirtualAddress", "other-vip")) {
		t.Fatal("VXLANTunnel accepted an unrelated role event")
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
		{name: "bgp", subs: bgpStatusSubscriptions(router)},
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
	if !subscriptionSetAccepts(subs, daemonapi.DaemonEvent{Type: daemonapi.EventDHCPLeaseAdded}) {
		t.Fatal("dns-resolver did not accept DHCP lease event")
	}
	if subscriptionSetAccepts(subs, daemonapi.DaemonEvent{Type: "routerd.dhcp.lease.add"}) {
		t.Fatal("dns-resolver accepted removed DHCP lease topic")
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
