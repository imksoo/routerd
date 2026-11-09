// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"testing"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestEventFromArgsNormalizesOnlyDnsmasqLeaseActions(t *testing.T) {
	for raw, want := range map[string]string{
		"add": daemonapi.DHCPLeaseActionAdded,
		"old": daemonapi.DHCPLeaseActionRenewed,
		"del": daemonapi.DHCPLeaseActionRemoved,
	} {
		event, err := eventFromArgs([]string{raw, "02:00:00:00:00:01", "192.0.2.10", "host"}, []string{"DNSMASQ_INTERFACE=lan0", "SECRET=value"})
		if err != nil {
			t.Fatalf("eventFromArgs(%q): %v", raw, err)
		}
		if event.Action != want || event.Interface != "lan0" {
			t.Fatalf("eventFromArgs(%q) = %#v", raw, event)
		}
	}
	if _, err := eventFromArgs([]string{"renew", "02:00:00:00:00:01", "192.0.2.10"}, nil); err == nil {
		t.Fatal("eventFromArgs accepted an unknown dnsmasq lease action")
	}
}
