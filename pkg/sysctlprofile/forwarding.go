// SPDX-License-Identifier: BSD-3-Clause

package sysctlprofile

import "routerd/pkg/api"

func ForwardingEntries(router *api.Router) []Entry {
	if !NeedsForwarding(router) {
		return nil
	}
	return []Entry{
		{Key: "net.ipv4.ip_forward", Value: "1"},
		{Key: "net.ipv6.conf.all.forwarding", Value: "1"},
	}
}

func NeedsForwarding(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "IngressService", "PortForward", "NAT44Rule", "IPv4SourceNAT", "BGPRouter", "BGPPeer", "ClusterNetworkRoute", "IPv4StaticRoute", "IPv6StaticRoute", "IPv4PolicyRoute", "IPv4DefaultRoutePolicy", "EgressRoutePolicy", "DSLiteTunnel", "WireGuardInterface", "VXLANTunnel", "VRF":
			return true
		}
	}
	return false
}
