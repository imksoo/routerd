// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/platform"
)

// These are ifconfig -L observations, not native FreeBSD qualification.
// FreeBSD 14.3 af_inet6.c prints lifetimes and IN6_IFF_DUPLICATED on the
// address line; nd6_nbr.c keeps that flag after clearing TENTATIVE.
func TestDHCPv6NativeFreeBSDAddressObservation(t *testing.T) {
	for _, tc := range []struct {
		name, flags       string
		usable, duplicate bool
	}{
		{"tentative", "tentative", false, false},
		{"ready", "", true, false},
		{"duplicate", "duplicated", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := fmt.Sprintf("wan0: flags=1008843<UP,BROADCAST,RUNNING,SIMPLEX,MULTICAST>\n\tinet6 fe80::1%%wan0 prefixlen 64 scopeid 0x7\n\tinet6 2001:db8::123 prefixlen 128 %s pltime 29 vltime 59\n", tc.flags)
			got, err := parseDelegatedAddressObservation([]byte(output), "2001:db8::123/128", platform.OSFreeBSD, false)
			if err != nil || !got.present || got.usable != tc.usable || got.dadFailed != tc.duplicate || !got.lifetimeKnown || got.preferredSeconds != 29 || got.validSeconds != 59 {
				t.Fatalf("FreeBSD finite address observation: %+v %v", got, err)
			}
		})
	}
}

func TestDHCPv6NativeFreeBSDDuplicateWithdrawsBeforeDecline(t *testing.T) {
	f := newDHCPv6NativeFixture(t)
	f.c.OperatingSystem = platform.OSFreeBSD
	f.c.Command = func(_ context.Context, name string, args ...string) error {
		call := name + " " + strings.Join(args, " ")
		f.calls = append(f.calls, call)
		switch call {
		case "ifconfig wan0 inet6 2001:db8::123 prefixlen 128 pltime 30 vltime 60 alias":
			f.observation.present = true
			f.observation.sameHost = []string{"2001:db8::123/128"}
			f.observation.preferredSeconds, f.observation.validSeconds = 30, 60
		case "ifconfig wan0 inet6 2001:db8::123 -alias":
			if len(f.declines) != 0 {
				t.Fatal("Decline preceded native duplicate withdrawal")
			}
			f.observation.present, f.observation.usable = false, false
			f.observation.sameHost = nil
		default:
			t.Fatalf("unexpected FreeBSD native operation: %s", call)
		}
		return nil
	}
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	record, err := f.store.ReadDHCPv6AddressRecord("address")
	if err != nil || record.State.Phase == "Applied" {
		t.Fatalf("tentative acquisition must remain pending: %+v %v", record.State, err)
	}
	f.observation.dadFailed = true
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || len(f.declines) != 1 || f.declines[0].Addresses[0].Address != "2001:db8::123" || !f.declines[0].Addresses[0].AcquiredAt.Equal(f.snapshot.AddressUpdatedAt) {
		t.Fatalf("duplicate did not withdraw and report the exact acquisition: calls=%v declines=%+v", f.calls, f.declines)
	}
}
