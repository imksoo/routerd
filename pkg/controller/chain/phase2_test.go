// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestDHCPv6InformationWaitsForClientSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wan-pd.sock")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Information"}, Metadata: api.ObjectMeta{Name: "wan-info"}, Spec: api.DHCPv6InformationSpec{Interface: "wan"}},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/DHCPv6PrefixDelegation/wan-pd": {
			"phase": daemonapi.ResourcePhaseBound,
		},
	}
	controller := DHCPv6InformationController{
		Router:        router,
		Store:         store,
		DaemonSockets: map[string]string{"wan-pd": socket},
	}

	if err := controller.reconcile(context.Background(), "wan-pd", true); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv6Information", "wan-info")
	if status["phase"] != "Pending" {
		t.Fatalf("phase = %v, want Pending; status=%v", status["phase"], status)
	}
	if status["reason"] != "DHCPv6ClientSocketPending" {
		t.Fatalf("reason = %v, want DHCPv6ClientSocketPending; status=%v", status["reason"], status)
	}
	if status["source"] != "wan-pd" {
		t.Fatalf("source = %v, want wan-pd; status=%v", status["source"], status)
	}
	if status["socket"] != socket {
		t.Fatalf("socket = %v, want %s; status=%v", status["socket"], socket, status)
	}
}
