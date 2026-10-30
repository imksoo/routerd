// SPDX-License-Identifier: BSD-3-Clause

package daemonapi

import (
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestNamedDHCPLeaseSurvivesActualEventBoundary(t *testing.T) {
	now := time.Now().UTC()
	expiry := now.Add(time.Hour)
	event := NewEvent(DaemonRef{Name: "relay", Kind: "routerd-dhcp-event-relay"}, EventDHCPLeaseAdded, SeverityInfo)
	event.Resource = &ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server", Name: "lan"}
	event.Attributes = map[string]string{"mac": "00:04:aa:bb", "ip": "2001:db8::55", "hostname": "client", "observedAt": now.Format(time.RFC3339Nano), "expiresAt": expiry.Format(time.RFC3339Nano)}
	lease, err := DHCPLeaseFromEvent(event)
	if err != nil || lease.Source != "DHCPv6Server/lan" || !lease.ExpiresAt.Equal(expiry) || lease.Infinite {
		t.Fatalf("event lease lost identity/lifetime: %+v %v", lease, err)
	}
	if err := lease.Validate(time.Now()); err != nil {
		t.Fatal(err)
	}
	event.Resource = nil
	if _, err := DHCPLeaseFromEvent(event); err == nil {
		t.Fatal("unnamed event became named lease")
	}
}
