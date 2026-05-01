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
	for i := range router.Spec.Resources {
		if router.Spec.Resources[i].Kind != "IPv6PrefixDelegation" {
			continue
		}
		spec, err := router.Spec.Resources[i].IPv6PrefixDelegationSpec()
		if err != nil {
			t.Fatalf("read pd spec: %v", err)
		}
		spec.Client = "networkd"
		router.Spec.Resources[i].Spec = spec
	}
	files, err := NetworkdDropins(router)
	if err != nil {
		t.Fatalf("render networkd dropins: %v", err)
	}
	if len(files) != 4 {
		t.Fatalf("len(files) = %d, want 4", len(files))
	}

	raFile := findNetworkdTestFile(files, "10-netplan-ens18.network.d/89-routerd-ipv6-ra.conf")
	wanFile := findNetworkdTestFile(files, "10-netplan-ens18.network.d/90-routerd-dhcp6-pd.conf")
	lanFile := findNetworkdTestFile(files, "10-netplan-ens19.network.d/90-routerd-dhcp6-pd.conf")
	ntpFile := findNetworkdTestFile(files, "10-netplan-ens18.network.d/91-routerd-ntp.conf")
	ra := string(raFile.Data)
	wan := string(wanFile.Data)
	lan := string(lanFile.Data)
	if raFile.Path == "" {
		t.Fatal("missing WAN IPv6 RA drop-in")
	}
	if !strings.Contains(ra, "IPv6AcceptRA=yes") {
		t.Fatalf("ra drop-in missing IPv6AcceptRA=yes:\n%s", ra)
	}
	if wanFile.Path == "" {
		t.Fatal("missing WAN DHCPv6-PD drop-in")
	}
	if !strings.Contains(wan, "DHCP=yes") {
		t.Fatalf("wan drop-in missing DHCP=yes:\n%s", wan)
	}
	if !strings.Contains(wan, "UseDelegatedPrefix=yes") {
		t.Fatalf("wan drop-in missing UseDelegatedPrefix:\n%s", wan)
	}
	if !strings.Contains(wan, "DUIDType=link-layer") {
		t.Fatalf("wan drop-in missing NTT default DUIDType:\n%s", wan)
	}
	if strings.Contains(wan, "DUIDRawData=") {
		t.Fatalf("wan drop-in should not render DUIDRawData when it is unspecified:\n%s", wan)
	}
	if strings.Contains(wan, "PrefixDelegationHint=") {
		t.Fatalf("wan drop-in should not render a prefix hint for NTT profiles:\n%s", wan)
	}
	if strings.Contains(wan, "\nIAID=") {
		t.Fatalf("wan drop-in should not render IAID unless explicitly configured:\n%s", wan)
	}
	if strings.Contains(wan, "SendRelease=") {
		t.Fatalf("wan drop-in should leave DHCPv6 Release behavior to networkd:\n%s", wan)
	}
	if lanFile.Path == "" {
		t.Fatal("missing LAN delegated address drop-in")
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
	ntp := string(ntpFile.Data)
	if ntpFile.Path == "" {
		t.Fatal("missing NTP drop-in")
	}
	if !strings.Contains(ntp, "NTP=pool.ntp.org") {
		t.Fatalf("ntp drop-in missing NTP server:\n%s", ntp)
	}
}

func findNetworkdTestFile(files []File, suffix string) File {
	for _, file := range files {
		if strings.Contains(file.Path, suffix) {
			return file
		}
	}
	return File{}
}

