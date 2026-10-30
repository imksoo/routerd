// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestInterfaceHTTPSeparatesDeclaredAddressFromNativeObservation(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "lan-address", map[string]any{"phase": "Error", "address": "192.0.2.88/24"}); err != nil {
		t.Fatal(err)
	}
	declaration := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan-audit"}, Spec: api.InterfaceSpec{IfName: "routerd-audit-not-present"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-address"}, Spec: api.IPv4StaticAddressSpec{Interface: "Interface/lan-audit", Address: "192.0.2.99/24"}},
	}}}
	server := httptest.NewServer(New(Options{Router: declaration, Store: store}))
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/v1/summary?events=-1&connections=-1&firewallLogs=-1&dnsQueries=-1&trafficFlows=-1&vpn=0&dhcpLeases=0")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var snapshot Snapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(snapshot.Interfaces) != 1 {
		t.Fatalf("interface response: status=%d interfaces=%+v", response.StatusCode, snapshot.Interfaces)
	}
	iface := snapshot.Interfaces[0]
	if len(iface.Addresses) != 0 || len(iface.ConfiguredAddresses) != 1 || iface.ConfiguredAddresses[0] != "192.0.2.99/24" {
		t.Fatalf("intent or failed controller target presented as a native address: %+v", iface)
	}
	for _, resource := range snapshot.Resources {
		if resource.Kind == "IPv4StaticAddress" && resource.Status["address"] == "192.0.2.88/24" {
			return // Controller evidence remains available in the resource view.
		}
	}
	t.Fatal("separating native addresses removed the controller's resource evidence")
}
