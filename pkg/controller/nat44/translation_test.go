// SPDX-License-Identifier: BSD-3-Clause
package nat44

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestControllerAppliesSourceTranslation(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "wan0", Managed: false, Owner: "external"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "source"}, Spec: api.NAT44RuleSpec{OutboundInterface: "wan", SourceCIDRs: []string{"192.0.2.0/24"}, Translation: api.IPv4NATTranslationSpec{Type: "address", Address: "198.51.100.10", PortMapping: api.IPv4NATPortMappingSpec{Type: "range", Start: 40000, End: 40010}}}},
	}}}
	store := &testStore{}
	path := filepath.Join(t.TempDir(), "nat.nft")
	c := Controller{OS: platform.OSLinux, Router: router, Store: store, Bus: bus.New(), DryRun: true, NftablesPath: path, NftCommand: "true"}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `oifname "wan0" ip saddr 192.0.2.0/24 meta l4proto { tcp, udp } snat to 198.51.100.10:40000-40010`) {
		t.Errorf("source translation absent: %s", data)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "NAT44Rule", "source"); got["phase"] != "Active" {
		t.Errorf("source rule not active: %#v", got)
	}
}
