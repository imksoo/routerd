// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package netnssam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/config"
)

const (
	enableEnv = "ROUTERD_NETNS_INTEGRATION"
	keepEnv   = "ROUTERD_NETNS_KEEP"
)

type lab struct {
	t             *testing.T
	ctx           context.Context
	name          string
	workDir       string
	repoDir       string
	binDir        string
	bfd           bool
	pprof         bool
	nodes         []node
	bridges       []string
	fabrics       []string
	procs         []*exec.Cmd
	bgp           map[string]*exec.Cmd
	routerd       map[string]*exec.Cmd
	routes        map[string]string
	routeMu      sync.Mutex
	fabricCancel context.CancelFunc
	fabricDone   chan struct{}
	lockFile     *os.File
}

type node struct {
	Name      string
	Site      string
	Role      string
	SiteCIDR  string
	Transport string
	Router    bool
}

type labOptions struct {
	EnableBFD   bool
	EnablePprof bool
}

type cmdError struct {
	Command string
	Output  string
	Err     error
}

func (e cmdError) Error() string {
	return fmt.Sprintf("%s: %v\n%s", e.Command, e.Err, e.Output)
}

func TestNetNSSAMRealRouterdTopologySmoke(t *testing.T) {
	l := setupConvergedLab(t, 12*time.Minute)
	l.AssertClientMatrix(6 * time.Minute)
}

func TestLeafFailover(t *testing.T) {
	l := setupConvergedLab(t, 16*time.Minute)
	failed, survivor, before := l.leafFailoverPair("aws-leaf")
	t.Logf("scenario pair: failed=%s survivor=%s captures=%v", failed, survivor, before)
	l.KillRouterNode(failed)
	l.AssertCapturesPresent(survivor, len(before), 6*time.Minute)
	l.AssertFabricRoutesSynced(before, 6*time.Minute)
	l.AssertClientMatrix(8 * time.Minute)
}

func TestLeafRejoinNoPreempt(t *testing.T) {
	l := setupConvergedLab(t, 18*time.Minute)
	failed, survivor, before := l.leafFailoverPair("aws-leaf")
	t.Logf("scenario pair: failed=%s survivor=%s captures=%v", failed, survivor, before)
	l.KillRouterNode(failed)
	l.AssertCapturesPresent(survivor, len(before), 6*time.Minute)
	l.AssertFabricRoutesSynced(before, 6*time.Minute)
	l.StartRouterNode(failed)
	l.AssertRouterdStatusReady(3 * time.Minute)
	l.AssertBGPEstablished(5 * time.Minute)
	l.AssertMobilityReady(6 * time.Minute)
	l.AssertCapturesAbsent(failed, 4*time.Minute)
	l.AssertCapturesPresent(survivor, len(before), 4*time.Minute)
	l.AssertFabricRoutesSynced(before, 4*time.Minute)
	l.AssertClientMatrix(8 * time.Minute)
}

func TestForcedRebalance(t *testing.T) {
	l := setupConvergedLab(t, 20*time.Minute)
	failed, survivor, before := l.leafFailoverPair("aws-leaf")
	t.Logf("scenario pair: failed=%s survivor=%s captures=%v", failed, survivor, before)
	l.KillRouterNode(failed)
	l.AssertCapturesPresent(survivor, len(before), 6*time.Minute)
	l.AssertFabricRoutesSynced(before, 6*time.Minute)
	l.StartRouterNode(failed)
	l.AssertRouterdStatusReady(3 * time.Minute)
	l.AssertBGPEstablished(5 * time.Minute)
	l.AssertMobilityReady(6 * time.Minute)
	l.AssertCapturesAbsent(failed, 4*time.Minute)
	l.RequestCaptureRebalance(survivor, "forced-rebalance-test")
	l.AssertCapturesPresent(failed, 1, 8*time.Minute)
	l.AssertCapturesPresent(survivor, 1, 8*time.Minute)
	l.AssertClientMatrix(8 * time.Minute)
}

func TestGracefulDrain(t *testing.T) {
	l := setupConvergedLab(t, 18*time.Minute)
	drained, survivor, before := l.leafFailoverPair("aws-leaf")
	t.Logf("scenario pair: drained=%s survivor=%s captures=%v", drained, survivor, before)
	stopMatrix := l.StartClientMatrixMonitor(5*time.Second, 3*time.Minute)
	l.GracefullyStopRouterd(drained, 3*time.Minute)
	l.AssertCaptureAddressesPresent(survivor, before, 6*time.Minute)
	l.AssertFabricRoutesSynced(before, 6*time.Minute)
	stopMatrix()
	l.AssertClientMatrix(6 * time.Minute)
}

func TestBFDLivenessDetection(t *testing.T) {
	l := setupConvergedLabWithOptions(t, 20*time.Minute, labOptions{EnableBFD: true})
	failed, survivor, before := l.leafFailoverPair("aws-leaf")
	t.Logf("scenario pair: failed=%s survivor=%s captures=%v", failed, survivor, before)
	l.netns(failed, "ip", "link", "set", "eth0", "down")
	l.AssertBFDPeerState(survivor, bfdResourceName(survivor, failed), "Down", 4*time.Minute)
	l.AssertCapturesPresent(survivor, len(before), 8*time.Minute)
	l.AssertFabricRoutesSynced(before, 8*time.Minute)
	l.AssertClientMatrix(8 * time.Minute)
}

func setupConvergedLab(t *testing.T, timeout time.Duration) *lab {
	return setupConvergedLabWithOptions(t, timeout, labOptions{})
}

func setupConvergedLabWithOptions(t *testing.T, timeout time.Duration, opts labOptions) *lab {
	t.Helper()
	requireNetNS(t)
	if opts.EnableBFD {
		for _, name := range []string{"vtysh", "bfdd"} {
			if _, err := exec.LookPath(name); err != nil {
				t.Skipf("%s is required for BFD netns integration test: %v", name, err)
			}
		}
	}
	lockF, err := os.OpenFile("/run/lock/routerd-netnssam.lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open netns test lock: %v", err)
	}
	if err := syscall.Flock(int(lockF.Fd()), syscall.LOCK_EX); err != nil {
		lockF.Close()
		t.Fatalf("acquire netns test lock: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	l := newLab(t, ctx)
	l.bfd = opts.EnableBFD
	l.pprof = opts.EnablePprof
	l.lockFile = lockF
	t.Cleanup(func() {
		cancel()
		l.Cleanup()
	})

	l.BuildBinaries()
	l.CreateTopology(defaultTopology())
	l.StartRouterProcesses()
	l.StartFabricRouteReconciler()
	l.AssertUnderlayReachability()
	l.AssertRouterdStatusReady(2 * time.Minute)
	l.AssertWireGuardReachability(2 * time.Minute)
	l.AssertBGPEstablished(4 * time.Minute)
	l.AssertMobilityReady(6 * time.Minute)
	l.AssertClientMatrix(6 * time.Minute)
	return l
}

func requireNetNS(t *testing.T) {
	t.Helper()
	if os.Getenv(enableEnv) != "1" {
		t.Skipf("set %s=1 to run netns integration tests", enableEnv)
	}
	if runtime.GOOS != "linux" {
		t.Skip("network namespace tests require Linux")
	}
	if os.Geteuid() != 0 {
		t.Skip("network namespace tests require root")
	}
	for _, name := range []string{"ip", "timeout", "wg"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s is required: %v", name, err)
		}
	}
}

