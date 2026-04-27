package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestNixOSModuleRendersHostUsersInterfacesAndDependencies(t *testing.T) {
	enabled := true
	disabled := false
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NixOSHost"},
				Metadata: api.ObjectMeta{Name: "router02"},
				Spec: api.NixOSHostSpec{
					Hostname:            "router02",
					Domain:              "lain.local",
					StateVersion:        "25.11",
					Boot:                api.NixOSBootSpec{Loader: "grub", GrubDevice: "/dev/sda"},
					DebugSystemPackages: true,
					SSH: api.NixOSSSHSpec{
						Enabled:                &enabled,
						PasswordAuthentication: &enabled,
						PermitRootLogin:        "no",
					},
					Sudo: api.NixOSSudoSpec{WheelNeedsPassword: &disabled},
					Users: []api.NixOSUserSpec{
						{
							Name:              "nwadmin",
							Groups:            []string{"wheel"},
							InitialPassword:   "nwadmin",
							SSHAuthorizedKeys: []string{"ssh-ed25519 AAAA test"},
						},
					},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external", AdminUp: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPAddress"},
				Metadata: api.ObjectMeta{Name: "wan-dhcp4"},
				Spec:     api.IPv4DHCPAddressSpec{Interface: "wan"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DHCPAddress"},
				Metadata: api.ObjectMeta{Name: "wan-dhcp6"},
				Spec:     api.IPv6DHCPAddressSpec{Interface: "wan"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4SourceNAT"},
				Metadata: api.ObjectMeta{Name: "lan-nat"},
				Spec: api.IPv4SourceNATSpec{
					OutboundInterface: "wan",
					SourceCIDRs:       []string{"192.168.160.0/24"},
					Translation:       api.IPv4NATTranslationSpec{Type: "interfaceAddress"},
				},
			},
		}},
	}
	data, err := NixOSModule(router)
	if err != nil {
		t.Fatalf("render NixOS module: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`networking.hostName = "router02";`,
		`networking.domain = "lain.local";`,
		`boot.loader.grub.device = "/dev/sda";`,
		`systemd.network.networks."10-routerd-ens18"`,
		`DHCP = "yes";`,
		`IPv6AcceptRA = true;`,
		`systemd.network.networks."10-routerd-ens19"`,
		`LinkLocalAddressing = "no";`,
		`users.users.nwadmin`,
		`initialPassword = "nwadmin";`,
		`security.sudo.wheelNeedsPassword = false;`,
		`nftables`,
		`systemd.services.routerd.path`,
		`system.stateVersion = "25.11";`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("NixOS module missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "netplan") {
		t.Fatalf("NixOS module should not reference netplan:\n%s", got)
	}
}
