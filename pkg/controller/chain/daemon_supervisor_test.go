// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestSuperviseClientDaemonsStartsDNSResolverWhenEnabled(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	useSupervisedDaemonMarkerTestRoot(t)
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	logPath := filepath.Join(dir, "dns.args")
	pidPath := filepath.Join(dir, "dns.pid")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(logPath) + "\nprintf '%s\\n' \"$$\" > " + shellQuote(pidPath) + "\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(binDir, "routerd-dns-resolver"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
		Metadata: api.ObjectMeta{Name: "lan-resolver"},
		Spec: api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{{
			Name: "lan", Addresses: []string{"127.0.0.1"}, Port: 53,
		}}},
	}}}}
	runner := &Runner{
		Router: router,
		Opts:   Options{SuperviseDNSResolvers: true},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.superviseClientDaemons(ctx, nil)

	var data []byte
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		if got, err := os.ReadFile(logPath); err == nil {
			data = got
			break
		}
	}
	cancel()
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read supervised daemon pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		t.Fatalf("parse supervised daemon pid %q: %v", pidData, err)
	}
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		if err := syscall.Kill(pid, 0); err != nil {
			break
		}
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("supervised daemon %d remained after context cancellation", pid)
	}
	if len(data) == 0 {
		t.Fatalf("routerd-dns-resolver was not started")
	}
	got := strings.Fields(string(data))
	for _, want := range []string{
		"daemon",
		"--resource", "lan-resolver",
		"--config-file", "/var/lib/routerd/dns-resolver/lan-resolver/config.json",
		"--socket", "/run/routerd/dns-resolver/lan-resolver.sock",
		"--state-file", "/var/lib/routerd/dns-resolver/lan-resolver/state.json",
		"--event-file", "/var/lib/routerd/dns-resolver/lan-resolver/events.jsonl",
	} {
		if !stringSliceContains(got, want) {
			t.Fatalf("routerd-dns-resolver args missing %q: %v", want, got)
		}
	}
}

func TestReconcileSupervisedDaemonSpecsStopsStaleDaemon(t *testing.T) {
	useSupervisedDaemonMarkerTestRoot(t)
	canceled := false
	runner := &Runner{
		clientDaemonStates: map[string]supervisedDaemonState{
			supervisedDaemonKey("routerd-dhcpv6-client", "stale-pd"): {
				Spec: supervisedDaemonSpec{
					ResourceName: "stale-pd",
					Binary:       "routerd-dhcpv6-client",
					Args:         []string{"daemon", "--resource", "stale-pd"},
				},
				Cancel: func() { canceled = true },
			},
		},
	}

	runner.reconcileSupervisedDaemonSpecs(context.Background(), nil, nil)

	if !canceled {
		t.Fatalf("stale supervised daemon was not canceled")
	}
	if len(runner.clientDaemonStates) != 0 {
		t.Fatalf("stale supervised daemon state remains: %#v", runner.clientDaemonStates)
	}
}

func TestRouterdDaemonCmdlineMatchesResourceExactly(t *testing.T) {
	if !routerdDaemonCmdlineMatches([]string{"/usr/local/sbin/routerd-dhcpv6-client", "daemon", "--resource", "wan-pd"}, "routerd-dhcpv6-client", "wan-pd") {
		t.Fatalf("expected split --resource form to match")
	}
	if !routerdDaemonCmdlineMatches([]string{"routerd-dhcpv6-client", "daemon", "--resource=wan-pd"}, "routerd-dhcpv6-client", "wan-pd") {
		t.Fatalf("expected equals --resource form to match")
	}
	if routerdDaemonCmdlineMatches([]string{"routerd-dhcpv6-client", "daemon", "--resource", "wan-pd-old"}, "routerd-dhcpv6-client", "wan-pd") {
		t.Fatalf("resource prefix must not match")
	}
	if routerdDaemonCmdlineMatches([]string{"routerd-dhcpv6-client", "once", "--resource", "wan-pd"}, "routerd-dhcpv6-client", "wan-pd") {
		t.Fatalf("non-daemon command must not match")
	}
}