func newLab(t *testing.T, ctx context.Context) *lab {
	t.Helper()
	workDir := t.TempDir()
	if os.Getenv(keepEnv) == "1" {
		var err error
		workDir, err = os.MkdirTemp("", "routerd-netns-622-*")
		if err != nil {
			t.Fatalf("create persistent netns work dir: %v", err)
		}
	}
	const nameSpace = 36 * 36 * 36 * 36 * 36
	name := "r622" + strconv.FormatInt(time.Now().UnixNano()%nameSpace, 36)
	return &lab{
		t:       t,
		ctx:     ctx,
		name:    shortName(name),
		workDir: workDir,
		repoDir: repoRoot(t),
		binDir:  filepath.Join(workDir, "bin"),
		bgp:     map[string]*exec.Cmd{},
		routerd: map[string]*exec.Cmd{},
		routes:  map[string]string{},
	}
}

func defaultTopology() []node {
	return []node{
		{Name: "aws-rr-a", Site: "aws-rr", Role: "rr", SiteCIDR: "10.77.10.10/24", Transport: "172.31.0.10/24", Router: true},
		{Name: "aws-rr-b", Site: "aws-rr", Role: "rr", SiteCIDR: "10.77.10.11/24", Transport: "172.31.0.11/24", Router: true},
		{Name: "aws-leaf-a", Site: "aws-leaf", Role: "leaf", SiteCIDR: "10.77.60.4/24", Transport: "172.31.0.12/24", Router: true},
		{Name: "aws-leaf-b", Site: "aws-leaf", Role: "leaf", SiteCIDR: "10.77.60.5/24", Transport: "172.31.0.13/24", Router: true},
		{Name: "aws-client-a", Site: "aws-leaf", Role: "client", SiteCIDR: "10.77.60.11/24"},
		{Name: "aws-client-b", Site: "aws-leaf", Role: "client", SiteCIDR: "10.77.60.16/24"},
		{Name: "azure-leaf-a", Site: "azure-leaf", Role: "leaf", SiteCIDR: "10.77.60.14/24", Transport: "172.31.0.14/24", Router: true},
		{Name: "azure-leaf-b", Site: "azure-leaf", Role: "leaf", SiteCIDR: "10.77.60.21/24", Transport: "172.31.0.15/24", Router: true},
		{Name: "azure-client-a", Site: "azure-leaf", Role: "client", SiteCIDR: "10.77.60.12/24"},
		{Name: "azure-client-b", Site: "azure-leaf", Role: "client", SiteCIDR: "10.77.60.17/24"},
		{Name: "oci-leaf-a", Site: "oci-leaf", Role: "leaf", SiteCIDR: "10.77.60.24/24", Transport: "172.31.0.16/24", Router: true},
		{Name: "oci-leaf-b", Site: "oci-leaf", Role: "leaf", SiteCIDR: "10.77.60.25/24", Transport: "172.31.0.17/24", Router: true},
		{Name: "oci-client-a", Site: "oci-leaf", Role: "client", SiteCIDR: "10.77.60.13/24"},
		{Name: "oci-client-b", Site: "oci-leaf", Role: "client", SiteCIDR: "10.77.60.18/24"},
		{Name: "pve-leaf-a", Site: "pve-leaf", Role: "leaf", SiteCIDR: "10.77.60.30/24", Transport: "172.31.0.18/24", Router: true},
		{Name: "pve-leaf-b", Site: "pve-leaf", Role: "leaf", SiteCIDR: "10.77.60.35/24", Transport: "172.31.0.19/24", Router: true},
		{Name: "pve-client-a", Site: "pve-leaf", Role: "client", SiteCIDR: "10.77.60.15/24"},
		{Name: "pve-client-b", Site: "pve-leaf", Role: "client", SiteCIDR: "10.77.60.19/24"},
	}
}

