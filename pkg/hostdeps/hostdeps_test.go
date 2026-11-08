// SPDX-License-Identifier: BSD-3-Clause

package hostdeps

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sysctlprofile"
)

func TestNetworkAdoptionsKeepServerFacingLANOutOfDHCPv6AndRAClientMode(t *testing.T) {
	for _, resource := range []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
			Metadata: api.ObjectMeta{Name: "lan-dhcpv4"},
			Spec:     api.DHCPv4ServerSpec{Interface: "lan"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"},
			Metadata: api.ObjectMeta{Name: "lan-dhcpv6"},
			Spec:     api.DHCPv6ServerSpec{Interface: "lan"},
		},
	} {
		t.Run(resource.Kind, func(t *testing.T) {
			adoptions := NetworkAdoptions(&api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}})
			if len(adoptions) != 1 {
				t.Fatalf("NetworkAdoptions() = %#v, want one LAN adoption", adoptions)
			}
			got := adoptions[0].Spec.SystemdNetworkd
			if !got.DisableDHCPv4 || !got.DisableDHCPv6 || !got.DisableIPv6RA {
				t.Fatalf("server-facing LAN adoption = %#v, want DHCPv4/DHCPv6/RA clients all disabled", got)
			}
		})
	}
}

func TestExplicitSysctlSuppressesDerivedDuplicate(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "custom-ip-forward"}, Spec: api.SysctlSpec{Key: "net.ipv4.ip_forward", Value: "1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-nat"}, Spec: api.NAT44RuleSpec{Type: "masquerade", EgressInterface: "wan"}},
	}}}

	count := 0
	for _, res := range DerivedSysctlResourcesForOS(router, platform.OSLinux) {
		switch res.Kind {
		case "Sysctl":
			spec, err := res.SysctlSpec()
			if err != nil {
				t.Fatal(err)
			}
			if spec.Key == "net.ipv4.ip_forward" {
				count++
			}
		case "SysctlProfile":
			spec, err := res.SysctlProfileSpec()
			if err != nil {
				t.Fatal(err)
			}
			entries, err := sysctlprofile.Entries(spec.Profile, spec.Overrides)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Key == "net.ipv4.ip_forward" {
					count++
				}
			}
		}
	}
	if count != 0 {
		t.Fatalf("derived duplicate ip_forward count = %d, want 0", count)
	}
}
