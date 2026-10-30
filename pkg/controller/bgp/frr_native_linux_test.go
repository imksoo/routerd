// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package bgp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/manageddaemon"
	routerstate "github.com/imksoo/routerd/pkg/state"
	gobgpapi "github.com/osrg/gobgp/v4/api"
)

func TestBGPIndependentFRRNativeLinuxLifecycle(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires explicit isolated Linux namespace execution")
	}
	currentNS, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Skipf("cannot read network namespace: %v", err)
	}
	hostNS := strings.TrimSpace(os.Getenv("ROUTERD_HOST_NETNS"))
	if hostNS == "" {
		hostNS, err = os.Readlink("/proc/1/ns/net")
		if err != nil {
			t.Skipf("cannot verify host network identity: %v", err)
		}
	}
	if !strings.HasPrefix(hostNS, "net:[") || !strings.HasSuffix(hostNS, "]") || hostNS == currentNS {
		t.Skip("network namespace isolation is unverified")
	}
	// The actual daemon's singleton owner intentionally uses /run/routerd.
	// Require a distinct filesystem before starting it, not only a new netns.
	hostRunDevice, err := strconv.ParseUint(os.Getenv("ROUTERD_HOST_RUN_DEVICE"), 10, 64)
	var runInfo syscall.Stat_t
	if err != nil || syscall.Stat("/run", &runInfo) != nil || uint64(runInfo.Dev) == hostRunDevice {
		t.Skip("requires a fresh private /run filesystem and ROUTERD_HOST_RUN_DEVICE")
	}
	binary := os.Getenv("ROUTERD_BGP_BIN")
	if !filepath.IsAbs(binary) {
		t.Skip("requires ROUTERD_BGP_BIN pointing to the built production routerd-bgp binary")
	}
	if info, err := os.Stat(binary); err != nil || info.Mode()&0111 == 0 {
		t.Skip("production routerd-bgp binary is unavailable")
	}
	frr := os.Getenv("ROUTERD_FRR_BGPD")
	if frr == "" {
		frr = "/usr/lib/frr/bgpd"
	}
	if info, err := os.Stat(frr); err != nil || info.Mode()&0111 == 0 {
		t.Skip("independent FRR bgpd is unavailable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	evidence := os.Getenv("ROUTERD_NATIVE_EVIDENCE")
	if evidence == "" {
		evidence = t.TempDir()
	}
	if err := os.MkdirAll(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(name string, args ...string) []byte {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return out
	}
	// Each real daemon remains a directly owned foreground process. The same
	// stop callback is used by the controller removal path and test cleanup.
	start := func(label string, command *exec.Cmd) func(context.Context) error {
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
		var result error
		stop := func(stopCtx context.Context) error {
			once.Do(func() {
				_ = command.Process.Signal(syscall.SIGTERM)
				select {
				case exit := <-done:
					t.Logf("%s exited and was reaped: %v", label, exit)
				case <-stopCtx.Done():
					_ = command.Process.Kill()
					select {
					case <-done:
						result = stopCtx.Err()
					case <-time.After(5 * time.Second):
						result = fmt.Errorf("%s did not acknowledge exit", label)
					}
				}
				_ = log.Close()
			})
			return result
		}
		t.Cleanup(func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer stopCancel()
			if err := stop(stopCtx); err != nil {
				t.Error(err)
			}
		})
		return stop
	}
	peer := exec.CommandContext(ctx, "unshare", "-n", "--", "sleep", "180")
	_ = start("peer-namespace", peer)
	namespacePath := fmt.Sprintf("/proc/%d/ns/net", peer.Process.Pid)
	deadline := time.Now().Add(3 * time.Second)
	for {
		namespace, readErr := os.Readlink(namespacePath)
		if readErr == nil && namespace != currentNS {
			t.Logf("isolated BGP namespaces: host=%s router=%s peer=%s private_run_device=%d", hostNS, currentNS, namespace, runInfo.Dev)
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
	run("ip", "link", "add", "name", "bgp-proof", "type", "veth", "peer", "name", "bgp-peer")
	run("ip", "link", "set", "dev", "bgp-peer", "netns", strconv.Itoa(peer.Process.Pid))
	run("ip", "address", "add", "198.18.0.1/30", "dev", "bgp-proof")
	run("ip", "-6", "address", "add", "2001:db8:1::1/64", "dev", "bgp-proof", "nodad")
	run("ip", "link", "set", "dev", "bgp-proof", "up")
	peerRun("ip", "link", "set", "lo", "up")
	peerRun("ip", "address", "add", "198.18.0.2/30", "dev", "bgp-peer")
	peerRun("ip", "-6", "address", "add", "2001:db8:1::2/64", "dev", "bgp-peer", "nodad")
	peerRun("ip", "link", "set", "dev", "bgp-peer", "up")
	peerRun("ip", "address", "add", "198.19.40.1/32", "dev", "lo")
	peerRun("ip", "-6", "address", "add", "2001:db8:40::1/128", "dev", "lo", "nodad")
	base := t.TempDir()
	frrConfig := filepath.Join(base, "bgpd.conf")
	if err := os.WriteFile(filepath.Join(base, "vtysh.conf"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	vty := filepath.Join(base, "vty")
	if err := os.Mkdir(vty, 0700); err != nil {
		t.Fatal(err)
	}
	peerVTY := func(commands ...string) []byte {
		args := []string{"--config_dir", base, "--vty_socket", vty, "--histfile", filepath.Join(base, "vtysh.history"), "-d", "bgpd"}
		for _, command := range commands {
			args = append(args, "-c", command)
		}
		return peerRun("vtysh", args...)
	}
	config := `frr defaults traditional
hostname routerd-independent-peer
log stdout
router bgp 64513
 bgp router-id 198.18.0.2
 no bgp default ipv4-unicast
 no bgp ebgp-requires-policy
 no bgp network import-check
 bgp disable-ebgp-connected-route-check
 neighbor 198.18.0.1 remote-as 64512
 neighbor 198.18.0.1 passive
 neighbor 198.18.0.1 timers 1 3
 neighbor 2001:db8:1::1 remote-as 64512
 neighbor 2001:db8:1::1 passive
 neighbor 2001:db8:1::1 timers 1 3
 address-family ipv4 unicast
  neighbor 198.18.0.1 activate
  network 198.19.40.1/32
 exit-address-family
 address-family ipv6 unicast
  neighbor 2001:db8:1::1 activate
  network 2001:db8:40::1/128
 exit-address-family
!
`
	if err := os.WriteFile(frrConfig, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	frrCommand := func() *exec.Cmd {
		return peerCommand(frr, "-Z", "-n", "-S", "-f", frrConfig, "-i", filepath.Join(base, "bgpd.pid"), "--vty_socket", vty, "-P", "0", "--log", "stdout")
	}
	stopFRR := start("frr-initial", frrCommand())
	spec := manageddaemon.Spec{Name: "routerd-bgp", Binary: binary, SocketPath: filepath.Join(base, "gobgp.sock"), ControlSocketPath: filepath.Join(base, "control.sock"), StatePath: filepath.Join(base, "applied.json")}
	productCommand := func() *exec.Cmd {
		return exec.CommandContext(ctx, binary, "daemon", "--socket", spec.SocketPath, "--control-socket", spec.ControlSocketPath, "--state-file", spec.StatePath)
	}
	stopProduct := start("routerd-bgp-initial", productCommand())
	database := filepath.Join(base, "state.db")
	store, err := routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			_ = store.Close()
		}
	})
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "edge"}, Spec: api.BGPRouterSpec{ASN: 64512, RouterID: "198.18.0.1", ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"198.19.40.1/32"}}, Timers: api.BGPTimersSpec{Profile: "fast"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "frr-v4"}, Spec: api.BGPPeerSpec{RouterRef: "BGPRouter/edge", PeerASN: 64513, Peers: []string{"198.18.0.2"}, ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"198.19.40.1/32"}, NextHopRewrite: "unchanged"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "frr-v6"}, Spec: api.BGPPeerSpec{RouterRef: "BGPRouter/edge", PeerASN: 64513, Peers: []string{"2001:db8:1::2"}, ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"2001:db8:40::1/128"}, NextHopRewrite: "unchanged"}}},
	}}}
	controller := &Controller{Router: router, Store: store, Daemon: spec, StopDaemon: func(stopCtx context.Context) error { return stopProduct(stopCtx) }}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer stopCancel()
		if err := controller.Stop(stopCtx); err != nil {
			t.Error(err)
		}
	})
	waitFor := func(label string, ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		var reconcileErr error
		for {
			reconcileErr = controller.Reconcile(ctx)
			if reconcileErr == nil && ready() {
				return
			}
			if time.Now().After(deadline) || ctx.Err() != nil {
				status, _ := store.ReadObjectStatus(api.NetAPIVersion, "BGPRouter", "edge")
				raw, _ := json.Marshal(status)
				t.Logf("actual IPv4 routes: %s", run("ip", "-4", "-d", "route", "show", "table", "main", "proto", "186"))
				t.Logf("actual IPv6 routes: %s", run("ip", "-6", "-d", "route", "show", "table", "main", "proto", "186"))
				if handle, err := bgpNetlinkHandle(ctx); err == nil {
					routes, readErr := handle.RouteList(nil, syscall.AF_UNSPEC)
					for _, route := range routes {
						if int(route.Protocol) == bgpRouteProtocol {
							t.Logf("actual BGP netlink route: %#v", route)
						}
					}
					if readErr != nil {
						t.Logf("native diagnostic read: %v", readErr)
					}
					handle.Delete()
				}
				t.Logf("independent FRR summary: %s", peerVTY("show bgp summary json"))
				t.Fatalf("%s did not become ready: reconcile=%v status=%s", label, reconcileErr, raw)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	ribHas := func(prefix string) bool {
		wanted, err := netip.ParsePrefix(prefix)
		if err != nil {
			return false
		}
		controller.mu.Lock()
		defer controller.mu.Unlock()
		if controller.Server == nil {
			t.Fatal("actual BGP source observation is unavailable")
		}
		found := false
		err = controller.Server.ListPath(ctx, &gobgpapi.ListPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Family: familyForPrefix(wanted)}, func(destination *gobgpapi.Destination) {
			if destination.Prefix != prefix {
				return
			}
			for _, path := range destination.Paths {
				if path.Best && !path.IsNexthopInvalid && !path.IsWithdraw {
					found = true
				}
			}
		})
		if err != nil {
			t.Fatalf("actual BGP source observation failed: %v", err)
		}
		return found
	}
	nativeHas := func(prefix string) bool {
		snapshot, err := (netlinkFIBBackend{}).Snapshot(ctx)
		if err != nil {
			t.Fatalf("actual native route observation failed: %v", err)
		}
		for _, route := range snapshot.Routes {
			if route.Route.Prefix == prefix && route.Route.Protocol == bgpRouteProtocol && route.Exact {
				return true
			}
		}
		return false
	}
	waitFor("independent IPv4 route in RIB and acknowledged kernel FIB", func() bool { return ribHas("198.19.40.1/32") && nativeHas("198.19.40.1/32") })
	controller.Start(ctx)
	t.Logf("independent IPv4 peer communication: %s", strings.TrimSpace(string(run("ping", "-n", "-c", "1", "-W", "2", "198.19.40.1"))))
	t.Logf("actual IPv4 BGP kernel routes: %s", strings.TrimSpace(string(run("ip", "-4", "route", "show", "table", "main", "proto", "186"))))
	waitFor("independent IPv6 route in live RIB", func() bool { return ribHas("2001:db8:40::1/128") })
	t.Log("independent IPv6 prefix is present, valid and selected in the actual routerd-bgp RIB")
	controller.mu.Lock()
	err = controller.Server.ListPath(ctx, &gobgpapi.ListPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Family: ipv6Family()}, func(destination *gobgpapi.Destination) {
		for _, path := range destination.Paths {
			t.Logf("actual IPv6 BGP path: prefix=%s next_hop=%s best=%t", destination.Prefix, pathNextHop(path), path.Best)
		}
	})
	controller.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	waitFor("independent IPv6 route in acknowledged kernel FIB", func() bool { return nativeHas("2001:db8:40::1/128") })
	t.Logf("independent IPv6 peer communication: %s", strings.TrimSpace(string(run("ping", "-6", "-n", "-c", "1", "-W", "2", "2001:db8:40::1"))))
	dual := *router
	dual.Spec.Resources = append([]api.Resource(nil), router.Spec.Resources...)
	global, _ := dual.Spec.Resources[0].BGPRouterSpec()
	global.ImportPolicy = api.BGPImportPolicySpec{AllowedPrefixes: []string{"198.19.40.1/32", "2001:db8:40::1/128"}, NextHopRewrite: "unchanged"}
	dual.Spec.Resources[0].Spec = global
	for i := 1; i < len(dual.Spec.Resources); i++ {
		peer, _ := dual.Spec.Resources[i].BGPPeerSpec()
		peer.ImportPolicy = api.BGPImportPolicySpec{}
		dual.Spec.Resources[i].Spec = peer
	}
	if err := controller.ReconcileRouter(ctx, &dual, store); err != nil {
		t.Logf("mixed global policy transition is pending: %v", err)
	}
	waitFor("mixed global policy with both acknowledged families", func() bool {
		return ribHas("198.19.40.1/32") && ribHas("2001:db8:40::1/128") && nativeHas("198.19.40.1/32") && nativeHas("2001:db8:40::1/128")
	})
	checkOwned := func() {
		t.Helper()
		snapshot, err := (netlinkFIBBackend{}).Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		journal, found, err := store.ReadBGPFIB(ctx, snapshot.KernelScope)
		if err != nil || !found || journal.Pending != nil || len(journal.Applied) != 2 {
			t.Fatalf("native BGP ownership was not fully acknowledged: %+v found=%t err=%v", journal, found, err)
		}
		for _, receipt := range journal.Applied {
			if !snapshot.exact(receipt) || receipt.Table != 254 || receipt.Protocol != bgpRouteProtocol || receipt.Metric != 200 || len(receipt.NextHops) != 1 {
				t.Fatalf("BGP journal differs from actual kernel identity: %+v", receipt)
			}
		}
	}
	checkBoth := func(label string) {
		t.Helper()
		waitFor(label, func() bool {
			return ribHas("198.19.40.1/32") && ribHas("2001:db8:40::1/128") && nativeHas("198.19.40.1/32") && nativeHas("2001:db8:40::1/128")
		})
		checkOwned()
		t.Logf("%s IPv4: %s", label, strings.TrimSpace(string(run("ping", "-n", "-c", "1", "-W", "2", "198.19.40.1"))))
		t.Logf("%s IPv6: %s", label, strings.TrimSpace(string(run("ping", "-6", "-n", "-c", "1", "-W", "2", "2001:db8:40::1"))))
	}
	checkBoth("mixed global policy forwarding")
	peerNetworks := func(withdraw bool) {
		command := "network "
		if withdraw {
			command = "no network "
		}
		peerVTY("configure terminal", "router bgp 64513", "address-family ipv4 unicast", command+"198.19.40.1/32", "exit-address-family", "address-family ipv6 unicast", command+"2001:db8:40::1/128", "exit-address-family")
	}
	checkWithdrawn := func(label string) {
		t.Helper()
		waitFor(label, func() bool {
			return !ribHas("198.19.40.1/32") && !ribHas("2001:db8:40::1/128") && !nativeHas("198.19.40.1/32") && !nativeHas("2001:db8:40::1/128")
		})
		scope, err := (netlinkFIBBackend{}).KernelScope(ctx)
		if err != nil {
			t.Fatal(err)
		}
		journal, found, err := store.ReadBGPFIB(ctx, scope)
		if err != nil || !found || journal.Pending != nil || len(journal.Applied) != 0 {
			t.Fatalf("withdrawal retained BGP ownership: %+v found=%t err=%v", journal, found, err)
		}
		for _, address := range []string{"198.19.40.1", "2001:db8:40::1"} {
			out, err := exec.CommandContext(ctx, "ping", "-n", "-c", "1", "-W", "1", address).CombinedOutput()
			if err == nil {
				t.Fatalf("withdrawn peer route still forwards to %s: %s", address, out)
			}
		}
	}
	peerNetworks(true)
	checkWithdrawn("FRR explicit IPv4/IPv6 withdrawal")
	run("ping", "-n", "-c", "1", "-W", "2", "198.18.0.2")
	controller.mu.Lock()
	established := 0
	err = controller.Server.ListPeer(ctx, &gobgpapi.ListPeerRequest{}, func(peer *gobgpapi.Peer) {
		if peer.GetState().GetSessionState() == gobgpapi.PeerState_SESSION_STATE_ESTABLISHED {
			established++
		}
	})
	controller.mu.Unlock()
	if err != nil || established != 2 {
		t.Fatalf("withdrawal test lost its underlying BGP sessions: established=%d err=%v", established, err)
	}
	peerNetworks(false)
	checkBoth("FRR readvertisement")
	if err := stopFRR(ctx); err != nil {
		t.Fatal(err)
	}
	checkWithdrawn("independent FRR process stop")
	stopFRR = start("frr-restarted", frrCommand())
	checkBoth("independent FRR restart")
	if err := stopProduct(ctx); err != nil {
		t.Fatal(err)
	}
	stopProduct = start("routerd-bgp-restarted", productCommand())
	checkBoth("production routerd-bgp restart from applied state")
	// Rebuild the controller against the same SQLite journal only after its
	// watcher and RPC connection acknowledge completion. The daemon stays up.
	if err := controller.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	scope, err := (netlinkFIBBackend{}).KernelScope(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before, _, err := store.ReadBGPFIB(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	controller = &Controller{Router: &dual, Store: store, Daemon: spec, StopDaemon: func(stopCtx context.Context) error { return stopProduct(stopCtx) }}
	checkBoth("controller and SQLite reopen")
	controller.Start(ctx)
	after, _, err := store.ReadBGPFIB(ctx, scope)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("reopen replaced the acknowledged kernel generation: before=%+v after=%+v err=%v", before, after, err)
	}
	// Exercise actual declared-resource removal: the existing owner records its
	// removal fence, stops/Waits the daemon, and withdraws exact journal tuples.
	if err := controller.ReconcileRouter(ctx, &api.Router{}, store); err != nil {
		t.Fatalf("BGP resource removal: %v", err)
	}
	if nativeHas("198.19.40.1/32") || nativeHas("2001:db8:40::1/128") {
		t.Fatal("BGP resource removal retained owned native routes")
	}
	journal, found, err := store.ReadBGPFIB(ctx, scope)
	if err != nil || !found || journal.Pending != nil || len(journal.Applied) != 0 {
		t.Fatalf("BGP removal retained ownership: %+v found=%t err=%v", journal, found, err)
	}
	if _, err := os.Stat(spec.StatePath); !os.IsNotExist(err) {
		t.Fatalf("BGP removal retained restart intent: %v", err)
	}
	if err := controller.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := stopFRR(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("independent IPv4/IPv6 FRR reception, exact kernel readback, forwarding, withdrawal, peer/daemon restart, SQLite reopen and declared-resource removal verified")
}
