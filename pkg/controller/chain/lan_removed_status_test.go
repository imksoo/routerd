// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestLANExpiredThenRemovedDeletesStatus(t *testing.T) {
	for _, osName := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
		t.Run(string(osName), func(t *testing.T) {
			store, _, c, _ := sqliteNativeLANFixture(t)
			c.OperatingSystem = osName
			addresses := installLANIOFixture(&c)
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(addresses) != 1 {
				t.Fatalf("address was not acquired: %v", addresses)
			}
			now := c.Now().Add(121 * time.Second)
			c.Now = func() time.Time { return now }
			// The kernel has removed the address at its finite valid lifetime.
			clear(addresses)
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
			if err != nil || len(addresses) != 0 || status["phase"] != "Pending" || status["nativeReceipt"] != nil {
				t.Fatalf("expiry did not finish withdrawal: status=%v addresses=%v err=%v", status, addresses, err)
			}
			c.Router = &api.Router{}
			c.DeclaredRouter = c.Router
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			status, err = store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
			if err != nil || status != nil {
				t.Fatalf("removed resource retains stale status: %v, %v", status, err)
			}
		})
	}
}
