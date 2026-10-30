// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package chain

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFreeBSDNativeDnsmasqRejectsIncompleteDescriptorView(t *testing.T) {
	probe, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if _, err := os.Stat("/dev/fd/" + strconv.Itoa(int(probe.Fd()))); err == nil {
		t.Skip("requires the incomplete bare-devfs descriptor view")
	}
	directory := t.TempDir()
	spec := dnsmasqProcessSpec{Command: "/nonexistent/routerd15-dnsmasq-must-not-start", Config: filepath.Join(directory, "dnsmasq.conf"), PIDFile: filepath.Join(directory, "dnsmasq.pid")}
	if err := startOwnedDnsmasq(t.Context(), spec); err == nil || !strings.Contains(err.Error(), "mount fdescfs on /dev/fd") {
		t.Fatalf("incomplete descriptor view was not rejected before launch: %v", err)
	}
	if _, err := os.Stat(dnsmasqOwnerPath(spec.PIDFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed environment check created an ownership receipt: %v", err)
	}
}

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
