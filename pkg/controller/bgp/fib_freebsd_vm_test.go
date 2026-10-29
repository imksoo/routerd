// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package bgp

import (
	"context"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestFreeBSDFIBVMAcceptance is intentionally opt-in. The VM harness configures
// an isolated connected vtnet0 fixture before invoking this test binary. It
// exercises the production syncer rather than a shell-only route demonstration.
func TestFreeBSDFIBVMAcceptance(t *testing.T) {
	if os.Getenv("ROUTERD_FREEBSD_FIB_VM") != "1" {
		t.Skip("set ROUTERD_FREEBSD_FIB_VM=1 in the isolated FreeBSD VM fixture")
	}
	ctx := context.Background()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ownedPrefix := "198.18.77.0/24"
	foreignPrefix := "198.18.78.0/24"
	firstHop := "192.0.2.2"
	secondHop := "192.0.2.3"
	cleanupRoute(t, foreignPrefix, firstHop, false)
	cleanupRoute(t, ownedPrefix, firstHop, true)
	cleanupRoute(t, ownedPrefix, secondHop, true)
	t.Cleanup(func() {
		cleanupRoute(t, ownedPrefix, firstHop, true)
		cleanupRoute(t, ownedPrefix, secondHop, true)
		cleanupRoute(t, foreignPrefix, firstHop, false)
	})

	runRouteVM(t, "add", "-net", foreignPrefix, firstHop)
	s := defaultFIBSyncer()
	result, err := syncFIBFixture(ctx, s, store, []FIBRoute{{Prefix: ownedPrefix, NextHops: []string{firstHop}}})
	if err != nil || !result.Installed[ownedPrefix] {
		t.Fatalf("install = %#v, %v", result, err)
	}
	assertFreeBSDRouteVM(t, ownedPrefix, firstHop, true)
	assertFreeBSDRouteVM(t, foreignPrefix, firstHop, false)

	// A new syncer recovers the exact native receipts from the same SQLite journal.
	restarted := defaultFIBSyncer()
	result, err = syncFIBFixture(ctx, restarted, store, []FIBRoute{{Prefix: ownedPrefix, NextHops: []string{secondHop}}})
	if err != nil || !result.Installed[ownedPrefix] {
		t.Fatalf("restart replacement = %#v, %v", result, err)
	}
	assertFreeBSDRouteVM(t, ownedPrefix, secondHop, true)
	assertFreeBSDRouteAbsentVM(t, ownedPrefix, firstHop, true)

	result, err = syncFIBFixture(ctx, restarted, store, []FIBRoute{{Prefix: ownedPrefix, NextHops: []string{firstHop, secondHop}}})
	if err != nil || !result.Installed[ownedPrefix] {
		t.Fatalf("multipath replacement = %#v, %v", result, err)
	}
	assertFreeBSDRouteVM(t, ownedPrefix, firstHop, true)
	assertFreeBSDRouteVM(t, ownedPrefix, secondHop, true)

	result, err = syncFIBFixture(ctx, restarted, store, nil)
	if err != nil {
		t.Fatalf("withdraw = %#v, %v", result, err)
	}
	assertFreeBSDRouteAbsentVM(t, ownedPrefix, firstHop, true)
	assertFreeBSDRouteAbsentVM(t, ownedPrefix, secondHop, true)
	assertFreeBSDRouteVM(t, foreignPrefix, firstHop, false)
}

// TestFreeBSDIPv6FIBVMAcceptance is intentionally opt-in. The native VNET
// fixture supplies the connected IPv6 next hops on vtnet0. It exercises the
// same production syncer and RTF_PROTO1 ownership path as the IPv4 acceptance.
func TestFreeBSDIPv6FIBVMAcceptance(t *testing.T) {
	if os.Getenv("ROUTERD_FREEBSD_FIB_VM") != "1" {
		t.Skip("set ROUTERD_FREEBSD_FIB_VM=1 in the isolated FreeBSD VM fixture")
	}
	ctx := context.Background()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ownedPrefix := "2001:db8:77::/64"
	foreignPrefix := "2001:db8:78::/64"
	firstHop := "2001:db8:1::2"
	secondHop := "2001:db8:1::3"
	cleanupRoute6(t, foreignPrefix, firstHop, false)
	cleanupRoute6(t, ownedPrefix, firstHop, true)
	cleanupRoute6(t, ownedPrefix, secondHop, true)
	t.Cleanup(func() {
		cleanupRoute6(t, ownedPrefix, firstHop, true)
		cleanupRoute6(t, ownedPrefix, secondHop, true)
		cleanupRoute6(t, foreignPrefix, firstHop, false)
	})

	runRouteVM(t, "add", "-inet6", "-net", foreignPrefix, firstHop)
	s := defaultFIBSyncer()
	result, err := syncFIBFixture(ctx, s, store, []FIBRoute{{Prefix: ownedPrefix, NextHops: []string{firstHop}}})
	if err != nil || !result.Installed[ownedPrefix] {
		t.Fatalf("install = %#v, %v", result, err)
	}
	assertFreeBSDRouteVM6(t, ownedPrefix, firstHop, true)
	assertFreeBSDRouteVM6(t, foreignPrefix, firstHop, false)

	restarted := defaultFIBSyncer()
	result, err = syncFIBFixture(ctx, restarted, store, []FIBRoute{{Prefix: ownedPrefix, NextHops: []string{secondHop}}})
	if err != nil || !result.Installed[ownedPrefix] {
		t.Fatalf("restart replacement = %#v, %v", result, err)
	}
	assertFreeBSDRouteVM6(t, ownedPrefix, secondHop, true)
	assertFreeBSDRouteAbsentVM6(t, ownedPrefix, firstHop, true)

	result, err = syncFIBFixture(ctx, restarted, store, []FIBRoute{{Prefix: ownedPrefix, NextHops: []string{firstHop, secondHop}}})
	if err != nil || !result.Installed[ownedPrefix] {
		t.Fatalf("multipath replacement = %#v, %v", result, err)
	}
	assertFreeBSDRouteVM6(t, ownedPrefix, firstHop, true)
	assertFreeBSDRouteVM6(t, ownedPrefix, secondHop, true)

	if _, err := syncFIBFixture(ctx, restarted, store, nil); err != nil {
		t.Fatalf("withdraw = %v", err)
	}
	assertFreeBSDRouteAbsentVM6(t, ownedPrefix, firstHop, true)
	assertFreeBSDRouteAbsentVM6(t, ownedPrefix, secondHop, true)
	assertFreeBSDRouteVM6(t, foreignPrefix, firstHop, false)
}

func runRouteVM(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("/sbin/route", append([]string{"-n"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("route %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func cleanupRoute(t *testing.T, prefix, nextHop string, owned bool) {
	t.Helper()
	args := []string{"-n", "delete"}
	if owned {
		args = append(args, "-proto1")
	}
	args = append(args, "-net", prefix, nextHop)
	_, _ = exec.Command("/sbin/route", args...).CombinedOutput()
}

func cleanupRoute6(t *testing.T, prefix, nextHop string, owned bool) {
	t.Helper()
	args := []string{"-n", "delete", "-inet6"}
	if owned {
		args = append(args, "-proto1")
	}
	args = append(args, "-net", prefix, nextHop)
	_, _ = exec.Command("/sbin/route", args...).CombinedOutput()
}

func assertFreeBSDRouteVM(t *testing.T, prefix, nextHop string, owned bool) {
	t.Helper()
	out, err := exec.Command("/usr/bin/netstat", "-rn", "-f", "inet").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != prefix || fields[1] != nextHop {
			continue
		}
		if strings.Contains(fields[2], "1") == owned {
			return
		}
	}
	t.Fatalf("route %s via %s owned=%v not found in:\n%s", prefix, nextHop, owned, out)
}

func assertFreeBSDRouteAbsentVM(t *testing.T, prefix, nextHop string, owned bool) {
	t.Helper()
	out, err := exec.Command("/usr/bin/netstat", "-rn", "-f", "inet").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == prefix && fields[1] == nextHop && strings.Contains(fields[2], "1") == owned {
			t.Fatalf("route %s via %s owned=%v still present:\n%s", prefix, nextHop, owned, out)
		}
	}
}

func assertFreeBSDRouteVM6(t *testing.T, prefix, nextHop string, owned bool) {
	t.Helper()
	out, err := exec.Command("/usr/bin/netstat", "-rn", "-f", "inet6").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != prefix || fields[1] != nextHop {
			continue
		}
		if strings.Contains(fields[2], "1") == owned {
			return
		}
	}
	t.Fatalf("IPv6 route %s via %s owned=%v not found in:\n%s", prefix, nextHop, owned, out)
}

func assertFreeBSDRouteAbsentVM6(t *testing.T, prefix, nextHop string, owned bool) {
	t.Helper()
	out, err := exec.Command("/usr/bin/netstat", "-rn", "-f", "inet6").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == prefix && fields[1] == nextHop && strings.Contains(fields[2], "1") == owned {
			t.Fatalf("IPv6 route %s via %s owned=%v still present:\n%s", prefix, nextHop, owned, out)
		}
	}
}
