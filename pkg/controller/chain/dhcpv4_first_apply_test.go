// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
)

func TestFirstDHCPv4ApplyMayPrecedeSupervisedSocket(t *testing.T) {
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.DHCPv4ClientSpec{Interface: "eth0"}},
	}}}
	store := mapStore{}
	runner := &Runner{Router: router, Store: store, Bus: bus.New(), Opts: Options{
		EnabledControllers: []string{"dhcpv4-lease"},
		DaemonSockets:      map[string]string{"wan": filepath.Join(t.TempDir(), "starting.sock")},
	}}
	if err := runner.ReconcileOnce(t.Context()); err != nil {
		t.Fatalf("live first apply failed before supervisor could start: %v", err)
	}
	s := store[api.NetAPIVersion+"/DHCPv4Client/wan"]
	if s["phase"] != "Pending" || s["reason"] != "DaemonUnavailable" || s["dryRun"] != false {
		t.Fatalf("first apply fabricated readiness: %+v", s)
	}
}
