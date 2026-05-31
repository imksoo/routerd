// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestDistinctRoutedMeshFourSiteFixture(t *testing.T) {
	sites := routedMeshSites()
	assertRoutedMeshAddressPlanDistinct(t, sites)

	for _, site := range sites {
		t.Run(site.name, func(t *testing.T) {
			router := routedMeshRouter(site, sites)
			if err := Validate(router); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			assertNoMobilityResources(t, router)
			assertRoutedMeshRoutes(t, router, site, sites)
			assertRoutedMeshWireGuardPeers(t, router, site, sites)
		})
	}
}

type routedMeshSite struct {
	name      string
	router    string
	lanPrefix string
	routerLAN string
	client    string
	overlay   string
	endpoint  string
	publicKey string
}

func routedMeshSites() []routedMeshSite {
	return []routedMeshSite{
		{
			name:      "onprem",
			router:    "onprem-router",
			lanPrefix: "10.80.10.0/24",
			routerLAN: "10.80.10.1/24",
			client:    "10.80.10.10",
			overlay:   "10.99.0.1/32",
			endpoint:  "198.51.100.11:51820",
			publicKey: "ONPREM_ROUTER_PUBLIC_KEY",
		},
		{
			name:      "aws",
			router:    "aws-router",
			lanPrefix: "10.80.20.0/24",
			routerLAN: "10.80.20.1/24",
			client:    "10.80.20.10",
			overlay:   "10.99.0.2/32",
			endpoint:  "198.51.100.12:51820",
			publicKey: "AWS_ROUTER_PUBLIC_KEY",
		},
		{
			name:      "azure",
			router:    "azure-router",
			lanPrefix: "10.80.30.0/24",
			routerLAN: "10.80.30.1/24",
			client:    "10.80.30.10",
			overlay:   "10.99.0.3/32",
			endpoint:  "198.51.100.13:51820",
			publicKey: "AZURE_ROUTER_PUBLIC_KEY",
		},
		{
			name:      "oci",
			router:    "oci-router",
			lanPrefix: "10.80.40.0/24",
			routerLAN: "10.80.40.1/24",
			client:    "10.80.40.10",
			overlay:   "10.99.0.4/32",
			endpoint:  "198.51.100.14:51820",
			publicKey: "OCI_ROUTER_PUBLIC_KEY",
		},
	}
}

func routedMeshRouter(self routedMeshSite, sites []routedMeshSite) *api.Router {
	resources := []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "lan0", Managed: true, Owner: "routerd", AdminUp: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
			Metadata: api.ObjectMeta{Name: "lan-ipv4"},
			Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: self.routerLAN},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
			Metadata: api.ObjectMeta{Name: "wg-mesh"},
			Spec: api.WireGuardInterfaceSpec{
				PrivateKeyFile: "/usr/local/etc/routerd/secrets/" + self.router + "-wg.key",
				ListenPort:     51820,
				MTU:            1420,
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
			Metadata: api.ObjectMeta{Name: "wg-mesh-ipv4"},
			Spec:     api.IPv4StaticAddressSpec{Interface: "wg-mesh", Address: self.overlay},
		},
	}
	for _, remote := range sites {
		if remote.name == self.name {
			continue
		}
		resources = append(resources,
			api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"},
				Metadata: api.ObjectMeta{Name: remote.name},
				Spec: api.WireGuardPeerSpec{
					Interface:           "wg-mesh",
					PublicKey:           remote.publicKey,
					Endpoint:            remote.endpoint,
					AllowedIPs:          []string{remote.overlay, remote.lanPrefix},
					PersistentKeepalive: 25,
				},
			},
			api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
				Metadata: api.ObjectMeta{Name: "to-" + remote.name + "-lan"},
				Spec: api.IPv4RouteSpec{
					Destination: remote.lanPrefix,
					Device:      "wg-mesh",
					Gateway:     prefixAddrString(remote.overlay),
					Metric:      120,
				},
			},
		)
	}
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: self.router},
		Spec:     api.RouterSpec{Resources: resources},
	}
}

func assertRoutedMeshAddressPlanDistinct(t *testing.T, sites []routedMeshSite) {
	t.Helper()
	hosts := map[netip.Addr]string{}
	lanPrefixes := make([]namedPrefix, 0, len(sites))
	for _, site := range sites {
		lan := mustPrefix(t, site.name+" LAN", site.lanPrefix)
		lanPrefixes = append(lanPrefixes, namedPrefix{name: site.name + " LAN", prefix: lan})

		routerLAN := mustPrefix(t, site.name+" router LAN", site.routerLAN)
		if !lan.Contains(routerLAN.Addr()) {
			t.Fatalf("%s router LAN %s is outside %s", site.name, site.routerLAN, site.lanPrefix)
		}
		client := mustAddr(t, site.name+" client", site.client)
		if !lan.Contains(client) {
			t.Fatalf("%s client %s is outside %s", site.name, site.client, site.lanPrefix)
		}
		addUniqueHost(t, hosts, routerLAN.Addr(), site.name+" router LAN")
		addUniqueHost(t, hosts, client, site.name+" client")

		overlay := mustPrefix(t, site.name+" overlay", site.overlay)
		if overlay.Bits() != 32 {
			t.Fatalf("%s overlay = %s, want /32", site.name, site.overlay)
		}
		addUniqueHost(t, hosts, overlay.Addr(), site.name+" overlay")

		endpoint := endpointAddr(t, site)
		addUniqueHost(t, hosts, endpoint, site.name+" endpoint")
	}
	for i := range lanPrefixes {
		for j := i + 1; j < len(lanPrefixes); j++ {
			if lanPrefixes[i].prefix.Overlaps(lanPrefixes[j].prefix) {
				t.Fatalf("%s %s overlaps %s %s", lanPrefixes[i].name, lanPrefixes[i].prefix, lanPrefixes[j].name, lanPrefixes[j].prefix)
			}
		}
	}
	for _, site := range sites {
		for _, host := range []namedAddr{
			{name: site.name + " overlay", addr: mustPrefix(t, site.name+" overlay", site.overlay).Addr()},
			{name: site.name + " endpoint", addr: endpointAddr(t, site)},
		} {
			for _, lan := range lanPrefixes {
				if lan.prefix.Contains(host.addr) {
					t.Fatalf("%s %s must not be inside routed LAN %s %s", host.name, host.addr, lan.name, lan.prefix)
				}
			}
		}
	}
}

