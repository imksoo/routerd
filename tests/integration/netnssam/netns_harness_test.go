// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package netnssam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const enableEnv = "ROUTERD_NETNS_INTEGRATION"

type lab struct {
	t       *testing.T
	ctx     context.Context
	name    string
	workDir string
	binDir  string
	nodes   []node
	bridges []string
	procs   []*exec.Cmd
}

type node struct {
	Name     string
	Role     string
	Underlay string
	Access   string
	Router   bool
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
	requireNetNS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	l := newLab(t, ctx)
	defer l.Cleanup()

	l.BuildBinaries()
	l.CreateTopology(defaultTopology())
	l.StartRouterProcesses()
	l.AssertUnderlayReachability()
	l.AssertRouterdStatusReady(2 * time.Minute)
	l.AssertWireGuardReachability(2 * time.Minute)
	l.AssertBGPEstablished(2 * time.Minute)
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
	for _, name := range []string{"ip", "wg"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s is required: %v", name, err)
		}
	}
}

func newLab(t *testing.T, ctx context.Context) *lab {
	t.Helper()
	workDir := t.TempDir()
	name := "routerd622-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	return &lab{
		t:       t,
		ctx:     ctx,
		name:    shortName(name),
		workDir: workDir,
		binDir:  filepath.Join(workDir, "bin"),
	}
}

func defaultTopology() []node {
	return []node{
		{Name: "rr1", Role: "rr", Underlay: "10.252.0.10/24", Router: true},
		{Name: "leaf-a", Role: "leaf", Underlay: "10.252.0.11/24", Access: "10.88.10.1/24", Router: true},
		{Name: "leaf-b", Role: "leaf", Underlay: "10.252.0.12/24", Access: "10.88.20.1/24", Router: true},
		{Name: "client-a", Role: "client", Access: "10.88.10.16/24"},
		{Name: "client-b", Role: "client", Access: "10.88.20.16/24"},
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
	} {
		l.run("", "go", "build", "-o", filepath.Join(l.binDir, target.Name), target.Pkg)
	}
}

func (l *lab) CreateTopology(nodes []node) {
	l.t.Helper()
	l.nodes = nodes
	for _, n := range nodes {
		l.run("", "ip", "netns", "add", l.ns(n.Name))
		l.run("", "ip", "-n", l.ns(n.Name), "link", "set", "lo", "up")
	}
	l.createBridge(l.bridge("underlay"))
	for _, n := range nodes {
		if n.Router {
			l.attach(n.Name, "eth1", l.bridge("underlay"))
			l.netns(n.Name, "ip", "addr", "add", n.Underlay, "dev", "eth1")
			l.netns(n.Name, "ip", "link", "set", "eth1", "up")
			l.netns(n.Name, "sysctl", "-w", "net.ipv4.ip_forward=1")
		}
	}
	l.createBridge(l.bridge("access-a"))
	l.createBridge(l.bridge("access-b"))
	l.attach("leaf-a", "eth2", l.bridge("access-a"))
	l.attach("client-a", "eth1", l.bridge("access-a"))
	l.attach("leaf-b", "eth2", l.bridge("access-b"))
	l.attach("client-b", "eth1", l.bridge("access-b"))
	for _, n := range nodes {
		if n.Access == "" {
			continue
		}
		dev := "eth1"
		if n.Router {
			dev = "eth2"
		}
		l.netns(n.Name, "ip", "addr", "add", n.Access, "dev", dev)
		l.netns(n.Name, "ip", "link", "set", dev, "up")
		if n.Role == "client" {
			l.netns(n.Name, "ip", "route", "replace", "10.88.60.0/24", "via", clientGateway(n.Name))
		}
	}
	l.writeRouterConfigs()
}

func (l *lab) StartRouterProcesses() {
	l.t.Helper()
	for _, n := range l.routerNodes() {
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
		routerd := l.startNetNS(n.Name, filepath.Join(l.binDir, "routerd"),
			"serve",
			"--config", l.nodeDir(n.Name, "router.yaml"),
			"--socket", filepath.Join(runtimeDir, "control.sock"),
			"--status-socket", filepath.Join(runtimeDir, "status.sock"),
			"--state-file", filepath.Join(stateDir, "routerd.db"),
			"--ledger-file", filepath.Join(stateDir, "ledger.db"),
			"--bgp-socket", filepath.Join(runtimeDir, "bgp", "gobgp.sock"),
			"--bgp-control-socket", filepath.Join(runtimeDir, "bgp", "control.sock"),
			"--bgp-state-file", filepath.Join(stateDir, "bgp", "applied.json"),
			"--apply-interval", "5s",
		)
		l.procs = append(l.procs, routerd)
	}
}

