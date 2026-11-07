// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestNftablesExcludedAddressSetsStayWithinOnePacketFamily(t *testing.T) {
	for _, dual := range []bool{false, true} {
		set := nftIPAddressSet{Name: "exceptions", IPv4Enabled: true, IPv6Enabled: dual}
		expressions, err := nftFirewallRuleExprs("allow-rest", api.FirewallRuleSpec{Protocol: "tcp", Port: 443, Action: "accept", ExcludeDestinationSetRefs: []string{"exceptions"}}, firewallLogging{}, map[string]nftIPAddressSet{"exceptions": set})
		if err != nil || len(expressions) != 2 {
			t.Fatalf("dual=%v alternatives %v, %v", dual, expressions, err)
		}
		if expressions[0] != "meta nfproto ipv4 ip daddr != @ip_address_set_exceptions_v4 tcp dport 443 counter accept" {
			t.Fatalf("invalid IPv4 exclusion: %s", expressions[0])
		}
		wantV6 := "meta nfproto ipv6 tcp dport 443 counter accept"
		if dual {
			wantV6 = "meta nfproto ipv6 ip6 daddr != @ip_address_set_exceptions_v6 tcp dport 443 counter accept"
		}
		if expressions[1] != wantV6 {
			t.Fatalf("IPv6 traffic disappeared or acquired an IPv4 selector: %s", expressions[1])
		}
	}
}

func TestNftablesIPv6ForwardingUsesZonePolicy(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		netResource("Interface", "wan", api.InterfaceSpec{IfName: "eth0"}),
		netResource("Interface", "lan", api.InterfaceSpec{IfName: "eth1"}),
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
	}}}
	data, err := NftablesFirewall(router, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "meta l4proto ipv6-icmp counter accept") {
		t.Fatal("unconditional ICMPv6 accept bypasses zone policy")
	}
	if !strings.Contains(got, "ip6 hoplimit 255 icmpv6 type { nd-router-solicit, nd-router-advert, nd-neighbor-solicit, nd-neighbor-advert } counter accept") || !strings.Contains(got, "chain wan_to_lan {\n    counter drop") {
		t.Fatalf("lost local ND or untrusted forwarded ICMPv6 filtering:\n%s", data)
	}
}