func TestSupervisedDaemonCrashRestartAdoptsExactOwnedToken(t *testing.T) {
	root := useSupervisedDaemonMarkerTestRoot(t)
	spec := supervisedDaemonSpec{ResourceName: "wan", Binary: "routerd-dhcpv4-client", Args: []string{"daemon", "--resource", "wan", "--interface", "vtnet0"}}
	marker := supervisedDaemonMarker{Version: 1, Binary: spec.Binary, Resource: spec.ResourceName, SpecHash: supervisedDaemonSpecHash(spec), OwnerToken: "owned-token", PID: 4242}
	if err := writeSupervisedDaemonMarker(marker); err != nil {
		t.Fatal(err)
	}
	_ = root
	oldProcesses, oldReady := supervisedDaemonProcesses, supervisedDaemonSocketReady
	t.Cleanup(func() { supervisedDaemonProcesses, supervisedDaemonSocketReady = oldProcesses, oldReady })
	supervisedDaemonProcesses = func() []supervisedDaemonProcess {
		{
			return []supervisedDaemonProcess{{PID: 4242, Command: "/usr/local/sbin/routerd-dhcpv4-client daemon --resource wan --interface vtnet0 --supervisor-owner owned-token"}}
		}
	}
	supervisedDaemonSocketReady = func(string) bool { return true }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &Runner{}
	runner.reconcileSupervisedDaemonSpecs(ctx, nil, []supervisedDaemonSpec{spec})
	state, ok := runner.clientDaemonStates[supervisedDaemonKey(spec.Binary, spec.ResourceName)]
	if !ok || state.Spec.OwnerToken != marker.OwnerToken {
		t.Fatalf("crash restart state = %#v, want adopted exact marker token", runner.clientDaemonStates)
	}
	runner.reconcileSupervisedDaemonSpecs(ctx, nil, []supervisedDaemonSpec{spec})
	if got, ok := runner.clientDaemonStates[supervisedDaemonKey(spec.Binary, spec.ResourceName)]; !ok || got.Spec.OwnerToken != marker.OwnerToken {
		t.Fatalf("second reconcile stopped owned daemon: %#v", runner.clientDaemonStates)
	}
}

func TestSupervisedDaemonDeletedResourceStopsOwnedOrphanAndRemovesMarker(t *testing.T) {
	useSupervisedDaemonMarkerTestRoot(t)
	marker := supervisedDaemonMarker{Version: 1, Binary: "routerd-dhcpv6-client", Resource: "wan-pd", SpecHash: "hash", OwnerToken: "owned-token", PID: 4343}
	if err := writeSupervisedDaemonMarker(marker); err != nil {
		t.Fatal(err)
	}
	processes := []supervisedDaemonProcess{{PID: marker.PID, Command: "/usr/local/sbin/routerd-dhcpv6-client daemon --resource wan-pd --supervisor-owner owned-token"}}
	oldProcesses, oldSignal := supervisedDaemonProcesses, supervisedDaemonSignal
	t.Cleanup(func() { supervisedDaemonProcesses, supervisedDaemonSignal = oldProcesses, oldSignal })
	supervisedDaemonProcesses = func() []supervisedDaemonProcess { return processes }
	var signals []syscall.Signal
	supervisedDaemonSignal = func(pid int, sig syscall.Signal) error {
		signals = append(signals, sig)
		if pid == marker.PID && sig == syscall.SIGTERM {
			processes = nil
		}
		return nil
	}
	(&Runner{}).reconcileSupervisedDaemonSpecs(context.Background(), nil, nil)
	if len(signals) != 1 || signals[0] != syscall.SIGTERM {
		t.Fatalf("signals = %#v, want one SIGTERM", signals)
	}
	if _, err := os.Stat(supervisedDaemonMarkerPath(marker.Binary, marker.Resource)); !os.IsNotExist(err) {
		t.Fatalf("owned orphan marker remains: %v", err)
	}
}

func TestSupervisedDaemonChangedSpecStopsOwnedOrphanAndRemovesMarker(t *testing.T) {
	useSupervisedDaemonMarkerTestRoot(t)
	oldSpec := supervisedDaemonSpec{ResourceName: "wan", Binary: "routerd-dhcpv4-client", Args: []string{"daemon", "--resource", "wan", "--interface", "vtnet0"}}
	marker := supervisedDaemonMarker{Version: 1, Binary: oldSpec.Binary, Resource: oldSpec.ResourceName, SpecHash: supervisedDaemonSpecHash(oldSpec), OwnerToken: "owned-token", PID: 4353}
	if err := writeSupervisedDaemonMarker(marker); err != nil {
		t.Fatal(err)
	}
	processes := []supervisedDaemonProcess{{PID: marker.PID, Command: "/usr/local/sbin/routerd-dhcpv4-client daemon --resource wan --interface vtnet0 --supervisor-owner owned-token"}}
	oldProcesses, oldReady, oldSignal := supervisedDaemonProcesses, supervisedDaemonSocketReady, supervisedDaemonSignal
	t.Cleanup(func() {
		supervisedDaemonProcesses, supervisedDaemonSocketReady, supervisedDaemonSignal = oldProcesses, oldReady, oldSignal
	})
	supervisedDaemonProcesses = func() []supervisedDaemonProcess { return processes }
	supervisedDaemonSocketReady = func(string) bool { return true }
	var signals []syscall.Signal
	supervisedDaemonSignal = func(pid int, sig syscall.Signal) error {
		signals = append(signals, sig)
		if pid == marker.PID && sig == syscall.SIGTERM {
			processes = nil
		}
		return nil
	}
	changedSpec := oldSpec
	changedSpec.Args = []string{"daemon", "--resource", "wan", "--interface", "vtnet1"}
	(&Runner{}).reconcileSupervisedDaemonSpecs(context.Background(), nil, []supervisedDaemonSpec{changedSpec})
	if len(signals) != 1 || signals[0] != syscall.SIGTERM {
		t.Fatalf("signals = %#v, want one SIGTERM for changed desired spec", signals)
	}
	if _, err := os.Stat(supervisedDaemonMarkerPath(marker.Binary, marker.Resource)); !os.IsNotExist(err) {
		t.Fatalf("changed-spec owned orphan marker remains: %v", err)
	}
}

