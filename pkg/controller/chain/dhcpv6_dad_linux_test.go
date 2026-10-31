// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDHCPv6NativeLinuxDeletedDADFailureReportsDecline(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires explicit isolated netns execution")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	host := os.Getenv("ROUTERD_HOST_NETNS")
	if host == "" || current == host {
		t.Fatal("network namespace isolation is unverified")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	run := func(name string, args ...string) string {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return string(out)
	}
	peer := exec.CommandContext(ctx, "unshare", "-n", "sleep", "30")
	if err := peer.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Process.Kill(); _ = peer.Wait() })
	peerID := fmt.Sprint(peer.Process.Pid)
	ready := false
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		ns, e := os.Readlink("/proc/" + peerID + "/ns/net")
		if e == nil && ns != current {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("peer namespace was not ready")
	}
	run("ip", "link", "add", "wan0", "type", "veth", "peer", "name", "peer0", "netns", peerID)
	t.Cleanup(func() { _ = exec.Command("ip", "link", "del", "wan0").Run() })
	run("ip", "link", "set", "wan0", "up")
	run("nsenter", "-t", peerID, "-n", "ip", "link", "set", "peer0", "up")
	run("nsenter", "-t", peerID, "-n", "ip", "-6", "addr", "add", "2001:db8::123/128", "dev", "peer0", "nodad")
	f := newDHCPv6NativeFixture(t)
	f.c.Now = time.Now
	f.c.ObserveAddress = nil
	f.c.Command = nil
	// IPv6 finite addresses are removed immediately on DAD failure on Linux.
	// A snapshot taken afterwards has no dadfailed entry, only absence.
	for deadline := time.Now().Add(6 * time.Second); time.Now().Before(deadline) && len(f.declines) == 0; {
		if err := f.c.Reconcile(ctx); err != nil {
			t.Logf("pending native observation: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(f.declines) != 1 {
		t.Fatalf("kernel DAD failure did not produce exactly one source-bound Decline: %+v", f.declines)
	}
	if got := run("ip", "-6", "addr", "show", "dev", "wan0"); strings.Contains(got, "2001:db8::123") {
		t.Fatalf("failed address remained: %s", got)
	}
	record, err := f.store.ReadDHCPv6AddressRecord("address")
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Phase == "Applied" {
		t.Fatal("DAD-failed address reported Applied")
	}
}
