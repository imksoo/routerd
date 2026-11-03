// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestVMACNativeRARenewalWithdrawalAndExpiry(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires isolated root network namespace and radvd")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	initial, err := os.Readlink("/proc/1/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 || current == initial {
		t.Fatal("refuse native mutations in the initial network namespace")
	}
	radvdPath, err := exec.LookPath("radvd")
	if err != nil {
		t.Fatal("explicit native run requires independent radvd: ", err)
	}
	native := func(name string, args ...string) string {
		out, err := exec.CommandContext(t.Context(), name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return string(out)
	}
	peer := exec.CommandContext(t.Context(), "unshare", "-n", "sleep", "60")
	if err := peer.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Process.Kill(); _ = peer.Wait() })
	peerPID := strconv.Itoa(peer.Process.Pid)
	deadline := time.Now().Add(5 * time.Second)
	for {
		ns, err := os.Readlink("/proc/" + peerPID + "/ns/net")
		if err == nil && ns != current {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("independent RA namespace did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	peerRun := func(args ...string) string {
		return native("nsenter", append([]string{"-t", peerPID, "-n"}, args...)...)
	}
	native("ip", "link", "add", "rd15-wan", "type", "veth", "peer", "name", "rd15-peer")
	t.Cleanup(func() { exec.Command("ip", "link", "del", "rd15-wan").Run() })
	native("ip", "link", "set", "rd15-peer", "netns", peerPID)
	native("sysctl", "-qw", "net.ipv6.conf.all.forwarding=1")
	native("ip", "link", "set", "rd15-wan", "up")
	peerRun("sysctl", "-qw", "net.ipv6.conf.all.forwarding=1")
	peerRun("ip", "link", "set", "lo", "up")
	peerRun("ip", "link", "set", "rd15-peer", "up")
	peerRun("ip", "-6", "addr", "add", "fe80::1/64", "dev", "rd15-peer", "nodad")
	native("ip", "link", "add", "rd15-mgmt", "type", "dummy")
	t.Cleanup(func() { exec.Command("ip", "link", "del", "rd15-mgmt").Run() })
	native("ip", "link", "set", "rd15-mgmt", "up")
	native("ip", "-6", "addr", "add", "fe80::2/64", "dev", "rd15-mgmt", "nodad")
	native("ip", "-6", "route", "add", "default", "via", "fe80::3", "dev", "rd15-mgmt", "metric", "100")
	native("ip", "link", "add", "rd15-vmac", "link", "rd15-wan", "type", "macvlan", "mode", "private")
	native("ip", "link", "set", "rd15-vmac", "address", "02:00:5e:00:01:31", "up")
	native("ip", "-6", "addr", "add", "fe80::131/64", "dev", "rd15-vmac", "nodad")
	native("ip", "-6", "addr", "add", "2001:db8:25::131/64", "dev", "rd15-vmac", "nodad")
	hooks := runHooks{
		command: func(name string, args ...string) ([]byte, error) {
			return exec.CommandContext(t.Context(), name, args...).CombinedOutput()
		},
		inspectVMAC:                inspectVMACRuntimeState,
		requestRA:                  func(string) error { return nil },
		conntrackdTransitionNeeded: func(string) (bool, error) { return false, nil },
	}
	activate := func() {
		if err := runWithHooks([]string{"activate", "--vmac", "rd15-wan,rd15-vmac,02:00:5e:00:01:31,fe80::131,false"}, hooks); err != nil {
			t.Fatal(err)
		}
	}
	activate()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "radvd.conf")
	if err := os.WriteFile(cfg, []byte("interface rd15-peer { AdvSendAdvert on; MinRtrAdvInterval 3; MaxRtrAdvInterval 4; AdvDefaultLifetime 6; };\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var daemon *exec.Cmd
	start := func() {
		daemon = exec.CommandContext(t.Context(), "nsenter", "-t", peerPID, "-n", radvdPath, "-n", "-m", "stderr", "-C", cfg, "-p", filepath.Join(dir, "radvd.pid"))
		daemon.Stdout = os.Stderr
		daemon.Stderr = os.Stderr
		if err := daemon.Start(); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if daemon != nil {
			_ = daemon.Process.Kill()
			_ = daemon.Wait()
		}
	})
	stop := func(signal os.Signal) {
		if err := daemon.Process.Signal(signal); err != nil {
			t.Fatal(err)
		}
		_ = daemon.Wait()
		daemon = nil
	}
	waitRoute := func(device string, seconds int) {
		deadline := time.Now().Add(time.Duration(seconds) * time.Second)
		for {
			activate()
			route := native("ip", "-6", "route", "get", "2001:db8:ffff::80")
			if strings.Contains(route, "dev "+device) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("expected %s: %s", device, route)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	start()
	waitRoute("rd15-vmac", 6)
	// Stay preferred beyond the first RA's six-second lifetime while the
	// independent peer renews it. A userspace copy used to expire and stick.
	for range 9 {
		activate()
		route := native("ip", "-6", "route", "get", "2001:db8:ffff::80")
		if !strings.Contains(route, "dev rd15-vmac") {
			t.Fatalf("healthy RA renewal lost forwarding: %s; routes: %s", route, native("ip", "-6", "route", "show", "default"))
		}
		time.Sleep(time.Second)
	}
	stop(syscall.SIGTERM)
	waitRoute("rd15-mgmt", 5)
	start()
	waitRoute("rd15-vmac", 6)
	stop(syscall.SIGKILL)
	waitRoute("rd15-mgmt", 9)
	t.Log("native RA renewal, explicit withdrawal and natural expiry preserve the independent management default")
}
