// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestSAMRouteControllersSubscribeToVirtualAddressStatus(t *testing.T) {
	event := daemonapi.DaemonEvent{
		Type: "routerd.resource.status.changed",
		Resource: &daemonapi.ResourceRef{
			APIVersion: api.NetAPIVersion,
			Kind:       "VirtualAddress",
			Name:       "onprem-vip",
		},
		Attributes: map[string]string{"changedFields": "role,lastRoleTransitionAt"},
	}
	tests := []struct {
		name string
		subs []bus.Subscription
	}{
		{name: "ipv4-route", subs: ipv4RouteStatusSubscriptions()},
		{name: "hybrid-route", subs: hybridRouteStatusSubscriptions()},
		{name: "sam", subs: samStatusSubscriptions()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !subscriptionSetAccepts(tt.subs, event) {
				t.Fatalf("%s subscriptions did not accept VirtualAddress status change", tt.name)
			}
		})
	}
}

func subscriptionSetAccepts(subs []bus.Subscription, event daemonapi.DaemonEvent) bool {
	for _, sub := range subs {
		if sub.Filter == nil || sub.Filter(event) {
			return true
		}
	}
	return false
}