func (l *lab) BuildBinaries() {
	l.t.Helper()
	if err := os.MkdirAll(l.binDir, 0o755); err != nil {
		l.t.Fatalf("create bin dir: %v", err)
	}
	for _, target := range []struct {
		Name string
		Pkg  string
	}{
		{Name: "routerd", Pkg: "./cmd/routerd"},
		{Name: "routerctl", Pkg: "./cmd/routerctl"},
		{Name: "routerd-bgp", Pkg: "./cmd/routerd-bgp"},
		{Name: "netns-provider-executor", Pkg: "./examples/plugins/netns-provider-executor"},
		{Name: "netns-provider-inventory", Pkg: "./examples/plugins/netns-provider-inventory"},
	} {
		l.run(l.repoDir, "go", "build", "-o", filepath.Join(l.binDir, target.Name), target.Pkg)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func (l *lab) CreateTopology(nodes []node) {
	l.t.Helper()
	l.nodes = nodes
	for _, n := range nodes {
		l.run("", "ip", "netns", "add", l.ns(n.Name))
		l.run("", "ip", "-n", l.ns(n.Name), "link", "set", "lo", "up")
	}
	l.createBridge(l.bridge("transport"))
	siteSeen := map[string]bool{}
	for _, n := range nodes {
		if !siteSeen[n.Site] {
			l.createFabric(n.Site)
			l.createBridge(l.siteClientBridge(n.Site))
			l.createBridge(l.siteLeafBridge(n.Site))
			l.attachNamespace(l.fabricNS(n.Site), "fabric-"+n.Site, "eth-client", l.siteClientBridge(n.Site))
			l.attachNamespace(l.fabricNS(n.Site), "fabric-"+n.Site, "eth-leaf", l.siteLeafBridge(n.Site))
			l.netnsByName(l.fabricNS(n.Site), "ip", "addr", "add", cidrWithIP(siteFabricClientGateway(n.Site, nodes), n.SiteCIDR), "dev", "eth-client")
			l.netnsByName(l.fabricNS(n.Site), "ip", "addr", "add", siteFabricLeafGateway(n.Site, nodes)+"/32", "dev", "eth-leaf")
			l.netnsByName(l.fabricNS(n.Site), "sysctl", "-w", "net.ipv4.ip_forward=1")
			siteSeen[n.Site] = true
		}
	}
	for _, n := range nodes {
		siteBridge := l.siteClientBridge(n.Site)
		if n.Router {
			siteBridge = l.siteLeafBridge(n.Site)
		}
		l.attach(n.Name, "eth1", siteBridge)
		l.netns(n.Name, "ip", "addr", "add", n.SiteCIDR, "dev", "eth1")
		l.netns(n.Name, "ip", "link", "set", "eth1", "up")
		if n.Router {
			l.attach(n.Name, "eth0", l.bridge("transport"))
			l.netns(n.Name, "ip", "addr", "add", n.Transport, "dev", "eth0")
			l.netns(n.Name, "ip", "link", "set", "eth0", "up")
			l.netns(n.Name, "sysctl", "-w", "net.ipv4.ip_forward=1")
		}
	}
	for _, n := range nodes {
		if n.Router {
			l.netnsByName(l.fabricNS(n.Site), "ip", "route", "replace", ipOnly(n.SiteCIDR)+"/32", "dev", "eth-leaf")
			for _, client := range nodes {
				if client.Role == "client" && client.Site == n.Site {
					l.netns(n.Name, "ip", "route", "replace", ipOnly(client.SiteCIDR)+"/32", "via", siteFabricLeafGateway(n.Site, nodes), "dev", "eth1")
				}
			}
		}
	}
	for _, n := range nodes {
		if n.Role == "client" {
			l.netns(n.Name, "ip", "route", "replace", "default", "via", siteFabricClientGateway(n.Site, nodes))
			for _, remote := range nodes {
				if remote.Role != "client" || remote.Site == n.Site {
					continue
				}
				l.netns(n.Name, "ip", "route", "replace", ipOnly(remote.SiteCIDR)+"/32", "via", siteFabricClientGateway(n.Site, nodes))
			}
		}
	}
	l.writeRouterConfigs()
}

func (l *lab) StartRouterProcesses() {
	l.t.Helper()
	for _, n := range l.routerNodes() {
		l.StartRouterNode(n.Name)
	}
}

func (l *lab) StartRouterNode(nodeName string) {
	l.t.Helper()
	n, ok := l.nodeByName(nodeName)
	if !ok || !n.Router {
		l.t.Fatalf("unknown router node %q", nodeName)
	}
	runtimeDir := l.nodeDir(n.Name, "run")
	stateDir := l.nodeDir(n.Name, "state")
	if err := os.MkdirAll(filepath.Join(runtimeDir, "bgp"), 0o755); err != nil {
		l.t.Fatalf("create runtime dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "bgp"), 0o755); err != nil {
		l.t.Fatalf("create state dir: %v", err)
	}
	bgp := l.startNetNS(n.Name, filepath.Join(l.binDir, "routerd-bgp"),
		"daemon",
		"--socket", filepath.Join(runtimeDir, "bgp", "gobgp.sock"),
		"--control-socket", filepath.Join(runtimeDir, "bgp", "control.sock"),
		"--state-file", filepath.Join(stateDir, "bgp", "applied.json"),
	)
	l.procs = append(l.procs, bgp)
	l.bgp[n.Name] = bgp
	controllers := "link,sam-transport,tunnel,wireguard,ipv4-static-address,ipv4-route,hybrid-route,sam,mobility-discovery,mobility,mobility-shard,provider-action-execution,bgp"
	if l.bfd {
		controllers += ",bfd"
	}
	args := []string{
		"serve",
		"--config", l.nodeDir(n.Name, "router.yaml"),
		"--socket", filepath.Join(runtimeDir, "control.sock"),
		"--status-socket", filepath.Join(runtimeDir, "status.sock"),
		"--state-file", filepath.Join(stateDir, "routerd.db"),
		"--ledger-file", filepath.Join(stateDir, "ledger.db"),
		"--bgp-socket", filepath.Join(runtimeDir, "bgp", "gobgp.sock"),
		"--bgp-control-socket", filepath.Join(runtimeDir, "bgp", "control.sock"),
		"--bgp-state-file", filepath.Join(stateDir, "bgp", "applied.json"),
		"--controllers", controllers,
		"--apply-interval", "5s",
	}
	if l.pprof {
		args = append(args, "--pprof-addr", "0.0.0.0:6060")
	}
	routerd := l.startNetNS(n.Name, filepath.Join(l.binDir, "routerd"), args...)
	l.procs = append(l.procs, routerd)
	l.routerd[n.Name] = routerd
}

func (l *lab) KillRouterNode(nodeName string) {
	l.t.Helper()
	for _, cmd := range []*exec.Cmd{l.routerd[nodeName], l.bgp[nodeName]} {
		if cmd == nil || cmd.Process == nil {
			continue
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	}
	delete(l.routerd, nodeName)
	delete(l.bgp, nodeName)
}

func (l *lab) GracefullyStopRouterd(nodeName string, timeout time.Duration) {
	l.t.Helper()
	cmd := l.routerd[nodeName]
	if cmd == nil || cmd.Process == nil {
		l.t.Fatalf("routerd for %s is not running", nodeName)
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		l.t.Fatalf("SIGTERM routerd %s: %v", nodeName, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			l.t.Logf("routerd %s exited after graceful stop: %v", nodeName, err)
		}
	case <-time.After(timeout):
		l.dumpLogs()
		l.t.Fatalf("routerd %s did not exit within %s", nodeName, timeout)
	}
	delete(l.routerd, nodeName)
}

func (l *lab) RequestCaptureRebalance(nodeName, reason string) {
	l.t.Helper()
	statePath := filepath.Join(l.nodeDir(nodeName, "state"), "routerd.db")
	l.run("", filepath.Join(l.binDir, "routerctl"),
		"mobility", "rebalance-captures",
		"--pool", "cloudedge",
		"--state-file", statePath,
		"--by", "netns-test",
		"--reason", reason,
		"-o", "json",
	)
}

func (l *lab) StartFabricRouteReconciler() {
	l.t.Helper()
	ctx, cancel := context.WithCancel(l.ctx)
	l.fabricCancel = cancel
	l.fabricDone = make(chan struct{})
	go func() {
		defer close(l.fabricDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			l.reconcileFabricRoutes()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (l *lab) stopFabricRouteReconciler() {
	if l.fabricCancel != nil {
		l.fabricCancel()
	}
	if l.fabricDone != nil {
		<-l.fabricDone
	}
}

func (l *lab) reconcileFabricRoutes() {
	if l.ctx.Err() != nil {
		return
	}
	captureHolders := map[string]node{}
	for _, n := range l.leafNodes() {
		out, err := l.netnsOutput(n.Name, "ip", "-o", "-4", "addr", "show", "dev", "eth1")
		if err != nil {
			continue
		}
		for _, address := range parseIPv4HostAddresses(out) {
			if address == ipOnly(n.SiteCIDR)+"/32" {
				continue
			}
			l.removeCaptureKernelRoutes(n.Name, address)
			captureHolders[address] = n
		}
	}
	clientOwners := l.clientAddressOwners()
	desired := map[string]string{}
	for address, ownerSite := range clientOwners {
		holder, captured := captureHolders[address]
		for _, site := range l.leafSites() {
			via := l.fabricClientNextHop(site, address, ownerSite, holder, captured)
			if via == "" {
				continue
			}
			desired[site+"|"+address] = via
		}
	}

	l.routeMu.Lock()
	defer l.routeMu.Unlock()
	for key, via := range l.routes {
		if _, ok := desired[key]; ok {
			continue
		}
		site, address, ok := strings.Cut(key, "|")
		if !ok {
			continue
		}
		if clientOwners[address] != "" && site != clientOwners[address] {
			desired[key] = via
		}
	}
	for key, via := range desired {
		site, address, ok := strings.Cut(key, "|")
		if !ok {
			continue
		}
		err := l.netnsErrWithinByName(2*time.Second, l.fabricNS(site), "ip", "route", "replace", address, "via", via, "dev", "eth-leaf")
		if err == nil {
			l.routes[key] = via
			continue
		}
		if l.ctx.Err() != nil {
			return
		}
		l.t.Logf("%s: failed to sync fabric route %s via %s: %v", l.fabricNS(site), address, via, err)
	}
	for key := range l.routes {
		if _, ok := desired[key]; ok {
			continue
		}
		site, address, ok := strings.Cut(key, "|")
		if !ok {
			delete(l.routes, key)
			continue
		}
		err := l.netnsErrWithinByName(2*time.Second, l.fabricNS(site), "ip", "route", "del", address)
		if err == nil || strings.Contains(err.Error(), "No such process") || strings.Contains(err.Error(), "No such file or directory") {
			delete(l.routes, key)
		}
	}
}

func (l *lab) clientAddressOwners() map[string]string {
	owners := map[string]string{}
	for _, n := range l.clientNodes() {
		owners[ipOnly(n.SiteCIDR)+"/32"] = n.Site
	}
	return owners
}

func (l *lab) leafSites() []string {
	seen := map[string]bool{}
	var sites []string
	for _, n := range l.leafNodes() {
		if seen[n.Site] {
			continue
		}
		seen[n.Site] = true
		sites = append(sites, n.Site)
	}
	return sites
}

func (l *lab) fabricClientNextHop(site, address, ownerSite string, holder node, captured bool) string {
	if site == ownerSite {
		return ""
	}
	for _, candidate := range l.leafNodes() {
		if candidate.Site != site {
			continue
		}
		if !l.leafHasPrimary(candidate) {
			continue
		}
		if l.leafHasHostRoute(candidate, address) {
			return ipOnly(candidate.SiteCIDR)
		}
	}
	if captured && holder.Site == site && l.leafHasPrimary(holder) && l.leafHasHostRoute(holder, address) {
		return ipOnly(holder.SiteCIDR)
	}
	return ""
}

func (l *lab) leafHasPrimary(n node) bool {
	out, err := l.netnsOutput(n.Name, "ip", "-o", "-4", "addr", "show", "dev", "eth1")
	return err == nil && strings.Contains(out, "inet "+n.SiteCIDR)
}

func (l *lab) leafHasHostRoute(n node, address string) bool {
	out, err := l.netnsOutput(n.Name, "ip", "-4", "route", "show", address)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == address || fields[0] == ipOnly(address) {
			return true
		}
	}
	return false
}

func (l *lab) removeCaptureKernelRoutes(nodeName, address string) {
	for _, args := range [][]string{
		{"ip", "route", "del", "table", "local", "local", ipOnly(address)},
		{"ip", "route", "del", "table", "local", "local", ipOnly(address), "dev", "eth1"},
		{"ip", "route", "del", "table", "local", "local", address},
		{"ip", "route", "del", "table", "local", "local", address, "dev", "eth1"},
		{"ip", "route", "del", "table", "local", address, "dev", "eth1"},
		{"ip", "route", "del", "table", "local", ipOnly(address), "dev", "eth1"},
		{"ip", "route", "del", address, "dev", "eth1", "scope", "link", "metric", "1"},
		{"ip", "route", "del", address, "dev", "eth1", "scope", "link"},
	} {
		err := l.netnsErrWithin(2*time.Second, nodeName, args...)
		if err == nil || strings.Contains(err.Error(), "No such process") || strings.Contains(err.Error(), "No such file or directory") {
			continue
		}
		l.t.Logf("%s: failed to remove capture kernel route %s (%s): %v", nodeName, address, strings.Join(args, " "), err)
	}
}

func (l *lab) AssertUnderlayReachability() {
	l.t.Helper()
	for _, from := range l.routerNodes() {
		for _, to := range l.routerNodes() {
			if from.Name == to.Name {
				continue
			}
			l.netns(from.Name, "timeout", "3s", "ping", "-c", "1", "-W", "2", ipOnly(to.Transport))
		}
	}
}

func (l *lab) AssertRouterdStatusReady(timeout time.Duration) {
	l.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var pending []string
		for _, n := range l.routerNodes() {
			statusSocket := filepath.Join(l.nodeDir(n.Name, "run"), "status.sock")
			err := l.netnsErr(n.Name, filepath.Join(l.binDir, "routerctl"), "get", "status", "--socket", statusSocket, "-o", "json")
			if err != nil {
				pending = append(pending, n.Name+": "+err.Error())
			}
		}
		if len(pending) == 0 {
			return
		}
		if time.Now().After(deadline) {
			l.dumpLogs()
			l.t.Fatalf("routerd status did not become ready:\n%s", strings.Join(pending, "\n"))
		}
		time.Sleep(2 * time.Second)
	}
}

func (l *lab) AssertWireGuardReachability(timeout time.Duration) {
	l.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var pending []string
		for _, from := range l.routerNodes() {
			for _, to := range l.routerNodes() {
				if from.Name == to.Name {
					continue
				}
				target := fmt.Sprintf("10.253.0.%d", nodeIndex(to.Name)+10)
				if err := l.netnsErr(from.Name, "timeout", "3s", "ping", "-c", "1", "-W", "1", target); err != nil {
					pending = append(pending, from.Name+" -> "+target+": "+err.Error())
				}
			}
		}
		if len(pending) == 0 {
			return
		}
		if time.Now().After(deadline) {
			l.dumpLogs()
			l.t.Fatalf("WireGuard reachability did not converge:\n%s", strings.Join(pending, "\n"))
		}
		time.Sleep(2 * time.Second)
	}
}

func (l *lab) AssertBGPEstablished(timeout time.Duration) {
	l.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var pending []string
		for _, n := range l.routerNodes() {
			wantPeers := l.rrCount()
			if n.Role == "rr" {
				wantPeers = len(l.routerNodes()) - 1
			}
			statusSocket := filepath.Join(l.nodeDir(n.Name, "run"), "status.sock")
			out, err := l.netnsOutput(n.Name, filepath.Join(l.binDir, "routerctl"), "get", "BGPRouter/mobility-bgp", "--socket", statusSocket, "-o", "json")
			if err != nil {
				pending = append(pending, n.Name+": "+err.Error())
				continue
			}
			established, ok := jsonNumberField([]byte(out), "establishedPeers")
			if !ok || established < wantPeers {
				pending = append(pending, fmt.Sprintf("%s: establishedPeers=%d ok=%v want >= %d", n.Name, established, ok, wantPeers))
			}
		}
		if len(pending) == 0 {
			return
		}
		if time.Now().After(deadline) {
			l.dumpLogs()
			l.t.Fatalf("BGP did not establish:\n%s", strings.Join(pending, "\n"))
		}
		time.Sleep(2 * time.Second)
	}
}

func (l *lab) AssertMobilityReady(timeout time.Duration) {
	l.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var pending []string
		for _, n := range l.nodes {
			if n.Role != "leaf" {
				continue
			}
			statusSocket := filepath.Join(l.nodeDir(n.Name, "run"), "status.sock")
			out, err := l.netnsOutput(n.Name, filepath.Join(l.binDir, "routerctl"), "get", "MobilityPool/cloudedge", "--socket", statusSocket, "-o", "json")
			if err != nil {
				pending = append(pending, n.Name+": "+err.Error())
				continue
			}
			phase, phaseOK := jsonStringField([]byte(out), "phase")
			providerPhase, providerOK := jsonStringField([]byte(out), "providerActionPhase")
			conflicts, _ := jsonNumberField([]byte(out), "ownershipResolverConflictCount")
			if !phaseOK || phase != "Ready" || !providerOK || providerPhase != "OK" || conflicts != 0 {
				pending = append(pending, fmt.Sprintf("%s: phase=%q providerActionPhase=%q conflicts=%d", n.Name, phase, providerPhase, conflicts))
			}
		}
		if len(pending) == 0 {
			return
		}
		if time.Now().After(deadline) {
			l.dumpLogs()
			l.t.Fatalf("MobilityPool/cloudedge did not become ready:\n%s", strings.Join(pending, "\n"))
		}
		time.Sleep(2 * time.Second)
	}
}

func (l *lab) AssertClientMatrix(timeout time.Duration) {
	l.t.Helper()
	clients := l.clientNodes()
	deadline := time.Now().Add(timeout)
	for {
		l.reconcileFabricRoutes()
		var pending []string
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, from := range clients {
			for _, to := range clients {
				if from.Name == to.Name {
					continue
				}
				from := from
				to := to
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := l.netnsErrWithin(4*time.Second, from.Name, "timeout", "3s", "ping", "-c", "1", "-W", "1", ipOnly(to.SiteCIDR)); err != nil {
						mu.Lock()
						pending = append(pending, from.Name+" -> "+to.Name+"("+ipOnly(to.SiteCIDR)+"): "+err.Error())
						mu.Unlock()
					}
				}()
			}
		}
		wg.Wait()
		if len(pending) == 0 {
			return
		}
		if time.Now().After(deadline) {
			l.dumpLogs()
			l.t.Fatalf("client hostname-matrix-equivalent reachability did not converge:\n%s", strings.Join(pending, "\n"))
		}
		time.Sleep(2 * time.Second)
	}
}

func (l *lab) StartClientMatrixMonitor(interval, timeout time.Duration) func() {
	l.t.Helper()
	ctx, cancel := context.WithCancel(l.ctx)
	done := make(chan struct{})
	var mu sync.Mutex
	var failures []string
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		deadline := time.Now().Add(timeout)
		for {
			if err := l.clientMatrixOnce(2 * time.Second); err != nil {
				mu.Lock()
				failures = append(failures, err.Error())
				mu.Unlock()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if time.Now().After(deadline) {
				return
			}
		}
	}()
	return func() {
		cancel()
		<-done
		mu.Lock()
		defer mu.Unlock()
		if len(failures) > 0 {
			l.dumpLogs()
			l.t.Fatalf("client matrix had %d failure(s) during drain, first: %s", len(failures), failures[0])
		}
	}
}

func (l *lab) clientMatrixOnce(commandTimeout time.Duration) error {
	clients := l.clientNodes()
	var pending []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, from := range clients {
		for _, to := range clients {
			if from.Name == to.Name {
				continue
			}
			from := from
			to := to
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := l.netnsErrWithin(commandTimeout, from.Name, "timeout", "2s", "ping", "-c", "1", "-W", "1", ipOnly(to.SiteCIDR)); err != nil {
					mu.Lock()
					pending = append(pending, from.Name+" -> "+to.Name+"("+ipOnly(to.SiteCIDR)+"): "+err.Error())
					mu.Unlock()
				}
			}()
		}
	}
	wg.Wait()
	if len(pending) > 0 {
		return errors.New(strings.Join(pending, "\n"))
	}
	return nil
}

