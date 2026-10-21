// SPDX-License-Identifier: BSD-3-Clause
package platform

import "testing"

func TestManagedNftablesFile(t *testing.T) {
	for _, tc := range []struct {
		name     string
		defaults Defaults
		want     string
	}{
		{"linux", Defaults{OS: OSLinux, RuntimeDir: "/run/routerd", NftablesFile: "/usr/local/etc/routerd/nftables.nft"}, "/run/routerd/nat44.nft"},
		{"custom-runtime", Defaults{OS: OSLinux, RuntimeDir: "/tmp/runtime/"}, "/tmp/runtime/nat44.nft"},
		{"freebsd", Defaults{OS: OSFreeBSD}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.defaults.ManagedNftablesFile(); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
