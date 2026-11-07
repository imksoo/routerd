// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestBGPDynamicPeerWhenDependencyWakesActualBGPController(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPDynamicPeer"},
		Metadata: api.ObjectMeta{Name: "clients"},
		Spec: api.BGPDynamicPeerSpec{When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
			"VirtualAddress/gateway.role": {Equals: "master"},
		}}},
	}}}}
	subs := bgpStatusSubscriptions(router)
	if !subscriptionSetAccepts(subs, statusChangedEvent("VirtualAddress", "gateway")) {
		t.Fatal("BGP did not subscribe to its dynamic admission dependency")
	}
	if subscriptionSetAccepts(subs, statusChangedEvent("VirtualAddress", "unrelated")) {
		t.Fatal("BGP subscribed to an unrelated dynamic admission dependency")
	}
	if subscriptionSetAccepts(subs, statusChangedEvent("BGPDynamicPeer", "clients")) {
		t.Fatal("BGP subscribed to its own dynamic admission status publication")
	}
}
