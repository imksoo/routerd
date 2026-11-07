// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package bfd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestBFDIndependentFRRNativeLinuxLifecycle(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires explicit isolated Linux namespace execution")
	}
	currentNS, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Skipf("cannot read network namespace: %v", err)
	}
	hostNS := os.Getenv("ROUTERD_HOST_NETNS")
	if hostNS == "" {
		hostNS, err = os.Readlink("/proc/1/ns/net")
		if err != nil {
			t.Skipf("cannot verify host network identity: %v", err)
		}
	}
	if !strings.HasPrefix(hostNS, "net:[") || !strings.HasSuffix(hostNS, "]") || hostNS == currentNS {
		t.Skip("network namespace isolation is unverified")
	}
	frr := os.Getenv("ROUTERD_FRR_BFDD")
	if frr == "" {
		frr = "/usr/lib/frr/bfdd"
	}
	if info, err := os.Stat(frr); err != nil || info.Mode()&0111 == 0 {
		t.Skip("independent FRR bfdd is unavailable")
	}
	for _, mode := range []string{"multihop", "single-hop"} {
		t.Run(mode, func(t *testing.T) {
			runBFDIndependentFRRNativeLifecycle(t, currentNS, hostNS, frr, mode)
		})
	}
}

func runBFDIndependentFRRNativeLifecycle(t *testing.T, currentNS, hostNS, frr, mode string) {
	ctx, cancel := context.WithTimeout(t.Context(), 65*time.Second)
	t.Cleanup(cancel)
	evidence := os.Getenv("ROUTERD_NATIVE_EVIDENCE")
	if evidence == "" {
		evidence = t.TempDir()
	} else {
		evidence = filepath.Join(evidence, mode)
		if err := os.MkdirAll(evidence, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(name string, args ...string) []byte {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return out
	}
	// Foreground daemons and the peer namespace each have one Wait owner.
	start := func(label string, command *exec.Cmd) func() {
		log, err := os.Create(filepath.Join(evidence, label+".log"))
		if err != nil {
			t.Fatal(err)
		}
		command.Stdout, command.Stderr = log, log
		if err := command.Start(); err != nil {
			_ = log.Close()
			t.Fatalf("start %s: %v", label, err)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		var once sync.Once
		stop := func() {
			once.Do(func() {
				_ = command.Process.Signal(syscall.SIGTERM)
				select {
				case exit := <-done:
					t.Logf("%s exited and was reaped: %v", label, exit)
				case <-time.After(5 * time.Second):
					_ = command.Process.Kill()
					select {
					case exit := <-done:
						t.Errorf("%s required kill before reaping: %v", label, exit)
					case <-time.After(3 * time.Second):
						t.Errorf("%s did not acknowledge exit", label)
					}
				}
				_ = log.Close()
			})
		}
		t.Cleanup(stop)
		return stop
	}
	peer := exec.CommandContext(ctx, "unshare", "-n", "--", "sleep", "120")
	stopPeerNS := start("bfd-peer-namespace", peer)
	namespacePath := fmt.Sprintf("/proc/%d/ns/net", peer.Process.Pid)
	deadline := time.Now().Add(3 * time.Second)
	for {
		namespace, readErr := os.Readlink(namespacePath)
		if readErr == nil && namespace != currentNS {
			t.Logf("isolated BFD namespaces: host=%s router=%s peer=%s", hostNS, currentNS, namespace)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("peer namespace did not become independent: %v", readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	peerNS, err := os.Open(namespacePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peerNS.Close() })
	peerCommand := func(name string, args ...string) *exec.Cmd {
		command := exec.CommandContext(ctx, "nsenter", append([]string{"--net=/proc/self/fd/3", "--", name}, args...)...)
		command.ExtraFiles = []*os.File{peerNS}
		return command
	}
	peerRun := func(name string, args ...string) []byte {
		out, err := peerCommand(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("independent peer %s %v: %v: %s", name, args, err, out)
		}
		return out
	}
	run("ip", "link", "set", "lo", "up")
	run("ip", "link", "add", "name", "bfd-proof", "type", "veth", "peer", "name", "bfd-peer")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cleanupCtx, "ip", "link", "delete", "dev", "bfd-proof").CombinedOutput()
		if err != nil && !strings.Contains(string(out), "Cannot find device") {
			t.Errorf("remove fixture-owned veth: %v: %s", err, out)
		}
	})
	run("ip", "link", "set", "dev", "bfd-peer", "netns", strconv.Itoa(peer.Process.Pid))
	run("ip", "address", "add", "198.18.0.1/30", "dev", "bfd-proof")
	run("ip", "link", "set", "dev", "bfd-proof", "up")
	peerRun("ip", "link", "set", "lo", "up")
	peerRun("ip", "address", "add", "198.18.0.2/30", "dev", "bfd-peer")
	peerRun("ip", "link", "set", "dev", "bfd-peer", "up")
	base := t.TempDir()
	localDir, peerDir := filepath.Join(base, "local"), filepath.Join(base, "peer")
	for _, dir := range []string{localDir, peerDir} {
		if err := os.MkdirAll(filepath.Join(dir, "vty"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "vtysh.conf"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(localDir, "bfdd.conf"), []byte("log stdout\n"), 0600); err != nil {
		t.Fatal(err)
	}
	peerHeader, localHeader := "peer 198.18.0.1 multihop local-address 198.18.0.2", "peer 198.18.0.2 multihop local-address 198.18.0.1"
	bindInterface := ""
	if mode == "single-hop" {
		peerHeader, localHeader = "peer 198.18.0.1 interface bfd-peer", "peer 198.18.0.2 interface bfd-proof"
		bindInterface = "bfd-proof"
	}
	if err := os.WriteFile(filepath.Join(peerDir, "bfdd.conf"), []byte("log stdout\nbfd\n "+peerHeader+"\n  receive-interval 100\n  transmit-interval 100\n  detect-multiplier 3\n exit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bfddArgs := func(dir string) []string {
		return []string{"-u", "root", "-g", "root", "-f", filepath.Join(dir, "bfdd.conf"), "-i", filepath.Join(dir, "bfdd.pid"), "--vty_socket", filepath.Join(dir, "vty"), "-z", filepath.Join(dir, "zebra.sock"), "--bfdctl", filepath.Join(dir, "bfdctl.sock"), "--log", "stdout"}
	}
	var stopZebra []func()
	if mode == "single-hop" {
		zebra := filepath.Join(filepath.Dir(frr), "zebra")
		if _, err := os.Stat(zebra); err != nil {
			t.Fatalf("single-hop interface inventory requires zebra: %v", err)
		}
		for _, side := range []struct {
			label, dir string
			peer       bool
		}{{"local", localDir, false}, {"peer", peerDir, true}} {
			config := filepath.Join(side.dir, "zebra.conf")
			if err := os.WriteFile(config, []byte("log stdout\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"-u", "root", "-g", "root", "-f", config, "-i", filepath.Join(side.dir, "zebra.pid"), "-z", filepath.Join(side.dir, "zebra.sock"), "--vty_socket", filepath.Join(side.dir, "vty"), "--log", "stdout"}
			command := exec.CommandContext(ctx, zebra, args...)
			if side.peer {
				command = peerCommand(zebra, args...)
			}
			stopZebra = append(stopZebra, start("bfd-zebra-"+side.label, command))
			deadline := time.Now().Add(5 * time.Second)
			for {
				if info, err := os.Stat(filepath.Join(side.dir, "zebra.sock")); err == nil && info.Mode()&os.ModeSocket != 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("%s zebra did not become ready", side.label)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
	stopLocal := start("bfd-local", exec.CommandContext(ctx, frr, bfddArgs(localDir)...))
	stopPeer := start("bfd-peer", peerCommand(frr, bfddArgs(peerDir)...))
	vtyArgs := func(dir string, args ...string) []string {
		return append([]string{"--config_dir", dir, "--vty_socket", filepath.Join(dir, "vty"), "--histfile", filepath.Join(dir, "vtysh.history"), "-d", "bfdd"}, args...)
	}
	database := filepath.Join(base, "routerd.sqlite")
	store, err := routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "edge"}, Spec: api.BGPRouterSpec{ASN: 64512, RouterID: "198.18.0.1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BFD"}, Metadata: api.ObjectMeta{Name: "peer"}, Spec: api.BFDSpec{Peer: "198.18.0.2", Interface: bindInterface, MinRx: "100ms", MinTx: "100ms", DetectMultiplier: 3}},
	}}}
	controller := Controller{Router: router, Store: store, RuntimeDir: filepath.Join(base, "runtime"), Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, vtyArgs(localDir, args...)...).CombinedOutput()
	}}
	waitState := func(label, want string) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		var lastErr error
		for {
			lastErr = controller.Reconcile(ctx)
			status, err := store.ReadObjectStatus(api.NetAPIVersion, "BFD", "peer")
			if lastErr == nil && err == nil && status["phase"] == want {
				t.Logf("%s: %+v", label, status)
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: want %s, status=%+v reconcile=%v read=%v native=%s peer=%s", label, want, status, lastErr, err, run("vtysh", vtyArgs(localDir, "-c", "show bfd peers json")...), peerRun("vtysh", vtyArgs(peerDir, "-c", "show bfd peers json")...))
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	waitState("actual peer reception", "Up")
	t.Logf("actual local BFD peer JSON: %s", run("vtysh", vtyArgs(localDir, "-c", "show bfd peers json")...))
	t.Logf("actual local BFD peer text: %s", run("vtysh", vtyArgs(localDir, "-c", "show bfd peers")...))
	t.Logf("actual independent BFD peer JSON: %s", peerRun("vtysh", vtyArgs(peerDir, "-c", "show bfd peers json")...))
	stopPeer()
	waitState("peer process stopped", "Down")
	stopPeer = start("bfd-peer-restarted", peerCommand(frr, bfddArgs(peerDir)...))
	waitState("peer process restarted", "Up")
	localVTY := func(commands ...string) []byte {
		args := []string{}
		for _, command := range commands {
			args = append(args, "-c", command)
		}
		return run("vtysh", vtyArgs(localDir, args...)...)
	}
	localVTY("configure terminal", "bfd", localHeader, "shutdown")
	waitState("real FRR administrative shutdown", "Down")
	localVTY("configure terminal", "bfd", localHeader, "no shutdown")
	waitState("administrative shutdown removed", "Up")
	stopLocal()
	if err := controller.Reconcile(ctx); err == nil {
		t.Fatal("unavailable local bfdd accepted as a successful observation")
	}
	failed, err := store.ReadObjectStatus(api.NetAPIVersion, "BFD", "peer")
	failedStates, _ := failed["peerStates"].(map[string]any)
	if err != nil || failed["phase"] != "Degraded" || failedStates["198.18.0.2"] != "Unknown" {
		t.Fatalf("local bfdd outage did not publish degradation: %+v err=%v", failed, err)
	}
	stopLocal = start("bfd-local-restarted", exec.CommandContext(ctx, frr, bfddArgs(localDir)...))
	waitState("local bfdd restarted from durable ownership", "Up")
	// A second configured tuple belongs to the operator, not this controller.
	localVTY("configure terminal", "bfd", "peer 198.18.1.2 multihop local-address 198.18.0.1")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	controller.Store = store
	waitState("SQLite reopen retains native ownership", "Up")
	controller.Router = &api.Router{}
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatalf("withdraw configured BFD: %v", err)
	}
	peers, err := observeFRRBFD(ctx, controller.runner(), controller.vtysh())
	foreignKey := sessionKey(session{Address: "198.18.1.2", LocalAddr: "198.18.0.1"})
	if err != nil || len(peers) != 1 || peers[foreignKey].Type != "configured" {
		t.Fatalf("withdrawal did not preserve only the foreign tuple: peers=%+v err=%v", peers, err)
	}
	receipt, err := controller.readNativeReceipt()
	if err != nil || len(receipt.Held)+len(receipt.Pending) != 0 {
		t.Fatalf("BFD receipt remains after withdrawal: %+v err=%v", receipt, err)
	}
	t.Log("resource withdrawal acknowledged by real bfdd and SQLite; foreign configured tuple retained")
	stopPeer()
	stopLocal()
	for _, stop := range stopZebra {
		stop()
	}
	stopPeerNS()
}
