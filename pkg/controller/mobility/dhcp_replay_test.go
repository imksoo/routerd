// SPDX-License-Identifier: BSD-3-Clause
package mobility

import (
	"github.com/imksoo/routerd/pkg/daemonapi"
	"testing"
	"time"
)

func TestHistoricalDHCPDoesNotRenewOrWithdrawCurrentOwnership(t *testing.T) {
	at := time.Date(2026, 10, 21, 1, 2, 3, 123456, time.UTC)
	for _, topic := range []string{daemonapi.EventDHCPLeaseAdded, daemonapi.EventDHCPLeaseRenewed, daemonapi.EventDHCPLeaseRemoved} {
		e := daemonapi.DaemonEvent{Type: topic, Time: at, Attributes: map[string]string{"ip": "192.0.2.42", "interface": "test0"}}
		o, ok := onPremObservationFromDaemonEvent(e)
		if !ok || !o.ObservedAt.Equal(at) {
			t.Fatal(o, ok)
		}
		e.Attributes["replayed"] = "true"
		if _, ok := onPremObservationFromDaemonEvent(e); ok {
			t.Fatalf("historical %s reached ownership mutation, including subsecond ordering", topic)
		}
	}
}
