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
				Metadata: api.ObjectMeta{Name: "nixos-edge"},
				Spec: api.NixOSHostSpec{
					Hostname:            "nixos-edge",
					Domain:              "example.internal",
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
							Name:              "admin",
							Groups:            []string{"wheel"},
							InitialPassword:   "change-me",
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticRoute"},
				Metadata: api.ObjectMeta{Name: "lab-v4"},
				Spec:     api.IPv4StaticRouteSpec{Interface: "wan", Destination: "192.0.2.0/24", Via: "198.51.100.1", Metric: 100},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6StaticRoute"},
				Metadata: api.ObjectMeta{Name: "lab-v6"},
				Spec:     api.IPv6StaticRouteSpec{Interface: "wan", Destination: "2001:db8:1::/64", Via: "fe80::1", Metric: 200},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DHCPAddress"},
				Metadata: api.ObjectMeta{Name: "wan-dhcp6"},
				Spec:     api.IPv6DHCPAddressSpec{Interface: "wan"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec:     api.IPv6PrefixDelegationSpec{Interface: "wan", Profile: "ntt-hgw-lan-pd"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"},
				Metadata: api.ObjectMeta{Name: "br-home"},
				Spec:     api.BridgeSpec{IfName: "br0", Members: []string{"lan"}, RSTP: boolPtr(true)},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "mgmt"},
				Spec:     api.InterfaceSpec{IfName: "ens20", Managed: true, Owner: "routerd"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPAddress"},
				Metadata: api.ObjectMeta{Name: "mgmt-dhcp4"},
				Spec:     api.IPv4DHCPAddressSpec{Interface: "mgmt", UseRoutes: &disabled, UseDNS: &disabled, RouteMetric: 900},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4SourceNAT"},
				Metadata: api.ObjectMeta{Name: "lan-nat"},
				Spec: api.IPv4SourceNATSpec{
					OutboundInterface: "wan",
					SourceCIDRs:       []string{"192.168.10.0/24"},
					Translation:       api.IPv4NATTranslationSpec{Type: "interfaceAddress"},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"},
				Metadata: api.ObjectMeta{Name: "time"},
				Spec: api.NTPClientSpec{
					Provider: "systemd-timesyncd",
					Managed:  true,
					Source:   "static",
					Servers:  []string{"pool.ntp.org"},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"},
				Metadata: api.ObjectMeta{Name: "forwarding"},
				Spec: api.SysctlSpec{
					Key:        "net.ipv4.ip_forward",
					Value:      "1",
					Persistent: true,
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
		`networking.hostName = "nixos-edge";`,
		`networking.domain = "example.internal";`,
		`boot.loader.grub.device = "/dev/sda";`,
		`networking.firewall.checkReversePath = false;`,
		`systemd.network.networks."10-netplan-ens18"`,
		`DHCP = "yes";`,
		`IPv6AcceptRA = true;`,
		`systemd.network.networks."10-netplan-ens19"`,
		`Bridge = "br0";`,
		`systemd.network.netdevs."30-routerd-br0"`,
		`Kind = "bridge";`,
		`STP = true;`,
		`MulticastSnooping = false;`,
		`LinkLocalAddressing = "no";`,
		`systemd.network.networks."10-netplan-ens20"`,
		`DHCP = "ipv4";`,
		`UseRoutes = false;`,
		`UseDNS = false;`,
		`RouteMetric = 900;`,
		`Destination = "192.0.2.0/24";`,
		`Gateway = "198.51.100.1";`,
		`Metric = 100;`,
		`Destination = "2001:db8:1::/64";`,
		`Gateway = "fe80::1";`,
		`Metric = 200;`,
		`users.users.admin`,
		`initialPassword = "change-me";`,
		`security.sudo.wheelNeedsPassword = false;`,
		`services.timesyncd.servers = [ "pool.ntp.org" ];`,
		`"net.ipv4.ip_forward" = 1;`,
		`nftables`,
		`system.stateVersion = "25.11";`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("NixOS module missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "pkgs.netplan") || strings.Contains(got, "\n    netplan\n") {
		t.Fatalf("NixOS module should not depend on netplan:\n%s", got)
	}
	if strings.Contains(got, "systemd.services.routerd") {
		t.Fatalf("NixOS module must not emit routerd service unless requested:\n%s", got)
	}
}

func TestNixOSModuleRendersOptionalRouterdService(t *testing.T) {
	enabled := true
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NixOSHost"},
				Metadata: api.ObjectMeta{Name: "nixos-edge"},
				Spec: api.NixOSHostSpec{
					RouterdService: api.NixOSRouterdServiceSpec{
						Enabled:       &enabled,
						BinaryPath:    "/usr/local/sbin/routerd",
						ConfigFile:    "/usr/local/etc/routerd/router.yaml",
						Socket:        "/run/routerd/routerd.sock",
						ApplyInterval: "60s",
						ExtraFlags:    []string{"--status-file", "/run/routerd/status.json"},
					},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPServer"},
				Metadata: api.ObjectMeta{Name: "dhcp4"},
				Spec:     api.IPv4DHCPServerSpec{Server: "dnsmasq", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"},
				Metadata: api.ObjectMeta{Name: "default-home"},
				Spec:     api.FirewallPolicySpec{Preset: "home-router"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface"},
				Metadata: api.ObjectMeta{Name: "pppoe"},
				Spec: api.PPPoEInterfaceSpec{
					Interface: "wan",
					Username:  "open@open.ad.jp",
					Password:  "open",
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
		`systemd.services.routerd = {`,
		`description = "routerd declarative router controller";`,
		`wantedBy = [ "multi-user.target" ];`,
		`dnsmasq`,
		`dnsutils`,
		`iputils`,
		`nftables`,
		`ppp`,
		`tcpdump`,
		`traceroute`,
		`"/usr/local/sbin/routerd"`,
		`"serve"`,
		`"--config"`,
		`"/usr/local/etc/routerd/router.yaml"`,
		`"--apply-interval"`,
		`"60s"`,
		`"--status-file"`,
		`"/run/routerd/status.json"`,
		`RuntimeDirectory = "routerd";`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("NixOS routerd service missing %q:\n%s", want, got)
		}
	}
}

func TestNixOSModuleRejectsDHCP6CWithoutPackagePath(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NixOSHost"},
				Metadata: api.ObjectMeta{Name: "host"},
				Spec:     api.NixOSHostSpec{DebugSystemPackages: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec: api.IPv6PrefixDelegationSpec{
					Interface: "wan",
					Client:    "dhcp6c",
					Profile:   "ntt-hgw-lan-pd",
				},
			},
		}},
	}
	_, err := NixOSModule(router)
	if err == nil || !strings.Contains(err.Error(), "client=dhcp6c") {
		t.Fatalf("NixOSModule error = %v, want dhcp6c unsupported error", err)
	}
}

func TestNixOSModuleRendersDHCPCDPackageWithoutNetworkdDHCPv6(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NixOSHost"},
				Metadata: api.ObjectMeta{Name: "host"},
				Spec:     api.NixOSHostSpec{DebugSystemPackages: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec: api.IPv6PrefixDelegationSpec{
					Interface: "wan",
					Profile:   "ntt-hgw-lan-pd",
				},
			},
		}},
	}
	data, err := NixOSModule(router)
	if err != nil {
		t.Fatalf("render NixOS module: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "dhcpcd") {
		t.Fatalf("NixOS module should include dhcpcd for omitted NTT-profile client:\n%s", got)
	}
	if strings.Contains(got, `DHCP = "ipv6";`) || strings.Contains(got, `DHCP = "yes";`) {
		t.Fatalf("NixOS module should not enable networkd DHCPv6 for client=dhcpcd:\n%s", got)
	}
	if !strings.Contains(got, "IPv6AcceptRA = true;") {
		t.Fatalf("NixOS module should still accept RA for client=dhcpcd:\n%s", got)
	}
}