func TestSupervisedDaemonMarkerlessReadySocketIsForeign(t *testing.T) {
	useSupervisedDaemonMarkerTestRoot(t)
	spec := supervisedDaemonSpec{ResourceName: "wan", Binary: "routerd-pppoe-client", Args: []string{"daemon", "--resource", "wan"}}
	oldProcesses, oldReady, oldSignal := supervisedDaemonProcesses, supervisedDaemonSocketReady, supervisedDaemonSignal
	t.Cleanup(func() {
		supervisedDaemonProcesses, supervisedDaemonSocketReady, supervisedDaemonSignal = oldProcesses, oldReady, oldSignal
	})
	supervisedDaemonProcesses = func() []supervisedDaemonProcess {
		return []supervisedDaemonProcess{{PID: 4444, Command: "/usr/local/sbin/routerd-pppoe-client daemon --resource wan --supervisor-owner foreign-token"}}
	}
	supervisedDaemonSocketReady = func(string) bool { return true }
	supervisedDaemonSignal = func(int, syscall.Signal) error { t.Fatal("markerless foreign daemon must not be signaled"); return nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &Runner{}
	runner.reconcileSupervisedDaemonSpecs(ctx, nil, []supervisedDaemonSpec{spec})
	if len(runner.clientDaemonStates) != 0 {
		t.Fatalf("markerless ready socket was adopted: %#v", runner.clientDaemonStates)
	}
	if _, err := os.Stat(supervisedDaemonMarkerPath(spec.Binary, spec.ResourceName)); !os.IsNotExist(err) {
		t.Fatalf("markerless foreign socket gained ownership marker: %v", err)
	}
}

func TestRouterdDaemonCommandMatchesExactBinaryResourceAndToken(t *testing.T) {
	good := "/usr/local/sbin/routerd-dhcpv4-client daemon --resource wan --supervisor-owner exact"
	if !routerdDaemonCommandMatches(good, "routerd-dhcpv4-client", "wan", "exact") {
		t.Fatal("exact owned command did not match")
	}
	for _, command := range []string{
		"/usr/local/sbin/routerd-dhcpv4-client-old daemon --resource wan --supervisor-owner exact",
		"/usr/local/sbin/routerd-dhcpv4-client once --resource wan --supervisor-owner exact",
		"/usr/local/sbin/routerd-dhcpv4-client daemon --resource wan-old --supervisor-owner exact",
		"/usr/local/sbin/routerd-dhcpv4-client daemon --resource wan --supervisor-owner exact-old",
	} {
		if routerdDaemonCommandMatches(command, "routerd-dhcpv4-client", "wan", "exact") {
			t.Fatalf("near-name command matched ownership: %q", command)
		}
	}
}

func TestMatchingOwnedSupervisedDaemonPIDRejectsPIDReuse(t *testing.T) {
	old := supervisedDaemonProcesses
	t.Cleanup(func() { supervisedDaemonProcesses = old })
	marker := supervisedDaemonMarker{Binary: "routerd-dhcpv4-client", Resource: "wan", OwnerToken: "exact", PID: 5151}
	supervisedDaemonProcesses = func() []supervisedDaemonProcess {
		return []supervisedDaemonProcess{
			{PID: 5150, Command: "/usr/local/sbin/routerd-dhcpv4-client daemon --resource wan --supervisor-owner exact"},
			{PID: 5151, Command: "/usr/local/sbin/routerd-dhcpv4-client daemon --resource wan --supervisor-owner exact"},
		}
	}
	if got := matchingOwnedSupervisedDaemonPIDs(marker); len(got) != 1 || got[0] != 5151 {
		t.Fatalf("owned PIDs = %#v, want only recorded PID", got)
	}
}

func useSupervisedDaemonMarkerTestRoot(t *testing.T) string {
	t.Helper()
	old := supervisedDaemonMarkerRoot
	root := filepath.Join(t.TempDir(), "supervised-daemons")
	supervisedDaemonMarkerRoot = func() string { return root }
	t.Cleanup(func() { supervisedDaemonMarkerRoot = old })
	return root
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
