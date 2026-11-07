// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDSLiteParentNativeLinuxAFTRLifecycle(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires ROUTERD_RUN_NETNS=1 in an isolated network namespace")
	}
	currentNS, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	hostNS := strings.TrimSpace(os.Getenv("ROUTERD_HOST_NETNS"))
	if !strings.HasPrefix(hostNS, "net:[") || !strings.HasSuffix(hostNS, "]") || currentNS == hostNS {
		t.Fatal("outer network namespace isolation is unverified")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	run := func(name string, args ...string) []byte {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return out
	}
	// Reuse the native peer lifetime: direct child, private namespace FD, no
	// host /run/netns registration and no PID loaded from persistent state.
	peer := exec.CommandContext(ctx, "unshare", "-n", "--", "sleep", "120")
	if err := peer.Start(); err != nil {
		t.Fatal(err)
	}
	peerDone := make(chan error, 1)
	go func() { peerDone <- peer.Wait() }()
	peerStopped := false
	stopPeer := func() {
		if peerStopped {
			return
		}
		_ = peer.Process.Kill()
		select {
		case <-peerDone:
			peerStopped = true
			t.Log("independent AFTR namespace process exited and was reaped")
		case <-time.After(5 * time.Second):
			t.Error("independent AFTR namespace process did not acknowledge exit")
		}
	}
	t.Cleanup(stopPeer)
	namespacePath := fmt.Sprintf("/proc/%d/ns/net", peer.Process.Pid)
	deadline := time.Now().Add(3 * time.Second)
	for {
		namespace, readErr := os.Readlink(namespacePath)
		if readErr == nil && namespace != currentNS {
			t.Logf("isolated DS-Lite namespaces: host=%s B4=%s AFTR=%s", hostNS, currentNS, namespace)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("independent AFTR namespace did not become ready: %v", readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	peerNS, err := os.Open(namespacePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peerNS.Close() })
	peerCommand := func(commandCtx context.Context, name string, args ...string) *exec.Cmd {
		command := exec.CommandContext(commandCtx, "nsenter", append([]string{"--net=/proc/self/fd/3", "--", name}, args...)...)
		command.ExtraFiles = []*os.File{peerNS}
		return command
	}
	peerRun := func(name string, args ...string) []byte {
		out, err := peerCommand(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("independent AFTR %s %v: %v: %s", name, args, err, out)
		}
		return out
	}
	run("ip", "link", "add", "name", "ds-proof-u", "type", "veth", "peer", "name", "ds-aftr-u")
	run("ip", "link", "set", "dev", "ds-aftr-u", "netns", strconv.Itoa(peer.Process.Pid))
	run("ip", "-6", "address", "add", "2001:db8:88::1/64", "dev", "ds-proof-u", "nodad")
	run("ip", "link", "set", "dev", "ds-proof-u", "up")
	peerRun("ip", "-6", "address", "add", "2001:db8:88::2/64", "dev", "ds-aftr-u", "nodad")
	peerRun("ip", "link", "set", "dev", "ds-aftr-u", "up")
	peerRun("ip", "link", "set", "dev", "lo", "up")
	// The independent endpoint uses only the kernel's public iproute2 CLI.
	peerRun("ip", "-6", "tunnel", "add", "aftr-proof", "mode", "ipip6", "local", "2001:db8:88::2", "remote", "2001:db8:88::1", "dev", "ds-aftr-u", "encaplimit", "none")
	peerRun("ip", "-4", "address", "add", "192.0.0.1/32", "peer", "192.0.0.2/32", "dev", "aftr-proof")
	peerRun("ip", "link", "set", "dev", "aftr-proof", "mtu", "1460", "up")
	peerRun("ip", "-4", "address", "add", "192.0.2.2/32", "dev", "lo")
	peerRun("ip", "-4", "route", "add", "192.0.2.1/32", "dev", "aftr-proof")
	database := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ds-proof-u"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "parent"}, Spec: api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "ds-proof", LocalAddressSource: "interface", AFTRIPv6: "2001:db8:88::2", MTU: 1460}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "inside-source"}, Spec: api.IPv4StaticAddressSpec{Interface: "parent", Address: "192.0.2.1/32"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "inside-route"}, Spec: api.IPv4RouteSpec{Destination: "192.0.2.2/32", Device: "ds-proof"}},
	}}}
	parent := DSLiteTunnelController{Router: router, Store: store}
	address := IPv4StaticAddressController{Router: router, Store: store, DSLite: &parent}
	route := IPv4RouteController{Router: router, Store: store, DSLite: &parent}
	acquire := func() dsliteTunnelReceipt {
		if err := parent.reconcile(ctx); err != nil {
			t.Fatalf("native DS-Lite acquisition: %v", err)
		}
		records, err := parent.nativeTunnelReceipts()
		if err != nil || len(records) != 1 || records[0].Pending || records[0].IfIndex < 1 {
			t.Fatalf("native DS-Lite ownership receipt is not acknowledged: records=%+v err=%v", records, err)
		}
		record := records[0]
		if err := address.Reconcile(ctx); err != nil {
			t.Fatalf("native DS-Lite child address: %v", err)
		}
		if err := route.reconcile(ctx); err != nil {
			t.Fatalf("native DS-Lite child route: %v", err)
		}
		status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "inside-source")
		if err != nil {
			t.Fatal(err)
		}
		childAddress, err := staticReceipt(status)
		if err != nil || childAddress == nil || childAddress.Pending || childAddress.IfIndex != record.IfIndex || childAddress.Scope != record.Scope || childAddress.ParentToken != "dslite:"+record.Token {
			t.Fatalf("native DS-Lite child address lacks exact current parent proof: %+v err=%v", childAddress, err)
		}
		status, err = store.ReadObjectStatus(api.NetAPIVersion, "IPv4Route", "inside-route")
		if err != nil {
			t.Fatal(err)
		}
		childRoute, err := ipv4RouteReceiptFromStatus(status)
		if err != nil || childRoute == nil || childRoute.Pending || childRoute.IfIndex != record.IfIndex || childRoute.Scope != record.Scope || childRoute.ParentToken != "dslite:"+record.Token {
			t.Fatalf("native DS-Lite child route lacks exact current parent proof: %+v err=%v", childRoute, err)
		}
		return record
	}
	receivedPackets := func() uint64 {
		var links []struct {
			Stats struct {
				RX struct{ Packets uint64 } `json:"rx"`
			} `json:"stats64"`
		}
		if err := json.Unmarshal(peerRun("ip", "-j", "-s", "link", "show", "dev", "aftr-proof"), &links); err != nil || len(links) != 1 {
			t.Fatalf("independent AFTR receive counters are unavailable: %v", err)
		}
		return links[0].Stats.RX.Packets
	}
	record := acquire()
	before := receivedPackets()
	captureCtx, captureCancel := context.WithTimeout(ctx, 8*time.Second)
	defer captureCancel()
	capture := peerCommand(captureCtx, "tcpdump", "-Z", "root", "-i", "ds-aftr-u", "-c", "1", "-n", "-v", "-Q", "in", "ip6", "proto", "4")
	var packet bytes.Buffer
	capture.Stdout = &packet
	stderr, err := capture.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := capture.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if capture.ProcessState == nil {
			_ = capture.Process.Kill()
			_ = capture.Wait()
		}
	})
	scanner := bufio.NewScanner(stderr)
	ready := false
	var captureMessages []string
	for scanner.Scan() {
		captureMessages = append(captureMessages, scanner.Text())
		if strings.Contains(scanner.Text(), "listening on") {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatalf("independent AFTR capture did not become ready: %v: %s", scanner.Err(), strings.Join(captureMessages, "; "))
	}
	run("ping", "-c", "2", "-W", "2", "-I", "192.0.2.1", "192.0.2.2")
	if err := capture.Wait(); err != nil {
		t.Fatalf("independent AFTR did not capture B4 encapsulation: %v", err)
	}
	if text := packet.String(); !strings.Contains(text, "2001:db8:88::1 > 2001:db8:88::2") || !strings.Contains(text, "192.0.2.1 > 192.0.2.2") {
		t.Fatalf("AFTR capture lacks the outer IPv6 and inner IPv4 packet: %s", text)
	}
	if after := receivedPackets(); after < before+2 {
		t.Fatalf("independent AFTR did not decapsulate both packets: before=%d after=%d", before, after)
	}
	t.Logf("AFTR received B4 encapsulation: %s", strings.TrimSpace(packet.String()))
	run("ip", "-6", "address", "del", "2001:db8:88::1/64", "dev", "ds-proof-u")
	reader := nativeParentReader{Router: router, Store: store, DSLite: &parent}
	if proof, err := reader.observeDeclared(ctx, "ds-proof"); err == nil || proof.Exists {
		t.Fatalf("DS-Lite parent proof survived removal of its actual source: %+v err=%v", proof, err)
	}
	run("ip", "-6", "address", "add", "2001:db8:88::1/64", "dev", "ds-proof-u", "nodad")
	withdraw := func() {
		address.Router, route.Router = &api.Router{}, &api.Router{}
		if err := route.reconcile(ctx); err != nil {
			t.Fatalf("native DS-Lite child route withdrawal: %v", err)
		}
		if err := address.Reconcile(ctx); err != nil {
			t.Fatalf("native DS-Lite child address withdrawal: %v", err)
		}
		if out := run("ip", "-j", "-4", "address", "show", "dev", "ds-proof"); strings.Contains(string(out), "192.0.2.1") {
			t.Fatalf("child address remains before parent withdrawal: %s", out)
		}
		if out := run("ip", "-j", "-4", "route", "show", "192.0.2.2/32"); strings.TrimSpace(string(out)) != "[]" {
			t.Fatalf("child route remains before parent withdrawal: %s", out)
		}
		parent.Router = &api.Router{}
		if err := parent.reconcile(ctx); err != nil {
			t.Fatalf("native DS-Lite withdrawal: %v", err)
		}
		if link, err := parent.observeNativeTunnel(ctx, "ds-proof"); err != nil || link.Exists {
			t.Fatalf("native DS-Lite remains after withdrawal: %+v err=%v", link, err)
		}
		if records, err := parent.nativeTunnelReceipts(); err != nil || len(records) != 0 {
			t.Fatalf("native DS-Lite receipt remains after withdrawal: %+v err=%v", records, err)
		}
	}
	withdraw()
	parent.Router, address.Router, route.Router = router, router, router
	reacquired := acquire()
	if reacquired.Token == record.Token {
		t.Fatalf("withdrawn DS-Lite ownership was reused: old=%+v new=%+v", record, reacquired)
	}
	before = receivedPackets()
	run("ping", "-c", "1", "-W", "2", "-I", "192.0.2.1", "192.0.2.2")
	if after := receivedPackets(); after <= before {
		t.Fatalf("reacquired DS-Lite did not reach the independent AFTR: before=%d after=%d", before, after)
	}
	withdraw()
	// Replace the object after the actor's final observations, at the actual
	// indexed mutation boundary. The old index must never select the namesake.
	parent.Router, address.Router, route.Router = router, router, router
	acquire()
	address.Router, route.Router = &api.Router{}, &api.Router{}
	if err := route.reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := address.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	replaced := false
	parent.DeleteByIndex = func(ctx context.Context, index int, name, proof, scope string) error {
		run("ip", "link", "delete", "dev", name)
		run("ip", "link", "add", "name", name, "type", "ip6tnl", "mode", "ipip6", "local", "2001:db8:88::1", "remote", "2001:db8:88::2", "dev", "ds-proof-u", "encaplimit", "none")
		replaced = true
		return deleteDSLiteByIndex(ctx, index, name, proof, scope)
	}
	parent.Router = &api.Router{}
	if err := parent.reconcile(ctx); err == nil || !replaced {
		t.Fatalf("replacement at indexed deletion was acknowledged as absent: replaced=%t err=%v", replaced, err)
	}
	if link, err := parent.observeNativeTunnel(ctx, "ds-proof"); err != nil || !link.Exists || link.Token != "" {
		t.Fatalf("indexed deletion changed the untagged namesake: %+v err=%v", link, err)
	}
	run("ip", "link", "delete", "dev", "ds-proof")
	parent.DeleteByIndex = nil
	if err := parent.reconcile(ctx); err != nil {
		t.Fatalf("confirmed old-owner absence did not retire its pending receipt: %v", err)
	}
	stopPeer()
	if err := peerNS.Close(); err != nil {
		t.Fatal(err)
	}
}
