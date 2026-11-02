// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestDHCPv4RelayTracksObservedAddressWithoutStaticDeclaration(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "relay-test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0", Owner: "external"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Relay"}, Metadata: api.ObjectMeta{Name: "relay"}, Spec: api.DHCPv4RelaySpec{Interfaces: []string{"lan"}, Upstream: "198.51.100.2"}},
		}},
	}
	// Runtime interface observation supplies giaddr; a static declaration is not
	// a prerequisite for accepting this configuration.
	if err := config.ValidateForOS(router, platform.OSLinux); err != nil {
		t.Fatalf("validate relay using an externally addressed interface: %v", err)
	}
	store := mapStore{}
	for _, step := range []struct {
		name, address string
		unavailable   bool
	}{
		{name: "initial", address: "192.0.2.1/24"},
		{name: "unchanged", address: "192.0.2.1/24"},
		{name: "renumbered", address: "192.0.2.2/24"},
		{name: "address lost", unavailable: true},
		{name: "recovered", address: "192.0.2.3/24"},
	} {
		t.Run(step.name, func(t *testing.T) {
			store[api.NetAPIVersion+"/Interface/lan"] = map[string]any{"primaryIPv4": step.address}
			lines, err := dnsmasqLANServiceLines(router, store)
			if step.unavailable {
				if err == nil || !strings.Contains(err.Error(), "no observed primary IPv4 address") {
					t.Fatalf("missing address: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var relayLines []string
			for _, line := range lines {
				if strings.HasPrefix(line, "dhcp-relay=") {
					relayLines = append(relayLines, line)
				}
			}
			want := "dhcp-relay=" + strings.Split(step.address, "/")[0] + ",198.51.100.2"
			if len(relayLines) != 1 || relayLines[0] != want {
				t.Fatalf("relay lines=%q want=%q", relayLines, want)
			}
		})
	}
	router.Spec.Resources = router.Spec.Resources[:1]
	lines, err := dnsmasqLANServiceLines(router, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "dhcp-relay=") {
			t.Fatal("withdrawn relay remains in config")
		}
	}
}