func (l *lab) AssertUnderlayReachability() {
	l.t.Helper()
	for _, from := range l.routerNodes() {
		for _, to := range l.routerNodes() {
			if from.Name == to.Name {
				continue
			}
			l.netns(from.Name, "ping", "-c", "1", "-W", "2", ipOnly(to.Underlay))
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
			err := l.netnsErr(n.Name, filepath.Join(l.binDir, "routerctl"), "status", "--socket", statusSocket, "-o", "json")
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
				if err := l.netnsErr(from.Name, "ping", "-c", "1", "-W", "1", target); err != nil {
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
			wantPeers := 1
			if n.Role == "rr" {
				wantPeers = 2
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

func (l *lab) Cleanup() {
	for i := len(l.procs) - 1; i >= 0; i-- {
		stopProcess(l.procs[i])
	}
	for i := len(l.nodes) - 1; i >= 0; i-- {
		_, _ = runOutput(context.Background(), "", "ip", "netns", "delete", l.ns(l.nodes[i].Name))
	}
	for i := len(l.bridges) - 1; i >= 0; i-- {
		_, _ = runOutput(context.Background(), "", "ip", "link", "delete", l.bridges[i])
	}
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
	hostIf := shortName(l.ns(nodeName) + "-" + ifName)
	peerIf := shortName(hostIf + "p")
	l.run("", "ip", "link", "add", hostIf, "type", "veth", "peer", "name", peerIf)
	l.run("", "ip", "link", "set", hostIf, "master", bridge)
	l.run("", "ip", "link", "set", hostIf, "up")
	l.run("", "ip", "link", "set", peerIf, "netns", l.ns(nodeName))
	l.netns(nodeName, "ip", "link", "set", peerIf, "name", ifName)
	l.netns(nodeName, "ip", "link", "set", ifName, "up")
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

func (l *lab) netnsOutput(nodeName string, args ...string) (string, error) {
	full := append([]string{"netns", "exec", l.ns(nodeName)}, args...)
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
				l.t.Logf("%s %s:\n%s", n.Name, base, content)
			}
		}
	}
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

func (l *lab) ns(name string) string {
	return shortName(l.name + "-" + name)
}

func (l *lab) bridge(name string) string {
	return shortName(l.name + "-" + name)
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

func clientGateway(name string) string {
	switch name {
	case "client-a":
		return "10.88.10.1"
	case "client-b":
		return "10.88.20.1"
	default:
		return ""
	}
}

func ipOnly(cidr string) string {
	return strings.SplitN(cidr, "/", 2)[0]
}

func nodeIndex(name string) int {
	switch name {
	case "rr1":
		return 1
	case "leaf-a":
		return 2
	case "leaf-b":
		return 3
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
		site := "local-a"
		if n.Name == "leaf-b" {
			site = "local-b"
		}
		fmt.Fprintf(&b, "          - nodeRef: %s\n", n.Name)
		fmt.Fprintf(&b, "            site: %s\n", site)
		fmt.Fprintf(&b, "            role: onprem\n")
		fmt.Fprintf(&b, "            routeReflector: %v\n", n.Role == "rr")
		fmt.Fprintf(&b, "            eventEndpoint: http://10.253.0.%d:9443\n", nodeIndex(n.Name)+10)
		fmt.Fprintf(&b, "            samEndpoint: 10.253.0.%d\n", nodeIndex(n.Name)+10)
		fmt.Fprintf(&b, "            wireGuard:\n")
		fmt.Fprintf(&b, "              publicKey: %s\n", publicKeys[n.Name])
		fmt.Fprintf(&b, "              endpoint: %s:51820\n", ipOnly(n.Underlay))
		fmt.Fprintf(&b, "              allowedIPs: [10.253.0.%d/32]\n", nodeIndex(n.Name)+10)
		fmt.Fprintf(&b, "              persistentKeepalive: 5\n")
	}
	fmt.Fprintf(&b, `
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
      kind: IPv4StaticAddress
      metadata: { name: wg-netns-self }
      spec: { interface: wg-netns, address: 10.253.0.%d/32 }

    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata: { name: mobility-bgp }
      spec:
        asn: 64522
        routerID: 10.253.0.%d
        listen: { port: 179 }
        timers: { profile: fast }
        convergenceProfile: fast
        importPolicy: { allowedPrefixes: [10.88.60.0/24, 10.99.0.0/24], nextHopRewrite: unchanged }

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
            allowedPrefixes: [10.88.60.0/24]
            nextHopRewrite: peer-address
`, self.Name, privateKeys[self.Name], nodeIndex(self.Name)+10, nodeIndex(self.Name)+10, self.Name, nodeIndex(self.Name)+10)
	if self.Role == "leaf" {
		fmt.Fprintf(&b, `
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: MobilityPool
      metadata: { name: local-capture }
      spec:
        prefix: 10.88.60.0/24
        groupRef: local
        deliveryPolicy: { mode: bgp }
        capturePolicy: { mode: all-non-owner-sites }
        ipOwnershipPolicy: { type: centralized, autoFailover: true }
        members:
          - nodeRef: leaf-a
            site: local-a
            role: onprem
            ownershipDiscovery:
              mode: onprem-l2
              sources:
                - type: arp-observer
                  interface: eth2
            capture:
              type: proxy-arp
              interface: eth2
              gratuitousARP: true
              activeWhen: { type: single-router }
          - nodeRef: leaf-b
            site: local-b
            role: onprem
            ownershipDiscovery:
              mode: onprem-l2
              sources:
                - type: arp-observer
                  interface: eth2
            capture:
              type: proxy-arp
              interface: eth2
              gratuitousARP: true
              activeWhen: { type: single-router }
`)
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

func TestRenderedNetNSRouterConfigs(t *testing.T) {
	if _, err := exec.LookPath("wg"); err != nil {
		t.Skipf("wg is required to render WireGuard keys: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	l := newLab(t, ctx)
	l.nodes = defaultTopology()
	for _, n := range l.routerNodes() {
		config := l.renderRouterConfig(n)
		for _, want := range []string{"kind: Router", "kind: WireGuardInterface", "kind: BGPRouter", "kind: SAMTransportProfile"} {
			if !strings.Contains(config, want) {
				t.Fatalf("%s config missing %q:\n%s", n.Name, want, config)
			}
		}
		if n.Role == "leaf" && !strings.Contains(config, "kind: MobilityPool") {
			t.Fatalf("%s leaf config missing MobilityPool:\n%s", n.Name, config)
		}
		if net.ParseIP(ipOnly(n.Underlay)) == nil {
			t.Fatalf("%s underlay IP is invalid: %s", n.Name, n.Underlay)
		}
	}
}
