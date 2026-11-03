// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"fmt"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	ra "github.com/imksoo/routerd/pkg/routeradvertisement"
)

func TestRAWithdrawalAfterColdBootRetiresOldKernelReceiptBeforeSocketOpen(t *testing.T) {
	service, store, _, endpoint, socket, _ := raServiceFixture(t)
	if err := service.transmit(t.Context(), "ra-test-lan", endpoint, false); err != nil {
		t.Fatal(err)
	}
	// This node returns as BACKUP with its VMAC down and no link-local address.
	// A receipt from the previous kernel cannot authorize a withdrawal on the
	// shared identity now held by the active peer, nor block empty config apply.
	service.scope = func(context.Context) (string, error) { return "new-boot-kernel", nil }
	opens := 0
	service.open = func(string) (ra.Endpoint, error) {
		opens++
		return ra.Endpoint{}, fmt.Errorf("RA source is not link-local")
	}
	empty := &api.Router{}
	if err := service.Reconcile(t.Context(), empty, empty, store); err != nil {
		t.Fatalf("old-boot withdrawal required an unavailable socket: %v", err)
	}
	if opens != 0 || len(socket.packets) != 1 {
		t.Fatalf("old kernel receipt touched current shared identity: opens=%d packets=%d", opens, len(socket.packets))
	}
	journal, err := readRAJournal(store)
	if err != nil || len(journal) != 0 {
		t.Fatalf("old kernel receipt not retired: %v %v", journal, err)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", "lan-ra")
	if err != nil || len(status) != 0 {
		t.Fatalf("removed RA status retained: %v %v", status, err)
	}
}
