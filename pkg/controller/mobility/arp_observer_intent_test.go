// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"net/netip"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

type arpObserverStatusReader map[string]map[string]any

func (r arpObserverStatusReader) ObjectStatus(_ string, kind, name string) map[string]any {
	return r[kind+"/"+name]
}

func TestResolveARPObserverSourceAddress(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "in mobility prefix", value: "192.168.123.134/24", want: "192.168.123.134"},
		{name: "outside mobility prefix", value: "198.51.100.24/24", want: "198.51.100.24"},
		{name: "malformed", value: "not-an-ip", want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader := arpObserverStatusReader{
				"DHCPv4Client/capture-source": {"currentAddress": tt.value},
			}
			got := resolveARPObserverSourceAddress(reader, api.StatusValueSourceSpec{
				Resource: "DHCPv4Client/capture-source",
				Field:    "currentAddress",
			})
			if got != tt.want {
				t.Fatalf("resolveARPObserverSourceAddress(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestARPObserverIntentsUsePoolScopedDaemonIdentity(t *testing.T) {
	sources := []api.MobilityOwnershipDiscoverySource{
		{Type: OnPremSourceARPObserver, Resource: "shared-observer", Interface: "br0"},
		{Type: OnPremSourceARPObserver, Resource: "shared-observer", Interface: "br1"},
		{Type: OnPremSourceOnDemandARP, Resource: "shared-observer", Interface: "br0"},
	}
	pool := func(name string) NormalizedMobilityPool {
		prefix := netip.MustParsePrefix("192.168.123.0/24")
		return NormalizedMobilityPool{
			Name:     name,
			SelfNode: "pve-rt08",
			Spec:     api.MobilityPoolSpec{Prefix: "192.168.123.0/24"},
			Prefix:   prefix,
			Self: memberPlanInfo{
				Role:                 "onprem",
				Capture:              api.MobilityMemberCapture{Type: "proxy-arp", Interface: "br0", SourceAddress: "192.168.123.1"},
				CaptureSourceAddress: "192.168.123.1",
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode:    "onprem-l2",
					Sources: sources,
				},
			},
		}
	}

	discovery := DiscoveryController{}
	first := discovery.arpObserverIntents(pool("cloudedge-a"))
	if len(first) != 3 {
		t.Fatalf("same explicit source resource produced %d intents, want three: %#v", len(first), first)
	}
	seen := map[string]bool{}
	byType := map[string]int{}
	for _, intent := range first {
		if seen[intent.ResourceName] {
			t.Fatalf("same explicit source resource reused daemon identity %q", intent.ResourceName)
		}
		seen[intent.ResourceName] = true
		byType[intent.SourceType]++
	}
	if byType[OnPremSourceARPObserver] != 2 || byType[OnPremSourceOnDemandARP] != 1 {
		t.Fatalf("same explicit source resource dropped a source type: %#v", first)
	}
	for _, intent := range first {
		if intent.ResourceName == "shared-observer" {
			t.Fatalf("intent reused source.resource as daemon identity: %#v", intent)
		}
		if intent.Prefix != "192.168.123.0/24" {
			t.Fatalf("intent prefix = %q, want canonical normalized prefix", intent.Prefix)
		}
	}

	repeated := discovery.arpObserverIntents(pool("cloudedge-a"))
	if len(repeated) != len(first) {
		t.Fatalf("repeated intents = %#v, want %#v", repeated, first)
	}
	for i := range first {
		if repeated[i].ResourceName != first[i].ResourceName {
			t.Fatalf("daemon identity changed across identical reconciliation: %q then %q", first[i].ResourceName, repeated[i].ResourceName)
		}
	}

	otherPool := discovery.arpObserverIntents(pool("cloudedge-b"))
	seen = map[string]bool{}
	for _, intent := range first {
		seen[intent.ResourceName] = true
	}
	for _, intent := range otherPool {
		if seen[intent.ResourceName] {
			t.Fatalf("pools sharing source.resource collided on daemon identity %q", intent.ResourceName)
		}
	}
}

func TestOnPremDiscoveryConfigKeyUsesNormalizedPrefix(t *testing.T) {
	pool := NormalizedMobilityPool{
		Name:     "cloudedge",
		SelfNode: "pve-rt08",
		Spec:     api.MobilityPoolSpec{Prefix: "192.168.123.0/24"},
		Prefix:   netip.MustParsePrefix("192.168.123.0/24"),
	}
	nonCanonical := pool
	nonCanonical.Spec.Prefix = "192.168.123.77/24"
	if got, want := onPremDiscoveryConfigKey(nonCanonical), onPremDiscoveryConfigKey(pool); got != want {
		t.Fatalf("config key changed for equivalent normalized prefix: got %q, want %q", got, want)
	}
}