func (l *lab) AssertCapturesAbsent(nodeName string, timeout time.Duration) {
	l.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if got := l.captureAddresses(nodeName); len(got) == 0 {
			return
		}
		if time.Now().After(deadline) {
			l.dumpLogs()
			l.t.Fatalf("%s still has captures after timeout: %v", nodeName, l.captureAddresses(nodeName))
		}
		time.Sleep(2 * time.Second)
	}
}

func (l *lab) AssertCaptureAddressesAbsent(nodeName string, addresses []string, timeout time.Duration) {
	l.t.Helper()
	wantAbsent := map[string]bool{}
	for _, address := range addresses {
		wantAbsent[address] = true
	}
	deadline := time.Now().Add(timeout)
	for {
		var remaining []string
		for _, got := range l.captureAddresses(nodeName) {
			if wantAbsent[got] {
				remaining = append(remaining, got)
			}
		}
		if len(remaining) == 0 {
			return
		}
		if time.Now().After(deadline) {
			l.dumpLogs()
			l.t.Fatalf("%s still has drained captures after timeout: %v", nodeName, remaining)
		}
		time.Sleep(2 * time.Second)
	}
}

func (l *lab) AssertCaptureAddressesPresent(nodeName string, addresses []string, timeout time.Duration) {
	l.t.Helper()
	wantPresent := map[string]bool{}
	for _, address := range addresses {
		wantPresent[address] = true
	}
	deadline := time.Now().Add(timeout)
	for {
		gotPresent := map[string]bool{}
		for _, got := range l.captureAddresses(nodeName) {
			gotPresent[got] = true
		}
		var missing []string
		for address := range wantPresent {
			if !gotPresent[address] {
				missing = append(missing, address)
			}
		}
		if len(missing) == 0 {
			return
		}
		if time.Now().After(deadline) {
			l.dumpLogs()
			l.t.Fatalf("%s missing handed-off captures after timeout: %v", nodeName, missing)
		}
		time.Sleep(2 * time.Second)
	}
}

