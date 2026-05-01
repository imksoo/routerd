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
	if len(files) != 6 {
		t.Fatalf("len(files) = %d, want 6", len(files))
	}

	raFile := findNetworkdTestFile(files, "10-netplan-ens18.network.d/89-routerd-ipv6-ra.conf")
	wanFile := findNetworkdTestFile(files, "10-netplan-ens18.network.d/90-routerd-dhcp6-pd.conf")
	lanFile := findNetworkdTestFile(files, "10-netplan-ens19.network.d/90-routerd-dhcp6-pd.conf")
	ntpFile := findNetworkdTestFile(files, "10-netplan-ens18.network.d/91-routerd-ntp.conf")
	wanRoutesFile := findNetworkdTestFile(files, "10-netplan-ens18.network.d/92-routerd-static-routes.conf")
	lanRoutesFile := findNetworkdTestFile(files, "10-netplan-ens19.network.d/92-routerd-static-routes.conf")
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
	if wanRoutesFile.Path == "" {
		t.Fatal("missing WAN static route drop-in")
	}
	if lanRoutesFile.Path == "" {
		t.Fatal("missing LAN static route drop-in")
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

func TestNetworkdDropinsRenderStaticRoutes(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		netResource("Interface", "wan", api.InterfaceSpec{IfName: "ens18", Managed: true}),
		netResource("IPv4StaticRoute", "lab-v4", api.IPv4StaticRouteSpec{Interface: "wan", Destination: "192.0.2.0/24", Via: "198.51.100.1", Metric: 100}),
		netResource("IPv6StaticRoute", "lab-v6", api.IPv6StaticRouteSpec{Interface: "wan", Destination: "2001:db8:1::/64", Via: "fe80::1", Metric: 200}),
	}}}
	files, err := NetworkdDropins(router)
	if err != nil {
		t.Fatalf("render networkd dropins: %v", err)
	}
	routeFile := findNetworkdTestFile(files, "10-netplan-ens18.network.d/92-routerd-static-routes.conf")
	got := string(routeFile.Data)
	for _, want := range []string{
		"Destination=192.0.2.0/24",
		"Gateway=198.51.100.1",
		"Metric=100",
		"Destination=2001:db8:1::/64",
		"Gateway=fe80::1",
		"Metric=200",
		"GatewayOnLink=yes",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("static route drop-in missing %q:\n%s", want, got)
		}
	}
}

func TestNetworkdDropinsRenderBridge(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		netResource("Interface", "lan", api.InterfaceSpec{IfName: "ens19", Managed: true}),
		netResource("Interface", "uplink", api.InterfaceSpec{IfName: "ens20", Managed: true}),
		netResource("Bridge", "br-home", api.BridgeSpec{
			IfName:            "br0",
			Members:           []string{"lan", "uplink"},
			ForwardDelay:      4,
			HelloTime:         2,
			MTU:               1500,
			MulticastSnooping: boolPtr(false),
		}),
	}}}
	files, err := NetworkdDropins(router)
	if err != nil {
		t.Fatalf("render networkd dropins: %v", err)
	}
	netdev := string(findNetworkdTestFile(files, "/etc/systemd/network/30-routerd-br0.netdev").Data)
	for _, want := range []string{
		"Name=br0",
		"Kind=bridge",
		"MTUBytes=1500",
		"STP=yes",
		"MulticastSnooping=no",
		"ForwardDelaySec=4s",
		"HelloTimeSec=2s",
	} {
		if !strings.Contains(netdev, want) {
			t.Fatalf("bridge netdev missing %q:\n%s", want, netdev)
		}
	}
	lan := string(findNetworkdTestFile(files, "10-netplan-ens19.network.d/88-routerd-bridge.conf").Data)
	if !strings.Contains(lan, "Bridge=br0") {
		t.Fatalf("bridge member drop-in missing Bridge=br0:\n%s", lan)
	}
	uplink := string(findNetworkdTestFile(files, "10-netplan-ens20.network.d/88-routerd-bridge.conf").Data)
	if !strings.Contains(uplink, "Bridge=br0") {
		t.Fatalf("bridge member drop-in missing Bridge=br0:\n%s", uplink)
	}
}

func TestNetworkdDropinsRenderVXLANSegment(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		netResource("Interface", "underlay", api.InterfaceSpec{IfName: "ens18", Managed: true}),
		netResource("Bridge", "br-home", api.BridgeSpec{IfName: "br0", Members: []string{"home-vxlan"}}),
		netResource("VXLANSegment", "home-vxlan", api.VXLANSegmentSpec{
			IfName:            "vxlan100",
			VNI:               100,
			LocalAddress:      "192.0.2.10",
			Remotes:           []string{"192.0.2.20", "192.0.2.30"},
			UnderlayInterface: "underlay",
			UDPPort:           4789,
			MTU:               1450,
			Bridge:            "br-home",
		}),
	}}}
	files, err := NetworkdDropins(router)
	if err != nil {
		t.Fatalf("render networkd dropins: %v", err)
	}
	netdev := string(findNetworkdTestFile(files, "/etc/systemd/network/31-routerd-vxlan100.netdev").Data)
	for _, want := range []string{
		"Name=vxlan100",
		"Kind=vxlan",
		"MTUBytes=1450",
		"VNI=100",
		"Local=192.0.2.10",
		"DestinationPort=4789",
	} {
		if !strings.Contains(netdev, want) {
			t.Fatalf("vxlan netdev missing %q:\n%s", want, netdev)
		}
	}
	network := string(findNetworkdTestFile(files, "/etc/systemd/network/31-routerd-vxlan100.network").Data)
	for _, want := range []string{
		"Name=vxlan100",
		"Bridge=br0",
		"MACAddress=00:00:00:00:00:00",
		"Destination=192.0.2.20",
		"Destination=192.0.2.30",
	} {
		if !strings.Contains(network, want) {
			t.Fatalf("vxlan network missing %q:\n%s", want, network)
		}
	}
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
	for _, client := range []string{"", "dhcp6c", "dhcpcd"} {
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
