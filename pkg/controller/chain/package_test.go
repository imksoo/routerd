// SPDX-License-Identifier: BSD-3-Clause

package chain

import "testing"

func TestParseOSReleaseID(t *testing.T) {
	if got := parseOSReleaseID("NAME=\"Ubuntu\"\nID=ubuntu\n"); got != "ubuntu" {
		t.Fatalf("ID = %q", got)
	}
}