func (l *lab) AssertCapturesPresent(nodeName string, minCount int, timeout time.Duration) {
	l.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if got := l.captureAddresses(nodeName); len(got) >= minCount {
			return
		}
		if time.Now().After(deadline) {
			l.dumpLogs()
			l.t.Fatalf("%s captures = %v, want at least %d", nodeName, l.captureAddresses(nodeName), minCount)
		}
		time.Sleep(2 * time.Second)
	}
}

func (l *lab) leafFailoverPair(preferredSite string) (string, string, []string) {
	l.t.Helper()
	type candidate struct {
		failed   node
		survivor node
		captures []string
	}
	var fallback *candidate
	for _, failed := range l.leafNodes() {
		captures := l.captureAddresses(failed.Name)
		if len(captures) == 0 {
			continue
		}
		for _, survivor := range l.leafNodes() {
			if survivor.Site != failed.Site || survivor.Name == failed.Name {
				continue
			}
			c := candidate{failed: failed, survivor: survivor, captures: captures}
			if failed.Site == preferredSite {
				return c.failed.Name, c.survivor.Name, c.captures
			}
			if fallback == nil {
				fallback = &c
			}
			break
		}
	}
	if fallback != nil {
		return fallback.failed.Name, fallback.survivor.Name, fallback.captures
	}
	l.dumpLogs()
	l.t.Fatalf("no leaf has captures before failover")
	return "", "", nil
}

func (l *lab) AssertFabricRoutesSynced(addresses []string, timeout time.Duration) {
	l.t.Helper()
	owners := l.clientAddressOwners()
	deadline := time.Now().Add(timeout)
	for {
		l.reconcileFabricRoutes()
		var pending []string
		for _, address := range addresses {
			ownerSite := owners[address]
			for _, site := range l.leafSites() {
				got := l.fabricRouteVia(site, address)
				if site == ownerSite {
					if got != "" {
						pending = append(pending, site+" "+address+" via "+got+", want direct")
					}
					continue
				}
				if got == "" {
					pending = append(pending, site+" "+address+" missing")
				}
			}
		}
		if len(pending) == 0 {
			return
		}
		if time.Now().After(deadline) {
			l.dumpLogs()
			l.t.Fatalf("fabric routes did not converge: %s", strings.Join(pending, ", "))
		}
		time.Sleep(2 * time.Second)
	}
}

func (l *lab) AssertBFDPeerState(nodeName, bfdName, want string, timeout time.Duration) {
	l.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got := l.bfdPeerState(nodeName, bfdName)
		if strings.EqualFold(got, want) {
			return
		}
		if time.Now().After(deadline) {
			l.dumpLogs()
			l.t.Fatalf("%s BFD/%s state = %q, want %q", nodeName, bfdName, got, want)
		}
		time.Sleep(2 * time.Second)
	}
}

func (l *lab) captureAddresses(nodeName string) []string {
	n, ok := l.nodeByName(nodeName)
	if !ok {
		return nil
	}
	out, err := l.netnsOutput(nodeName, "ip", "-o", "-4", "addr", "show", "dev", "eth1")
	if err != nil {
		return nil
	}
	var captures []string
	for _, address := range parseIPv4HostAddresses(out) {
		if address == ipOnly(n.SiteCIDR)+"/32" {
			continue
		}
		captures = append(captures, address)
	}
	return captures
}

