// SPDX-License-Identifier: BSD-3-Clause

package apply

import "testing"

func TestParseIPv4RouteTableArtifactsIncludesDefaultDev(t *testing.T) {
	got := parseIPv4RouteTableArtifacts(`
default dev ppp0 table 111 metric 10
192.0.2.0/24 dev ppp0 table 111 proto kernel scope link
192.168.1.0/24 dev ens18 table 112 proto kernel scope link
default via 192.168.1.1 dev ens18 table 112 metric 600
`)
	if len(got) != 2 {
		t.Fatalf("route table artifacts = %+v, want two", got)
	}
	if got[0].Name != "table=111" || got[0].Attributes["ifname"] != "ppp0" {
		t.Fatalf("first artifact = %+v, want table=111 ifname=ppp0", got[0])
	}
	if got[1].Name != "table=112" || got[1].Attributes["ifname"] != "ens18" {
		t.Fatalf("second artifact = %+v, want table=112 ifname=ens18", got[1])
	}
}

func TestParseIfconfigAddressArtifacts(t *testing.T) {
	got := parseIfconfigAddressArtifacts(`vtnet1: flags=1008843<UP,BROADCAST,RUNNING,SIMPLEX,MULTICAST,LOWER_UP> metric 0 mtu 1500
	inet 192.168.10.1 netmask 0xffffff00 broadcast 192.168.10.255
	inet6 fe80::be24:11ff:fe5d:e063%vtnet1 prefixlen 64 scopeid 0x2
`)
	if len(got) != 2 {
		t.Fatalf("ifconfig artifacts = %+v, want two", got)
	}
	if got[0].Kind != "net.ipv4.address" || got[0].Name != "vtnet1:192.168.10.1/24" {
		t.Fatalf("first artifact = %+v", got[0])
	}
	if got[1].Kind != "net.ipv6.address" || got[1].Name != "vtnet1:fe80::be24:11ff:fe5d:e063/64" {
		t.Fatalf("second artifact = %+v", got[1])
	}
}
