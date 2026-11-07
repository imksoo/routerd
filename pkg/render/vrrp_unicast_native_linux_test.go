// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package render

import (
	"context"
	"fmt"
	"github.com/imksoo/routerd/pkg/api"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Explicit isolated-kernel regression for the native two-master observation.
// Normal go test never creates links, namespaces or processes for this test.
func TestKeepalivedUnicastNativeAddressOrder(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires explicit isolated network fixture")
	}
	self, _ := os.Readlink("/proc/self/ns/net")
	host, _ := os.Readlink("/proc/1/ns/net")
	if os.Geteuid() != 0 || self == "" || self == host {
		t.Fatal("run under sudo unshare -n -m; refusing host network mutation")
	}
	for _, name := range []string{"ip", "keepalived"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(name + " is unavailable")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	command := func(args ...string) string {
		t.Helper()
		b, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v: %s", args, err, b)
		}
		return string(b)
	}
	work := t.TempDir()
	for _, n := range []string{"rd15-vrrp-a", "rd15-vrrp-b"} {
		command("ip", "netns", "add", n)
		t.Cleanup(func() {
			if b, err := exec.Command("ip", "netns", "delete", n).CombinedOutput(); err != nil {
				t.Errorf("remove %s: %v %s", n, err, b)
			}
		})
	}
	command("ip", "link", "add", "name", "rd15-a", "type", "veth", "peer", "name", "rd15-b")
	command("ip", "link", "set", "dev", "rd15-a", "netns", "rd15-vrrp-a")
	command("ip", "link", "set", "dev", "rd15-b", "netns", "rd15-vrrp-b")
	for _, entry := range []struct{ ns, dev string }{{"rd15-vrrp-a", "rd15-a"}, {"rd15-vrrp-b", "rd15-b"}} {
		command("ip", "-n", entry.ns, "link", "set", "dev", entry.dev, "name", "eth0")
		command("ip", "-n", entry.ns, "link", "set", "dev", "lo", "up")
		command("ip", "-n", entry.ns, "link", "set", "dev", "eth0", "up")
	}
	command("ip", "-n", "rd15-vrrp-a", "addr", "add", "198.18.30.2/24", "dev", "eth0")
	// Shared VIP precedes the node address. A backup transition removes the VIP,
	// but an implicitly bound raw socket can remain attached to its old address.
	command("ip", "-n", "rd15-vrrp-b", "addr", "add", "198.18.30.1/32", "dev", "eth0")
	command("ip", "-n", "rd15-vrrp-b", "addr", "add", "198.18.30.3/24", "dev", "eth0")
	command("ip", "-n", "rd15-vrrp-b", "addr", "add", "198.18.29.3/24", "dev", "eth0")
	peer := []byte("global_defs {\n router_id independent\n}\nvrrp_instance peer {\n state BACKUP\n interface eth0\n virtual_router_id 130\n priority 150\n advert_int 1\n nopreempt\n unicast_src_ip 198.18.30.2\n unicast_peer {\n 198.18.30.3\n }\n no_virtual_ipaddress\n}\n")
	start := func(node string, data []byte) string {
		t.Helper()
		path := filepath.Join(work, node+".conf")
		logPath := filepath.Join(work, node+".log")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		log, err := os.Create(logPath)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, "ip", "netns", "exec", "rd15-vrrp-"+node, "keepalived", "-P", "-n", "-l", "-G", "-D", "-f", path, "-p", filepath.Join(work, node+".pid"), "-r", filepath.Join(work, node+"-vrrp.pid"))
		cmd.Stdout, cmd.Stderr = log, log
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			log.Close()
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				<-done
			}
			log.Close()
		})
		return logPath
	}
	peerLog := start("a", peer)
	deadline := time.Now().Add(8 * time.Second)
	for {
		b, _ := os.ReadFile(peerLog)
		if strings.Contains(string(b), "Entering MASTER STATE") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("independent peer did not elect: %s", b)
		}
		time.Sleep(50 * time.Millisecond)
	}
	router := unicastSourceRouter()
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "legacy-lan"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "198.18.29.3/24"}})
	spec, _ := router.Spec.Resources[1].VirtualAddressSpec()
	spec.VRRP.GracefulActivation = &api.VirtualAddressVRRPGracefulActivationSpec{ReadyWhen: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"IPv4StaticAddress/node.phase": {Equals: "Applied"}}}}
	spec.VRRP.FailoverVMAC = &api.VirtualAddressVRRPFailoverVMACSpec{ParentInterface: "lan", Interface: "test-vmac", MACAddress: "02:00:5e:00:01:30"}
	router.Spec.Resources[1].Spec = spec
	data, err := KeepalivedConfig(router, map[string]string{"lan": "eth0"})
	if err != nil {
		t.Fatal(err)
	}
	// This component fixture owns the deferred VIP withdrawal. Exercise the
	// actual rendered unicast/socket configuration without invoking host-installed
	// routerd VMAC helpers; full hooks are exercised by the packaged two-VM gate.
	var wire []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "notify_") {
			wire = append(wire, line)
		}
	}
	nodeLog := start("b", []byte(strings.Join(wire, "\n")))
	deadline = time.Now().Add(3 * time.Second)
	for {
		raw := command("ip", "netns", "exec", "rd15-vrrp-b", "cat", "/proc/net/raw")
		if strings.Contains(raw, ":0070") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("native unicast socket did not open: %s", raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
	command("ip", "-n", "rd15-vrrp-b", "addr", "del", "198.18.30.1/32", "dev", "eth0")
	// Observe beyond the three-advertisement master-down interval. A faulted
	// instance is not a success: also require the actual receive bind and BACKUP.
	timer := time.NewTimer(6 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	raw := command("ip", "netns", "exec", "rd15-vrrp-b", "cat", "/proc/net/raw")
	log, _ := os.ReadFile(nodeLog)
	if !strings.Contains(raw, "031E12C6:0070") || !strings.Contains(string(log), "Entering BACKUP STATE") || strings.Contains(string(log), "Entering MASTER STATE") {
		t.Fatalf("node failed to remain backup with its own unicast receive address:\n%s\n%s", raw, log)
	}
	addresses := command("ip", "-n", "rd15-vrrp-b", "-4", "-o", "addr", "show", "dev", "eth0")
	if strings.Contains(addresses, "198.18.30.1/") {
		t.Fatalf("backup owns shared VIP: %s", addresses)
	}
	t.Log(fmt.Sprintf("native keepalived: correct receive address, active independent peer, no duplicate master or VIP"))
}
