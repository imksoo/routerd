// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"

	"github.com/imksoo/routerd/pkg/platform"
)

// requireLinuxRuntimeFixture marks tests whose subject is specifically the
// Linux command/artifact contract. Platform-neutral and FreeBSD tests in the
// same files continue to run on FreeBSD.
func requireLinuxRuntimeFixture(t *testing.T) {
	t.Helper()
	if platform.CurrentOS() != platform.OSLinux {
		t.Skip("Linux command/artifact fixture")
	}
}