func (l *lab) fabricRouteVia(site, address string) string {
	out, err := l.netnsOutputByName(l.fabricNS(site), "ip", "-4", "route", "get", ipOnly(address))
	if err != nil {
		return ""
	}
	fields := strings.Fields(out)
	for i, field := range fields {
		if field == "via" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func (l *lab) bfdPeerState(nodeName, bfdName string) string {
	statusSocket := filepath.Join(l.nodeDir(nodeName, "run"), "status.sock")
	out, err := l.netnsOutput(nodeName, filepath.Join(l.binDir, "routerctl"), "get", "BFD/"+bfdName, "--socket", statusSocket, "-o", "json")
	if err != nil {
		return ""
	}
	var value any
	if err := json.Unmarshal([]byte(out), &value); err != nil {
		return ""
	}
	peerStates, ok := findJSONField(value, "peerStates")
	if !ok {
		return ""
	}
	switch typed := peerStates.(type) {
	case map[string]any:
		for _, state := range typed {
			if s, ok := state.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func (l *lab) Cleanup() {
	l.stopFabricRouteReconciler()
	if os.Getenv(keepEnv) == "1" {
		l.t.Logf("%s=1; preserving netns lab %s under %s", keepEnv, l.name, l.workDir)
		return
	}
	for i := len(l.procs) - 1; i >= 0; i-- {
		stopProcess(l.procs[i])
	}
	for i := len(l.nodes) - 1; i >= 0; i-- {
		_, _ = runOutput(context.Background(), "", "ip", "netns", "delete", l.ns(l.nodes[i].Name))
	}
	for i := len(l.fabrics) - 1; i >= 0; i-- {
		_, _ = runOutput(context.Background(), "", "ip", "netns", "delete", l.fabrics[i])
	}
	for i := len(l.bridges) - 1; i >= 0; i-- {
		_, _ = runOutput(context.Background(), "", "ip", "link", "delete", l.bridges[i])
	}
	if l.lockFile != nil {
		l.lockFile.Close()
	}
}

func (l *lab) createFabric(site string) {
	l.t.Helper()
	ns := l.fabricNS(site)
	l.run("", "ip", "netns", "add", ns)
	l.run("", "ip", "-n", ns, "link", "set", "lo", "up")
	l.fabrics = append(l.fabrics, ns)
}

func (l *lab) createBridge(name string) {
	l.t.Helper()
	if _, err := runOutput(l.ctx, "", "ip", "link", "show", name); err != nil {
		l.run("", "ip", "link", "add", name, "type", "bridge")
	}
	l.run("", "ip", "link", "set", name, "up")
	l.bridges = append(l.bridges, name)
}

func (l *lab) attach(nodeName, ifName, bridge string) {
	l.t.Helper()
	l.attachNamespace(l.ns(nodeName), nodeName, ifName, bridge)
}

func (l *lab) attachNamespace(nsName, endpointName, ifName, bridge string) {
	l.t.Helper()
	hostIf := linuxIfName("rh", l.name, endpointName, ifName)
	peerIf := linuxIfName("rp", l.name, endpointName, ifName)
	l.run("", "ip", "link", "add", hostIf, "type", "veth", "peer", "name", peerIf)
	l.run("", "ip", "link", "set", hostIf, "master", bridge)
	l.run("", "ip", "link", "set", hostIf, "up")
	l.run("", "ip", "link", "set", peerIf, "netns", nsName)
	l.netnsByName(nsName, "ip", "link", "set", peerIf, "name", ifName)
	l.netnsByName(nsName, "ip", "link", "set", ifName, "up")
}

func (l *lab) writeRouterConfigs() {
	l.t.Helper()
	for _, n := range l.routerNodes() {
		path := l.nodeDir(n.Name, "router.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			l.t.Fatalf("create node dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(l.renderRouterConfig(n)), 0o644); err != nil {
			l.t.Fatalf("write router config for %s: %v", n.Name, err)
		}
	}
}

func (l *lab) startNetNS(nodeName, binary string, args ...string) *exec.Cmd {
	l.t.Helper()
	logPath := l.nodeDir(nodeName, filepath.Base(binary)+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		l.t.Fatalf("create %s log: %v", nodeName, err)
	}
	fullArgs := append([]string{"netns", "exec", l.ns(nodeName), binary}, args...)
	cmd := exec.CommandContext(l.ctx, "ip", fullArgs...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		l.t.Fatalf("start %s in %s: %v", filepath.Base(binary), nodeName, err)
	}
	return cmd
}

func stopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
}

func (l *lab) run(dir, name string, args ...string) {
	l.t.Helper()
	if _, err := runOutput(l.ctx, dir, name, args...); err != nil {
		l.t.Fatal(err)
	}
}

func (l *lab) netns(nodeName string, args ...string) {
	l.t.Helper()
	if err := l.netnsErr(nodeName, args...); err != nil {
		l.t.Fatal(err)
	}
}

func (l *lab) netnsErr(nodeName string, args ...string) error {
	_, err := l.netnsOutput(nodeName, args...)
	return err
}

func (l *lab) netnsErrWithin(timeout time.Duration, nodeName string, args ...string) error {
	return l.netnsErrWithinByName(timeout, l.ns(nodeName), args...)
}

func (l *lab) netnsErrWithinByName(timeout time.Duration, nsName string, args ...string) error {
	ctx, cancel := context.WithTimeout(l.ctx, timeout)
	defer cancel()
	full := append([]string{"netns", "exec", nsName}, args...)
	_, err := runOutput(ctx, "", "ip", full...)
	return err
}

func (l *lab) netnsOutput(nodeName string, args ...string) (string, error) {
	return l.netnsOutputByName(l.ns(nodeName), args...)
}

func (l *lab) netnsByName(nsName string, args ...string) {
	l.t.Helper()
	if err := l.netnsErrByName(nsName, args...); err != nil {
		l.t.Fatal(err)
	}
}

func (l *lab) netnsErrByName(nsName string, args ...string) error {
	_, err := l.netnsOutputByName(nsName, args...)
	return err
}

func (l *lab) netnsOutputByName(nsName string, args ...string) (string, error) {
	full := append([]string{"netns", "exec", nsName}, args...)
	return runOutput(l.ctx, "", "ip", full...)
}

func runOutput(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	out := output.String()
	if err != nil {
		return out, cmdError{Command: strings.Join(append([]string{name}, args...), " "), Output: out, Err: err}
	}
	return out, nil
}

func (l *lab) dumpLogs() {
	for _, n := range l.routerNodes() {
		for _, base := range []string{"routerd.log", "routerd-bgp.log"} {
			path := l.nodeDir(n.Name, base)
			content, err := os.ReadFile(path)
			if err == nil && len(content) > 0 {
				l.t.Logf("%s %s:\n%s", n.Name, base, tailLines(string(content), 80))
			}
		}
	}
}

func tailLines(s string, maxLines int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

func (l *lab) routerNodes() []node {
	var out []node
	for _, n := range l.nodes {
		if n.Router {
			out = append(out, n)
		}
	}
	return out
}

func (l *lab) clientNodes() []node {
	var out []node
	for _, n := range l.nodes {
		if n.Role == "client" {
			out = append(out, n)
		}
	}
	return out
}

func (l *lab) rrCount() int {
	count := 0
	for _, n := range l.nodes {
		if n.Role == "rr" {
			count++
		}
	}
	return count
}

func (l *lab) nodeByName(name string) (node, bool) {
	for _, n := range l.nodes {
		if n.Name == name {
			return n, true
		}
	}
	return node{}, false
}

func (l *lab) ns(name string) string {
	return shortName(l.name + "-" + name)
}

func (l *lab) bridge(name string) string {
	return linuxIfName("rb", l.name, name)
}

func (l *lab) siteClientBridge(site string) string {
	return l.bridge(site + "-client")
}

func (l *lab) siteLeafBridge(site string) string {
	return l.bridge(site + "-leaf")
}

func (l *lab) fabricNS(site string) string {
	return shortName(l.name + "-fabric-" + site)
}

func (l *lab) nodeDir(nodeName string, parts ...string) string {
	all := append([]string{l.workDir, nodeName}, parts...)
	return filepath.Join(all...)
}

func shortName(name string) string {
	if len(name) <= 48 {
		return name
	}
	return name[:48]
}

func linuxIfName(prefix string, parts ...string) string {
	h := fnv.New32a()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	name := fmt.Sprintf("%s%x", prefix, h.Sum32())
	if len(name) <= 15 {
		return name
	}
	return name[:15]
}

func bfdResourceName(self, peer string) string {
	return testSafeName("sam-bfd-" + self + "-" + peer)
}

func transportBGPPeerName(self, peer string) string {
	return testSafeName("sam-transport-local-transport-" + self + "-" + peer)
}

func testSafeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "mobility"
	}
	return out
}

func ipOnly(cidr string) string {
	return strings.SplitN(cidr, "/", 2)[0]
}

func cidrWithIP(ip, cidr string) string {
	if _, ipnet, err := net.ParseCIDR(cidr); err == nil {
		ones, _ := ipnet.Mask.Size()
		return ip + "/" + strconv.Itoa(ones)
	}
	return ip + "/24"
}

func siteFabricClientGateway(site string, nodes []node) string {
	return siteHostIP(site, nodes, 1)
}

func siteFabricLeafGateway(site string, nodes []node) string {
	return siteHostIP(site, nodes, 254)
}

func siteHostIP(site string, nodes []node, host byte) string {
	for _, n := range nodes {
		if n.Site != site {
			continue
		}
		ip, _, err := net.ParseCIDR(n.SiteCIDR)
		if err != nil {
			return ""
		}
		v4 := ip.To4()
		if v4 == nil {
			return ""
		}
		v4[3] = host
		return net.IPv4(v4[0], v4[1], v4[2], v4[3]).String()
	}
	return ""
}

func parseIPv4HostAddresses(output string) []string {
	var out []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field != "inet" || i+1 >= len(fields) {
				continue
			}
			ip, ipnet, err := net.ParseCIDR(fields[i+1])
			if err != nil {
				continue
			}
			ones, bits := ipnet.Mask.Size()
			if bits == 32 && ones == 32 {
				out = append(out, ip.String()+"/32")
			}
		}
	}
	return out
}

func nodeIndex(name string) int {
	switch name {
	case "aws-rr-a":
		return 1
	case "aws-rr-b":
		return 2
	case "aws-leaf-a":
		return 3
	case "aws-leaf-b":
		return 4
	case "azure-leaf-a":
		return 5
	case "azure-leaf-b":
		return 6
	case "oci-leaf-a":
		return 7
	case "oci-leaf-b":
		return 8
	case "pve-leaf-a":
		return 9
	case "pve-leaf-b":
		return 10
	default:
		return 99
	}
}

func jsonNumberField(data []byte, name string) (int, bool) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return 0, false
	}
	found, ok := findJSONField(value, name)
	if !ok {
		return 0, false
	}
	switch v := found.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	default:
		return 0, false
	}
}

