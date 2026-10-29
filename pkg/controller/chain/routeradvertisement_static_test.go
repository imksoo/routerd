// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

func TestWriteDnsmasqConfigAdvertisesStaticRAPrefix(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "lan", Prefix: "2001:db8:1::123/64"}},
	}}}
	dir := t.TempDir()
	path := filepath.Join(dir, "dnsmasq.conf")
	if _, _, err := writeDnsmasqConfig(router, mapStore{}, path, filepath.Join(dir, "dnsmasq.pid"), 0, nil); err != nil {
		t.Fatal(err)
	}
	live, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	offline, _, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range [][]byte{live, offline} {
		if !strings.Contains(string(output), "dhcp-range=set:lan-ra,2001:db8:1::,ra-only,64,12h\n") || strings.Contains(string(output), "constructor:") {
			t.Fatalf("RA config recorded a prefix without selecting it:\n%s", output)
		}
	}
}
