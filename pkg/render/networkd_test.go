package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/config"
)

func TestNetworkdDropinsDoNotRenderLegacyDHCPv6PD(t *testing.T) {
	router, err := config.Load("../../examples/router-lab.yaml")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}
	files, err := NetworkdDropins(router)
	if err != nil {
		t.Fatalf("render networkd dropins: %v", err)
	}

	ntpFile := findNetworkdTestFile(files, "10-netplan-ens18.network.d/91-routerd-ntp.conf")
	wanRoutesFile := findNetworkdTestFile(files, "10-netplan-ens18.network.d/92-routerd-static-routes.conf")
	lanRoutesFile := findNetworkdTestFile(files, "10-netplan-ens19.network.d/92-routerd-static-routes.conf")
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
	for _, file := range files {
		if strings.Contains(file.Path, "89-routerd-ipv6-ra.conf") || strings.Contains(file.Path, "90-routerd-dhcp6-pd.conf") {
			t.Fatalf("legacy DHCPv6/RA networkd drop-in should not be rendered: %s\n%s", file.Path, file.Data)
		}
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
		netResource("IPv4StaticAddress", "br-home-ipv4", api.IPv4StaticAddressSpec{Interface: "br-home", Address: "192.0.2.1/24"}),
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
	network := string(findNetworkdTestFile(files, "/etc/systemd/network/30-routerd-br0.network").Data)
	if !strings.Contains(network, "Address=192.0.2.1/24") {
		t.Fatalf("bridge network missing static address:\n%s", network)
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
		netResource("IPv4StaticAddress", "home-vxlan-ipv4", api.IPv4StaticAddressSpec{Interface: "home-vxlan", Address: "192.0.2.2/24"}),
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
		"Independent=yes",
		"UDPChecksum=yes",
		"PortRange=4789-4789",
		"Remote=192.0.2.20",
		"DestinationPort=4789",
	} {
		if !strings.Contains(netdev, want) {
			t.Fatalf("vxlan netdev missing %q:\n%s", want, netdev)
		}
	}
	network := string(findNetworkdTestFile(files, "/etc/systemd/network/31-routerd-vxlan100.network").Data)
	for _, want := range []string{
		"Name=vxlan100",
		"Address=192.0.2.2/24",
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