func jsonStringField(data []byte, name string) (string, bool) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return "", false
	}
	found, ok := findJSONField(value, name)
	if !ok {
		return "", false
	}
	s, ok := found.(string)
	return s, ok
}

func findJSONField(value any, name string) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if v, ok := typed[name]; ok {
			return v, true
		}
		for _, v := range typed {
			if found, ok := findJSONField(v, name); ok {
				return found, true
			}
		}
	case []any:
		for _, v := range typed {
			if found, ok := findJSONField(v, name); ok {
				return found, true
			}
		}
	}
	return nil, false
}

func (l *lab) renderRouterConfig(self node) string {
	privateKeys := map[string]string{}
	publicKeys := map[string]string{}
	for _, n := range l.routerNodes() {
		key, pub := l.wireGuardKeyPair(n.Name)
		privateKeys[n.Name] = key
		publicKeys[n.Name] = pub
	}
	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: routerd.net/v1alpha1\nkind: Router\nmetadata:\n  name: %s\nspec:\n  resources:\n", self.Name)
	fmt.Fprintf(&b, `    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMNodeSet
      metadata: { name: local-netns-nodes }
      spec:
        nodes:
`)
	for _, n := range l.routerNodes() {
		fmt.Fprintf(&b, "          - nodeRef: %s\n", n.Name)
		fmt.Fprintf(&b, "            site: %s\n", n.Site)
		fmt.Fprintf(&b, "            role: cloud\n")
		fmt.Fprintf(&b, "            routeReflector: %v\n", n.Role == "rr")
		fmt.Fprintf(&b, "            eventEndpoint: http://10.253.0.%d:9443\n", nodeIndex(n.Name)+10)
		fmt.Fprintf(&b, "            samEndpoint: 10.253.0.%d\n", nodeIndex(n.Name)+10)
		fmt.Fprintf(&b, "            wireGuard:\n")
		fmt.Fprintf(&b, "              publicKey: %s\n", publicKeys[n.Name])
		fmt.Fprintf(&b, "              endpoint: %s:51820\n", ipOnly(n.Transport))
		fmt.Fprintf(&b, "              allowedIPs: [10.253.0.%d/32]\n", nodeIndex(n.Name)+10)
		fmt.Fprintf(&b, "              persistentKeepalive: 5\n")
	}
	fmt.Fprintf(&b, `
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventGroup
      metadata: { name: cloudedge }
      spec:
        nodeName: %s
        peersFrom:
          - resource: SAMNodeSet/local-netns-nodes
        retention: { maxEvents: 1000, maxAge: 24h }
        listen: { address: 10.253.0.%d, port: 9443 }
        replayWindow: 5m

    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: { name: wg-netns }
      spec:
        selfNodeRef: %s
        privateKey: %s
        listenPort: 51820
        mtu: 1420
        peersFrom:
          - resource: SAMNodeSet/local-netns-nodes

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: { name: wg-netns }
      spec: { ifname: wg-netns, managed: false, mtu: 1420 }

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: { name: site-lan }
      spec: { ifname: eth1, managed: false }

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata: { name: site-primary }
      spec: { interface: site-lan, address: %s }

    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata: { name: mobility-bgp }
      spec:
        asn: 64522
        routerID: 10.253.0.%d
        listen: { port: 179 }
        timers: { profile: fast }
        convergenceProfile: fast
        importPolicy: { allowedPrefixes: [10.77.60.0/24, 10.99.0.0/24], nextHopRewrite: unchanged }

    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMTransportProfile
      metadata: { name: local-transport }
      spec:
        selfNodeRef: %s
        mode: ipip
        encryption: wireguard
        innerPrefix: 10.99.0.0/24
        underlayInterface: wg-netns
        localEndpoint: 10.253.0.%d
        addressingMode: pair-stable
        peersFrom:
          - resource: SAMNodeSet/local-netns-nodes
        bgp:
          routerRef: BGPRouter/mobility-bgp
          peerASN: 64522
          timersPreset: fast
          importPolicy:
            allowedPrefixes: [10.77.60.0/24]
            nextHopRewrite: peer-address
`, self.Name, nodeIndex(self.Name)+10, self.Name, privateKeys[self.Name], self.SiteCIDR, nodeIndex(self.Name)+10, self.Name, nodeIndex(self.Name)+10)
	if l.bfd {
		for _, peer := range l.routerNodes() {
			if peer.Name == self.Name {
				continue
			}
			fmt.Fprintf(&b, `
    - apiVersion: net.routerd.net/v1alpha1
      kind: BFD
      metadata: { name: %s }
      spec:
        peer: BGPPeer/%s
        profile: fast
        minRx: 300ms
        minTx: 300ms
        detectMultiplier: 3
`, bfdResourceName(self.Name, peer.Name), transportBGPPeerName(self.Name, peer.Name))
		}
	}
	if self.Role == "leaf" {
		fmt.Fprintf(&b, `
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: CloudProviderProfile
      metadata: { name: netns-lab }
      spec:
        provider: netns
        capabilities: [secondary-ip, ip-forwarding]
        auth: { mode: external-command, command: /bin/true }

    - apiVersion: plugin.routerd.net/v1alpha1
      kind: Plugin
      metadata: { name: netns-executor }
      spec:
        executable: %s
        timeout: 15s
        capabilities: [execute.providerAction]

    - apiVersion: plugin.routerd.net/v1alpha1
      kind: Plugin
      metadata: { name: netns-inventory }
      spec:
        executable: %s
        timeout: 15s
        env:
          ROUTERD_NETNS_SITE: %s
          ROUTERD_NETNS_SELF_IP: %s
          ROUTERD_NETNS_CLIENT_IPS: %s
        capabilities: [observe.providerPrivateIPs]

    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: ProviderActionPolicy
      metadata: { name: netns-live-mutation }
      spec:
        enabled: true
        dryRunOnly: false
        requireApproval: false
        allowedProviders: [netns]
        allowedProviderRefs: [netns-lab]
        allowedActions: [assign-secondary-ip, unassign-secondary-ip, ensure-forwarding-enabled, ensure-forwarding-disabled]
        allowedCIDRs: [10.77.60.0/24]
        maxActionsPerRun: 64
        allowUndo: true

    - apiVersion: mobility.routerd.net/v1alpha1
      kind: MobilityPool
      metadata: { name: cloudedge }
      spec:
        prefix: 10.77.60.0/24
        groupRef: cloudedge
        deliveryPolicy: { mode: bgp }
        capturePolicy: { mode: all-non-owner-sites }
        ipOwnershipPolicy: { type: centralized, autoFailover: true }
        profiles:
          cloudCaptures:
`, filepath.Join(l.binDir, "netns-provider-executor"), filepath.Join(l.binDir, "netns-provider-inventory"), self.Site, self.SiteCIDR, l.siteClientEnv(self.Site))
		for _, leaf := range l.leafNodes() {
			fmt.Fprintf(&b, "            %s-self:\n", leaf.Name)
			fmt.Fprintf(&b, "              capture:\n")
			fmt.Fprintf(&b, "                type: provider-secondary-ip\n")
			fmt.Fprintf(&b, "                interface: eth1\n")
			fmt.Fprintf(&b, "                providerRef: netns-lab\n")
			fmt.Fprintf(&b, "                providerMode: secondary-ip\n")
			fmt.Fprintf(&b, "                captureStrategy: secondary-ip\n")
			fmt.Fprintf(&b, "                nicRef: eth1\n")
			fmt.Fprintf(&b, "                configureOSAddress: true\n")
			fmt.Fprintf(&b, "                target: { interface: eth1 }\n")
			fmt.Fprintf(&b, "              ownershipDiscovery:\n")
			fmt.Fprintf(&b, "                mode: provider-private-ip\n")
			fmt.Fprintf(&b, "                providerRef: netns-lab\n")
			fmt.Fprintf(&b, "                pluginRef: netns-inventory\n")
			fmt.Fprintf(&b, "                subnetRef: %s\n", leaf.Site)
			fmt.Fprintf(&b, "                scanInterval: 30s\n")
			fmt.Fprintf(&b, "                leaseTTL: 2m\n")
			fmt.Fprintf(&b, "                selector:\n")
			fmt.Fprintf(&b, "                  tags:\n")
			fmt.Fprintf(&b, "                    cloudedge-mobility: \"true\"\n")
		}
		fmt.Fprintf(&b, `        members:
`)
		for _, leaf := range l.leafNodes() {
			fmt.Fprintf(&b, "          - nodeRef: %s\n", leaf.Name)
			fmt.Fprintf(&b, "            site: %s\n", leaf.Site)
			fmt.Fprintf(&b, "            role: cloud\n")
			fmt.Fprintf(&b, "            profileRef: %s-self\n", leaf.Name)
			fmt.Fprintf(&b, "            placement: { group: %s, priority: %d }\n", leaf.Site, leafPriority(leaf.Name))
			fmt.Fprintf(&b, "            maxSecondaryIPs: 128\n")
		}
		fmt.Fprintf(&b, "\n")
	}
	return b.String()
}

