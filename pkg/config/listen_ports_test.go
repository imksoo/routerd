// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestValidateRejectsIngressLocalRedirectListenPortCollision(t *testing.T) {
	router := routerWithResources(
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api"}, Spec: api.IngressServiceSpec{
			Listen:   api.IngressListenSpec{Interface: "lan", Protocol: "tcp", Port: 8443},
			Backends: []api.IngressBackendSpec{{Address: "10.0.0.11", Port: 6443}},
		}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "LocalServiceRedirect"}, Metadata: api.ObjectMeta{Name: "local"}, Spec: api.LocalServiceRedirectSpec{
			Interface: "lan",
			Rules: []api.LocalServiceRedirectRuleSpec{{
				Protocols:         []string{"tcp"},
				DestinationSetRef: "public-dns",
				DestinationPort:   443,
				RedirectPort:      8443,
			}},
		}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "public-dns"}, Spec: api.IPAddressSetSpec{Addresses: []string{"1.1.1.1"}}},
	)
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "tcp/8443") {
		t.Fatalf("expected listen-port collision, got %v", err)
	}
}

func TestValidateRejectsIngressInternalDaemonListenPortCollision(t *testing.T) {
	router := routerWithResources(
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "dhcp"}, Spec: api.DHCPv4ServerSpec{
			Interface: "lan",
			AddressPool: api.DHCPAddressPoolSpec{
				Start: "192.0.2.10",
				End:   "192.0.2.20",
			},
		}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "bootp"}, Spec: api.IngressServiceSpec{
			Listen:   api.IngressListenSpec{Interface: "lan", Protocol: "udp", Port: 67},
			Backends: []api.IngressBackendSpec{{Address: "10.0.0.11", Port: 67}},
		}},
	)
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "udp/67") {
		t.Fatalf("expected DHCP listen-port collision, got %v", err)
	}
}

func TestValidateAllowsMultipleLocalRedirectRulesToSamePort(t *testing.T) {
	router := routerWithResources(
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "LocalServiceRedirect"}, Metadata: api.ObjectMeta{Name: "local-dns"}, Spec: api.LocalServiceRedirectSpec{
			Interface: "lan",
			Rules: []api.LocalServiceRedirectRuleSpec{
				{Protocols: []string{"udp", "tcp"}, DestinationSetRef: "public-dns-google", DestinationPort: 53, RedirectPort: 53},
				{Protocols: []string{"udp", "tcp"}, DestinationSetRef: "public-dns-cloudflare", DestinationPort: 53, RedirectPort: 53},
			},
		}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "public-dns-google"}, Spec: api.IPAddressSetSpec{Addresses: []string{"8.8.8.8"}}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "public-dns-cloudflare"}, Spec: api.IPAddressSetSpec{Addresses: []string{"1.1.1.1"}}},
	)
	if err := Validate(router); err != nil {
		t.Fatalf("local redirect rules sharing a redirect port should validate: %v", err)
	}
}

func routerWithResources(resources ...api.Resource) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec:     api.RouterSpec{Resources: resources},
	}
}
