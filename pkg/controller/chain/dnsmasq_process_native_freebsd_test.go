// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package chain

import "testing"

func TestFreeBSDNativeDnsmasqInventorySkipsKernelProcess(t *testing.T) {
	processes, err := listNativeDnsmasqProcesses(t.Context(), dnsmasqProcessSpec{
		Command: "/nonexistent/routerd15-dnsmasq-inventory-test",
	})
	if err != nil {
		t.Fatalf("native dnsmasq inventory: %v", err)
	}
	if len(processes) != 0 {
		t.Fatal("native inventory matched an absent executable")
	}
}