func (l *lab) wireGuardKeyPair(nodeName string) (string, string) {
	keyPath := l.nodeDir(nodeName, "wg.key")
	pubPath := l.nodeDir(nodeName, "wg.pub")
	if _, err := os.Stat(keyPath); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(keyPath), 0o755); err != nil {
			l.t.Fatalf("create key dir: %v", err)
		}
		key, err := runOutput(l.ctx, "", "wg", "genkey")
		if err != nil {
			l.t.Fatalf("generate WireGuard key for %s: %v", nodeName, err)
		}
		key = strings.TrimSpace(key)
		if err := os.WriteFile(keyPath, []byte(key), 0o600); err != nil {
			l.t.Fatalf("write private key: %v", err)
		}
		cmd := exec.CommandContext(l.ctx, "wg", "pubkey")
		cmd.Stdin = strings.NewReader(key)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			l.t.Fatalf("derive public key for %s: %v\n%s", nodeName, err, stderr.String())
		}
		if err := os.WriteFile(pubPath, []byte(strings.TrimSpace(stdout.String())), 0o644); err != nil {
			l.t.Fatalf("write public key: %v", err)
		}
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		l.t.Fatalf("read private key: %v", err)
	}
	pub, err := os.ReadFile(pubPath)
	if err != nil {
		l.t.Fatalf("read public key: %v", err)
	}
	return strings.TrimSpace(string(key)), strings.TrimSpace(string(pub))
}

func (l *lab) leafNodes() []node {
	var out []node
	for _, n := range l.nodes {
		if n.Role == "leaf" {
			out = append(out, n)
		}
	}
	return out
}

func (l *lab) siteClientEnv(site string) string {
	var parts []string
	for _, n := range l.nodes {
		if n.Role == "client" && n.Site == site {
			parts = append(parts, n.Name+"="+n.SiteCIDR)
		}
	}
	return strings.Join(parts, ",")
}

func leafPriority(name string) int {
	if strings.HasSuffix(name, "-a") {
		return 10
	}
	return 20
}

func TestRenderedNetNSRouterConfigs(t *testing.T) {
	if _, err := exec.LookPath("wg"); err != nil {
		t.Skipf("wg is required to render WireGuard keys: %v", err)
	}
	for _, enableBFD := range []bool{false, true} {
		t.Run(fmt.Sprintf("bfd=%v", enableBFD), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			l := newLab(t, ctx)
			l.nodes = defaultTopology()
			l.bfd = enableBFD
			for _, n := range l.routerNodes() {
				rendered := l.renderRouterConfig(n)
				for _, want := range []string{"kind: Router", "kind: WireGuardInterface", "kind: BGPRouter", "kind: SAMTransportProfile"} {
					if !strings.Contains(rendered, want) {
						t.Fatalf("%s config missing %q:\n%s", n.Name, want, rendered)
					}
				}
				if enableBFD && !strings.Contains(rendered, "kind: BFD") {
					t.Fatalf("%s BFD config missing BFD resource:\n%s", n.Name, rendered)
				}
				if n.Role == "leaf" && !strings.Contains(rendered, "kind: MobilityPool") {
					t.Fatalf("%s leaf config missing MobilityPool:\n%s", n.Name, rendered)
				}
				router, err := config.LoadBytes([]byte(rendered), n.Name+".yaml")
				if err != nil {
					t.Fatalf("%s config load: %v\n%s", n.Name, err, rendered)
				}
				if err := config.Validate(router); err != nil {
					t.Fatalf("%s config validate: %v\n%s", n.Name, err, rendered)
				}
				if net.ParseIP(ipOnly(n.Transport)) == nil {
					t.Fatalf("%s transport IP is invalid: %s", n.Name, n.Transport)
				}
			}
		})
	}
}