func TestNetworkdDropinsRenderNTTFletsProfile(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{
			Resources: []api.Resource{
				netResource("Interface", "wan", api.InterfaceSpec{IfName: "ens18", Managed: false}),
				netResource("IPv6PrefixDelegation", "wan-pd", api.IPv6PrefixDelegationSpec{
					Interface:   "wan",
					Client:      "networkd",
					Profile:     "ntt-hgw-lan-pd",
					IAID:        "00000001",
					DUIDType:    "link-layer",
					DUIDRawData: "000102005e102030",
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
		"IAID=1",
		"DUIDRawData=00:01:02:00:5e:10:20:30",
		"UseAddress=no",
		"UseDelegatedPrefix=yes",
		"WithoutRA=solicit",
		"RapidCommit=no",
	} {
		if !strings.Contains(wan, want) {
			t.Fatalf("wan drop-in missing %q:\n%s", want, wan)
		}
	}
	if strings.Contains(wan, "PrefixDelegationHint=") {
		t.Fatalf("wan drop-in should not render a prefix hint for NTT profiles:\n%s", wan)
	}
	for _, removed := range []string{"SendHostname=", "UseNTP=", "UseSIP=", "UseCaptivePortal=", "RequestOptions="} {
		if strings.Contains(wan, removed) {
			t.Fatalf("wan drop-in should keep the NTT profile minimal and not render %q:\n%s", removed, wan)
		}
	}
}

func TestNetworkdDropinsRenderGenericPrefixHint(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{
			Resources: []api.Resource{
				netResource("Interface", "wan", api.InterfaceSpec{IfName: "ens18", Managed: false}),
				netResource("IPv6PrefixDelegation", "wan-pd", api.IPv6PrefixDelegationSpec{
					Interface:    "wan",
					Client:       "networkd",
					Profile:      "default",
					PrefixLength: 56,
				}),
			},
		},
	}
	files, err := NetworkdDropins(router)
	if err != nil {
		t.Fatalf("render networkd dropins: %v", err)
	}
	wan := string(files[0].Data)
	if !strings.Contains(wan, "PrefixDelegationHint=::/56") {
		t.Fatalf("generic drop-in missing PrefixDelegationHint:\n%s", wan)
	}
}

func TestNetworkdDropinsSkipExternalPrefixDelegationClient(t *testing.T) {
	for _, client := range []string{"dhcp6c", "dhcpcd"} {
		t.Run(client, func(t *testing.T) {
			router := &api.Router{
				Spec: api.RouterSpec{
					Resources: []api.Resource{
						netResource("Interface", "wan", api.InterfaceSpec{IfName: "ens18", Managed: false}),
						netResource("Interface", "lan", api.InterfaceSpec{IfName: "ens19", Managed: false}),
						netResource("IPv6RAAddress", "wan-ra", api.IPv6RAAddressSpec{
							Interface: "wan",
							Managed:   boolPtr(true),
						}),
						netResource("IPv6PrefixDelegation", "wan-pd", api.IPv6PrefixDelegationSpec{
							Interface: "wan",
							Client:    client,
							Profile:   "ntt-hgw-lan-pd",
						}),
						netResource("IPv6DelegatedAddress", "lan-v6", api.IPv6DelegatedAddressSpec{
							PrefixDelegation: "wan-pd",
							Interface:        "lan",
							AddressSuffix:    "::3",
						}),
					},
				},
			}
			files, err := NetworkdDropins(router)
			if err != nil {
				t.Fatalf("render networkd dropins: %v", err)
			}
			ra := findNetworkdTestFile(files, "10-netplan-ens18.network.d/89-routerd-ipv6-ra.conf")
			if ra.Path == "" {
				t.Fatal("missing RA drop-in")
			}
			for _, want := range []string{"IPv6AcceptRA=yes", "[IPv6AcceptRA]", "DHCPv6Client=no", "UseDNS=no", "UseDomains=no"} {
				if !strings.Contains(string(ra.Data), want) {
					t.Fatalf("RA drop-in missing %q:\n%s", want, string(ra.Data))
				}
			}
			wan := findNetworkdTestFile(files, "10-netplan-ens18.network.d/90-routerd-dhcp6-pd.conf")
			lan := findNetworkdTestFile(files, "10-netplan-ens19.network.d/90-routerd-dhcp6-pd.conf")
			for _, file := range []File{wan, lan} {
				if file.Path == "" {
					t.Fatalf("missing neutralizing DHCPv6-PD drop-in for client=%s: %#v", client, files)
				}
				data := string(file.Data)
				for _, want := range []string{"IPv6AcceptRA=yes", "DHCPv6Client=no", "UseDNS=no", "UseDomains=no"} {
					if !strings.Contains(data, want) {
						t.Fatalf("disabled drop-in missing %q:\n%s", want, data)
					}
				}
				for _, unwanted := range []string{"DHCP=yes", "DHCPPrefixDelegation=yes", "UseDelegatedPrefix=yes"} {
					if strings.Contains(data, unwanted) {
						t.Fatalf("disabled drop-in should not contain %q:\n%s", unwanted, data)
					}
				}
			}
		})
	}
}

func boolPtr(value bool) *bool {
	return &value
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
