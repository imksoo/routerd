// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import "testing"

func TestNormalizeFreeBSDRouteFamily(t *testing.T) {
	if _, err := normalizeFreeBSDRouteFamily("ip4"); err == nil {
		t.Fatal("expected unsupported family error")
	}
}
