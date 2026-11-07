// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package chain

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestHAPromotionNativePreservesImportedConntrackMark(t *testing.T) {
	requireSystemPolicyNamespaces(t)
	currentMount, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil || os.Getenv("ROUTERD_HOST_MNTNS") == "" || currentMount == os.Getenv("ROUTERD_HOST_MNTNS") {
		t.Fatal("requires a private mount namespace for named network namespaces")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
	run := func(name string, args ...string) string {
		t.Helper()
		out, err := command(ctx, name, args...)
		if err != nil {
			t.Fatalf("%s %v: %v %s", name, args, err, out)
		}
		return string(out)
	}
	peer := fmt.Sprintf("rd15mark%d", os.Getpid())
	run("ip", "netns", "add", peer)
	t.Cleanup(func() {
		exec.Command("ip", "link", "del", "mark-router").Run()
		exec.Command("ip", "netns", "del", peer).Run()
	})
	run("ip", "link", "add", "mark-router", "type", "veth", "peer", "name", "mark-client")
	run("ip", "link", "set", "mark-client", "netns", peer)
	run("ip", "address", "add", "198.18.130.1/24", "dev", "mark-router")
	run("ip", "link", "set", "mark-router", "up")
	run("ip", "netns", "exec", peer, "ip", "address", "add", "198.18.130.2/24", "dev", "mark-client")
	run("ip", "netns", "exec", peer, "ip", "link", "set", "mark-client", "up")
	run("ip", "link", "set", "lo", "up")
	listener, err := net.ListenPacket("udp4", "198.18.130.1:49000")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		buf := make([]byte, 256)
		for {
			n, addr, err := listener.ReadFrom(buf)
			if err != nil {
				return
			}
			listener.WriteTo(buf[:n], addr)
		}
	}()
	now := time.Now().UTC()
	store := mapStore{api.NetAPIVersion + "/VirtualAddress/gateway": {"role": "backup", "vipPublicationConfirmed": false}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "primary"}, Spec: api.HealthCheckSpec{Target: "192.0.2.1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "internet"}, Spec: api.EgressRoutePolicySpec{Mode: "priority", SourceCIDRs: []string{"198.18.130.0/24"}, Candidates: []api.EgressRoutePolicyCandidate{
			{Name: "primary", Interface: "lo", Mark: 0x110, Table: 110, Weight: 120, HealthCheck: "primary", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/gateway.role": {Equals: "master"}}}},
			{Name: "fallback", Interface: "lo", Mark: 0x113, Table: 113, Weight: 80},
		}}},
	}}}
	store[api.NetAPIVersion+"/HealthCheck/primary"] = healthProbeStatusForTest(t, router, store, "primary", "Unhealthy", now, 3)
	c := IPv4PolicyRouteController{Router: router, DeclaredRouter: router, Store: store, Now: func() time.Time { return now }, CommandOutput: command}
	for i, phase := range []struct {
		role      string
		published bool
		mark      int
	}{{"backup", false, 272}, {"master", false, 272}, {"master", true, 275}} {
		store[api.NetAPIVersion+"/VirtualAddress/gateway"]["role"] = phase.role
		store[api.NetAPIVersion+"/VirtualAddress/gateway"]["vipPublicationConfirmed"] = phase.published
		if err := c.applyDefaultRoutePolicies(ctx, "nft", filepath.Join(t.TempDir(), "default.nft")); err != nil {
			t.Fatal(err)
		}
		port := fmt.Sprint(40000 + i)
		args := []string{"-p", "udp", "-s", "198.18.130.2", "-d", "198.18.130.1", "--sport", port, "--dport", "49000"}
		run("conntrack", append([]string{"-I"}, append(args, "--mark", "272", "--timeout", "60")...)...)
		run("ip", "netns", "exec", peer, "python3", "-c", "import socket,sys; s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.settimeout(3); s.bind(('198.18.130.2',int(sys.argv[1]))); s.sendto(b'original-session',('198.18.130.1',49000)); assert s.recv(256)==b'original-session'", port)
		actual := run("conntrack", append([]string{"-L"}, args...)...)
		if !strings.Contains(actual, fmt.Sprintf("mark=%d ", phase.mark)) {
			t.Fatalf("role=%s published=%t unexpected native mark: %s", phase.role, phase.published, actual)
		}
	}
	t.Log("real UDP payloads retain imported mark during backup/promotion; published master remaps unhealthy path")
}
