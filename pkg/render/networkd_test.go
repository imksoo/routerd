package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/config"
)

func TestNetworkdDropinsRenderDHCPv6PD(t *testing.T) {
	router, err := config.Load("../../examples/router-lab.yaml")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}
	files, err := NetworkdDropins(router)
	if err != nil {
		t.Fatalf("render networkd dropins: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}

	wan := string(files[0].Data)
	lan := string(files[1].Data)
	if !strings.Contains(files[0].Path, "10-netplan-ens18.network.d") {
		t.Fatalf("wan path = %s", files[0].Path)
	}
	if !strings.Contains(wan, "DHCP=yes") {
		t.Fatalf("wan drop-in missing DHCP=yes:\n%s", wan)
	}
	if !strings.Contains(wan, "UseDelegatedPrefix=yes") {
		t.Fatalf("wan drop-in missing UseDelegatedPrefix:\n%s", wan)
	}
	if !strings.Contains(wan, "PrefixDelegationHint=::/60") {
		t.Fatalf("wan drop-in missing PrefixDelegationHint:\n%s", wan)
	}
	if !strings.Contains(files[1].Path, "10-netplan-ens19.network.d") {
		t.Fatalf("lan path = %s", files[1].Path)
	}
	for _, want := range []string{
		"DHCPPrefixDelegation=yes",
		"UplinkInterface=ens18",
		"SubnetId=0",
		"Token=::3",
		"Announce=yes",
	} {
		if !strings.Contains(lan, want) {
			t.Fatalf("lan drop-in missing %q:\n%s", want, lan)
		}
	}
	if strings.Contains(lan, "IPv6SendRA=yes") {
		t.Fatalf("lan drop-in should leave RA to dnsmasq:\n%s", lan)
	}
}

func TestNetworkdDropinsRenderNTTFletsProfile(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{
			Resources: []api.Resource{
				netResource("Interface", "wan", api.InterfaceSpec{IfName: "ens18", Managed: false}),
				netResource("IPv6PrefixDelegation", "wan-pd", api.IPv6PrefixDelegationSpec{
					Interface: "wan",
					Client:    "networkd",
					Profile:   "ntt-hgw-lan-pd",
				}),
			},
		},
	}
	files, err := NetworkdDropins(router)
	if err != nil {
		t.Fatalf("render networkd dropins: %v", err)
	}
	wan := string(files[0].Data)
	for _, want := range []string{
		"DUIDType=link-layer",
		"UseAddress=no",
		"UseDelegatedPrefix=yes",
		"WithoutRA=solicit",
		"RapidCommit=no",
		"PrefixDelegationHint=::/60",
	} {
		if !strings.Contains(wan, want) {
			t.Fatalf("wan drop-in missing %q:\n%s", want, wan)
		}
	}
}

func netResource(kind, name string, spec any) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{
			APIVersion: api.NetAPIVersion,
			Kind:       kind,
		},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     spec,
	}
}
