// SPDX-License-Identifier: BSD-3-Clause

package chain

import "testing"

func TestLinuxIPv4AddressPresentRequiresExactPrefix(t *testing.T) {
	output := []byte("7: ens19    inet 10.77.60.34/24 brd 10.77.60.255 scope global ens19\\n")
	if linuxIPv4AddressPresent(output, "10.77.60.34/32") {
		t.Fatal("a wider external address must not satisfy a managed /32")
	}
	if !linuxIPv4AddressPresent(output, "10.77.60.34/24") {
		t.Fatal("exactly matching address was not detected")
	}
}
