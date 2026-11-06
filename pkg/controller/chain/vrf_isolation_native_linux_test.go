// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// Equal destination addresses must resolve to different actual peers, including
// after receipt reopen and removal of one routing domain.
func TestVRFNativeLinuxOverlappingPeerIsolation(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires ROUTERD_RUN_NETNS=1 in an isolated network namespace")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	host := os.Getenv("ROUTERD_HOST_NETNS")
	if host == "" {
		host, err = os.Readlink("/proc/1/ns/net")
	}
	if err != nil || !strings.HasPrefix(host, "net:[") || current == host {
		t.Skip("network namespace isolation is unverified")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	run := func(name string, args ...string) string {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	start := func(cmd *exec.Cmd) {
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("peer process did not exit")
			}
		})
	}
	router := &api.Router{}
	for i, name := range []string{"blue", "red"} {
		peer := exec.CommandContext(ctx, "unshare", "-n", "sleep", "120")
		start(peer)
		nsPath := fmt.Sprintf("/proc/%d/ns/net", peer.Process.Pid)
		deadline := time.Now().Add(3 * time.Second)
		for {
			ns, err := os.Readlink(nsPath)
			if err == nil && ns != current {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("independent peer namespace did not start")
			}
			time.Sleep(10 * time.Millisecond)
		}
		port := "vri-" + name
		run("ip", "link", "add", port, "type", "veth", "peer", "name", "peer-"+name)
		run("ip", "link", "set", "peer-"+name, "netns", fmt.Sprint(peer.Process.Pid))
		run("ip", "link", "set", port, "up")
		peerRun := func(args ...string) string {
			return run("nsenter", append([]string{"--net=" + nsPath, "--", "ip"}, args...)...)
		}
		peerRun("link", "set", "lo", "up")
		peerRun("link", "set", "peer-"+name, "up")
		peerRun("address", "add", "198.18.0.2/24", "dev", "peer-"+name)
		peerRun("address", "add", fmt.Sprintf("198.18.0.%d/24", 3+i), "dev", "peer-"+name)
		server := exec.CommandContext(ctx, "nsenter", "--net="+nsPath, "--", "python3", "-u", "-c", `
import socket,sys
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM)
s.bind(('0.0.0.0',18081))
print('ready',flush=True)
while True:
 data,addr=s.recvfrom(1024)
 s.sendto(sys.argv[1].encode()+b':'+data,addr)
`, name)
		stdout, err := server.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		start(server)
		if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
			t.Fatalf("peer listener failed: %q %v", line, err)
		}
		router.Spec.Resources = append(router.Spec.Resources,
			api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: port}, Spec: api.InterfaceSpec{IfName: port}},
			api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: name}, Spec: api.VRFSpec{IfName: "vrf-" + name, RouteTable: 101 + i, Members: []string{port}}},
			api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: name + "-address"}, Spec: api.IPv4StaticAddressSpec{Interface: port, Address: "198.18.0.1/24"}},
		)
	}
	database := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	reconcile := func() {
		parent := VRFController{Router: router, Store: store}
		if err := parent.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
		address := IPv4StaticAddressController{Router: router, Store: store, VRFs: &parent}
		if err := address.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	request := func(name, address, want string) {
		got := run("python3", "-c", `
import socket,sys
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM)
s.setsockopt(socket.SOL_SOCKET,socket.SO_BINDTODEVICE,sys.argv[1].encode()+b'\0')
s.settimeout(1)
s.sendto(b'isolation-probe',(sys.argv[2],18081))
try:
 data,addr=s.recvfrom(1024)
 print(data.decode())
except socket.timeout:
 print('timeout')
`, "vrf-"+name, address)
		if got != want {
			t.Fatalf("%s to %s: got %q, want %q", name, address, got, want)
		}
	}
	check := func() {
		request("blue", "198.18.0.2", "blue:isolation-probe")
		request("red", "198.18.0.2", "red:isolation-probe")
		request("blue", "198.18.0.3", "blue:isolation-probe")
		request("red", "198.18.0.4", "red:isolation-probe")
		request("blue", "198.18.0.4", "timeout")
		request("red", "198.18.0.3", "timeout")
		if main := run("ip", "-4", "route", "show", "table", "main"); strings.Contains(main, "198.18.0.0/24") {
			t.Fatalf("overlapping network leaked into main: %s", main)
		}
	}
	reconcile()
	check()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	reconcile()
	check()
	// Explicit child withdrawal precedes deleting its parent, as in runtime.
	router.Spec.Resources = router.Spec.Resources[3:]
	parent := VRFController{Router: router, Store: store}
	address := IPv4StaticAddressController{Router: router, Store: store, VRFs: &parent}
	if err := address.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	reconcile()
	request("red", "198.18.0.2", "red:isolation-probe")
	request("red", "198.18.0.3", "timeout")
	if err := exec.CommandContext(ctx, "ip", "link", "show", "vrf-blue").Run(); err == nil {
		t.Fatal("removed blue VRF remains present")
	}
	router.Spec.Resources = nil
	if err := address.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := parent.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := exec.CommandContext(ctx, "ip", "link", "show", "vrf-red").Run(); err == nil {
		t.Fatal("removed red VRF remains present")
	}
	for _, port := range []string{"vri-blue", "vri-red"} {
		if got := run("ip", "-4", "address", "show", "dev", port); strings.Contains(got, "198.18.0.1/") {
			t.Fatalf("withdrawn address remains: %s", got)
		}
	}
	receipts, err := parent.readReceipts()
	if err != nil || len(receipts) != 0 {
		t.Fatalf("withdrawal retained VRF receipts: %+v %v", receipts, err)
	}
	t.Log("distinct UDP payloads across overlapping VRFs, cross-VRF denial, reopen and independent withdrawal passed")
}
