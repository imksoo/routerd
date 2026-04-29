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

func TestIAIDFromMACUsesLastFourOctets(t *testing.T) {
	got, ok := iaidFromMAC("02:00:00:12:34:56")
	if !ok {
		t.Fatal("iaidFromMAC returned !ok")
	}
	if got != 0x00123456 {
		t.Fatalf("iaidFromMAC = %#x, want 0x00123456", got)
	}
}

func TestEffectiveIPv6PDIAIDPrefersConfiguredValue(t *testing.T) {
	got := effectiveIPv6PDIAID(api.IPv6PDProfileNTTHGWLANPD, "00000001", "no-such-interface")
	if got != "00000001" {
		t.Fatalf("effectiveIPv6PDIAID = %q, want configured value", got)
	}
}

func TestEffectiveIPv6PDIAIDDoesNotDefaultForGenericProfile(t *testing.T) {
	got := effectiveIPv6PDIAID(api.IPv6PDProfileDefault, "", "no-such-interface")
	if got != "" {
		t.Fatalf("effectiveIPv6PDIAID = %q, want empty for generic profile", got)
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
