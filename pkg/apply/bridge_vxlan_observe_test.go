// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"errors"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestMissingBridgeAndUnobservedVXLANTunnelCannotBeHealthy(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: true}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "legacy-l2"}, Spec: api.BridgeSpec{IfName: "br-l2", Members: []string{"lan"}, MTU: 1370}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-l2"}, Spec: api.WireGuardInterfaceSpec{PrivateKeyFile: "/run/test.key"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "overlay"}, Spec: api.VXLANTunnelSpec{IfName: "vx-l2", VNI: 200001, LocalAddress: "10.254.200.1", Peers: []string{"10.254.200.2"}, UnderlayInterface: "wg-l2", Bridge: "legacy-l2"}},
		}},
	}
	engine := New(t.Context())
	engine.Command = func(name string, args ...string) ([]byte, error) {
		return nil, errors.New("not found")
	}
	result, err := engine.Observe(router)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if result.Phase != "Blocked" {
		t.Fatalf("result phase = %s, want Blocked: %#v", result.Phase, result)
	}
	for _, id := range []string{api.NetAPIVersion + "/Bridge/legacy-l2", api.NetAPIVersion + "/VXLANTunnel/overlay"} {
		found := false
		for _, resource := range result.Resources {
			if resource.ID == id {
				found = true
				phase, present := "Drifted", "false"
				if resource.ID == api.NetAPIVersion+"/VXLANTunnel/overlay" {
					phase, present = "Blocked", "unknown"
				}
				if resource.Phase != phase || resource.Observed["present"] != present {
					t.Fatalf("%s = %#v", id, resource)
				}
			}
		}
		if !found {
			t.Fatalf("missing resource result %s", id)
		}
	}
}