func assertNoMobilityResources(t *testing.T, router *api.Router) {
	t.Helper()
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "MobilityPool", "RemoteAddressClaim", "AddressMobilityDomain":
			t.Fatalf("%s fixture must be routed mesh only; unexpected %s", router.Metadata.Name, res.ID())
		}
	}
}

func assertRoutedMeshRoutes(t *testing.T, router *api.Router, self routedMeshSite, sites []routedMeshSite) {
	t.Helper()
	routes := map[string]api.IPv4RouteSpec{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv4Route" {
			continue
		}
		spec, err := res.IPv4RouteSpec()
		if err != nil {
			t.Fatalf("%s: %v", res.ID(), err)
		}
		routes[spec.Destination] = spec
	}
	if len(routes) != len(sites)-1 {
		t.Fatalf("%s routes = %d, want %d: %#v", router.Metadata.Name, len(routes), len(sites)-1, routes)
	}
	for _, remote := range sites {
		if remote.name == self.name {
			continue
		}
		route, ok := routes[remote.lanPrefix]
		if !ok {
			t.Fatalf("%s missing route to %s", router.Metadata.Name, remote.lanPrefix)
		}
		if route.Device != "wg-mesh" || route.Gateway != prefixAddrString(remote.overlay) {
			t.Fatalf("%s route to %s = %+v, want device wg-mesh gateway %s", router.Metadata.Name, remote.lanPrefix, route, prefixAddrString(remote.overlay))
		}
	}
}

func assertRoutedMeshWireGuardPeers(t *testing.T, router *api.Router, self routedMeshSite, sites []routedMeshSite) {
	t.Helper()
	peers := map[string]api.WireGuardPeerSpec{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "WireGuardPeer" {
			continue
		}
		spec, err := res.WireGuardPeerSpec()
		if err != nil {
			t.Fatalf("%s: %v", res.ID(), err)
		}
		peers[res.Metadata.Name] = spec
	}
	if len(peers) != len(sites)-1 {
		t.Fatalf("%s peers = %d, want %d: %#v", router.Metadata.Name, len(peers), len(sites)-1, peers)
	}
	for _, remote := range sites {
		if remote.name == self.name {
			continue
		}
		peer, ok := peers[remote.name]
		if !ok {
			t.Fatalf("%s missing WireGuardPeer/%s", router.Metadata.Name, remote.name)
		}
		if peer.Interface != "wg-mesh" {
			t.Fatalf("%s WireGuardPeer/%s interface = %q, want wg-mesh", router.Metadata.Name, remote.name, peer.Interface)
		}
		for _, want := range []string{remote.overlay, remote.lanPrefix} {
			if !containsString(peer.AllowedIPs, want) {
				t.Fatalf("%s WireGuardPeer/%s allowedIPs = %v, missing %s", router.Metadata.Name, remote.name, peer.AllowedIPs, want)
			}
		}
	}
}

type namedPrefix struct {
	name   string
	prefix netip.Prefix
}

type namedAddr struct {
	name string
	addr netip.Addr
}

func mustPrefix(t *testing.T, label, value string) netip.Prefix {
	t.Helper()
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		t.Fatalf("%s prefix %q: %v", label, value, err)
	}
	return prefix.Masked()
}

func mustAddr(t *testing.T, label, value string) netip.Addr {
	t.Helper()
	addr, err := netip.ParseAddr(value)
	if err != nil {
		t.Fatalf("%s address %q: %v", label, value, err)
	}
	return addr
}

func endpointAddr(t *testing.T, site routedMeshSite) netip.Addr {
	t.Helper()
	host, _, err := net.SplitHostPort(site.endpoint)
	if err != nil {
		t.Fatalf("%s endpoint %q: %v", site.name, site.endpoint, err)
	}
	return mustAddr(t, site.name+" endpoint", host)
}

func addUniqueHost(t *testing.T, hosts map[netip.Addr]string, addr netip.Addr, label string) {
	t.Helper()
	if existing, ok := hosts[addr]; ok {
		t.Fatalf("duplicate routed-mesh address %s: %s and %s", addr, existing, label)
	}
	hosts[addr] = label
}

func prefixAddrString(value string) string {
	prefix := netip.MustParsePrefix(value)
	return prefix.Addr().String()
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}
