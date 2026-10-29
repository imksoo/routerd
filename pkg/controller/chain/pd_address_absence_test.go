// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"github.com/imksoo/routerd/pkg/platform"
	"testing"
)

func TestLANWithdrawalPresenceIncludesFiniteDynamicAndTentativeAddresses(t *testing.T) {
	out := []byte("7: lan0 inet6 2001:db8::1/64 scope global dynamic tentative valid_lft 100sec preferred_lft 80sec\n")
	for _, tc := range []struct {
		address string
		present bool
	}{{"2001:db8::1/64", true}, {"2001:db8::1/128", false}} {
		observation, err := parseDelegatedAddressObservation(out, tc.address, platform.OSLinux, false)
		if err != nil || observation.present != tc.present {
			t.Fatalf("observation=%+v err=%v", observation, err)
		}
		if observation.usable {
			t.Fatal("tentative address falsely ready")
		}
	}
}
