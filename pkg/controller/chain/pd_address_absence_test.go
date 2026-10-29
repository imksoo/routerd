// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/platform"
)

func TestLANWithdrawalPresenceIncludesFiniteDynamicAndTentativeAddresses(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '7: lan0 inet6 2001:db8::1/64 scope global dynamic tentative valid_lft 100sec preferred_lft 80sec'\n"
	if err := os.WriteFile(filepath.Join(bin, "ip"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	controller := LANAddressController{OperatingSystem: platform.OSLinux}
	absent, err := controller.delegatedAddressAbsent(context.Background(), "lan0", "2001:db8::1/64")
	if err != nil || absent {
		t.Fatalf("existing finite/tentative native address lost withdrawal receipt: absent=%t err=%v", absent, err)
	}
	absent, err = controller.delegatedAddressAbsent(context.Background(), "lan0", "2001:db8::1/128")
	if err != nil || !absent {
		t.Fatalf("presence must preserve exact native prefix length: absent=%t err=%v", absent, err)
	}
}
