// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package render

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

// Execute only inside an explicitly isolated outer netns. A single live UDP
// socket keeps its original tuple while production nft rules change underneath.
// This proves mark reassignment, not NAT or remote TCP-session survivability.
func TestEgressExistingFlowNative(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires explicit isolated Linux network namespace")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	host := os.Getenv("ROUTERD_HOST_NETNS")
	if !strings.HasPrefix(host, "net:[") || !strings.HasSuffix(host, "]") || current == host {
		t.Fatal("outer network namespace isolation is unverified")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	run := func(name string, args ...string) []byte {
		t.Helper()
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return out
	}
	peer := exec.CommandContext(ctx, "unshare", "-n", "sleep", "60")
	if err := peer.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = peer.Process.Kill(); _ = peer.Wait() }()
	path := fmt.Sprintf("/proc/%d/ns/net", peer.Process.Pid)
	deadline := time.Now().Add(3 * time.Second)
	for {
		ns, err := os.Readlink(path)
		if err == nil && ns != current {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("peer network namespace unavailable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	pid := strconv.Itoa(peer.Process.Pid)
	run("ip", "link", "add", "rd-flow", "type", "veth", "peer", "name", "rd-client")
	defer func() { _ = exec.Command("ip", "link", "del", "rd-flow").Run() }()
	run("ip", "link", "set", "rd-client", "netns", pid)
	run("ip", "address", "add", "198.18.26.1/30", "dev", "rd-flow")
	run("ip", "link", "set", "rd-flow", "up")
	run("nsenter", "--net="+path, "ip", "address", "add", "198.18.26.2/30", "dev", "rd-client")
	run("nsenter", "--net="+path, "ip", "link", "set", "rd-client", "up")
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("198.18.26.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	port := server.LocalAddr().(*net.UDPAddr).Port
	client := exec.CommandContext(ctx, "nsenter", "--net="+path, "python3", "-u", "-c", `import socket,sys
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM)
s.bind(('198.18.26.2',40000))
s.settimeout(3)
for line in sys.stdin:
 s.sendto(line.encode(),('198.18.26.1',int(sys.argv[1])))
 print(s.recv(128).decode(),flush=True)
`, strconv.Itoa(port))
	input, err := client.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := client.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	client.Stderr = &stderr
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close(); _ = client.Process.Kill(); _ = client.Wait() }()
	scanner := bufio.NewScanner(output)
	apply := func(spec api.EgressRoutePolicySpec, healthy []api.EgressRoutePolicyCandidate) {
		t.Helper()
		defaults, err := NftablesEgressRoutePolicyDefaultMarks("EgressRoutePolicy/wan", spec, spec.Candidates[0], healthy)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := NftablesIPv4PolicyRoutes(&api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: spec}}}})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, "nft", "-f", "-")
		cmd.Stdin = bytes.NewReader(append(defaults, hash...))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("apply nft rules: %v: %s", err, out)
		}
	}
	exchangeMark := func(stage string) int {
		t.Helper()
		if _, err := fmt.Fprintln(input, stage); err != nil {
			t.Fatal(err)
		}
		_ = server.SetReadDeadline(time.Now().Add(4 * time.Second))
		data := make([]byte, 128)
		n, addr, err := server.ReadFromUDP(data)
		if err != nil {
			t.Fatalf("UDP receive: %v; client=%s", err, stderr.String())
		}
		if addr.Port != 40000 || string(data[:n]) != stage+"\n" {
			t.Fatalf("unexpected persistent tuple/payload: %v %q", addr, data[:n])
		}
		if _, err := server.WriteToUDP([]byte(stage), addr); err != nil {
			t.Fatal(err)
		}
		if !scanner.Scan() || scanner.Text() != stage {
			t.Fatalf("UDP reply failed: %v; %s", scanner.Err(), stderr.String())
		}
		out := string(run("conntrack", "-L", "-p", "udp", "--orig-src", "198.18.26.2", "--sport", "40000", "--dport", strconv.Itoa(port), "-o", "extended"))
		for _, field := range strings.Fields(out) {
			if strings.HasPrefix(field, "mark=") {
				mark, err := strconv.Atoi(strings.TrimPrefix(field, "mark="))
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("%s: unchanged UDP tuple conntrack mark=%#x", stage, mark)
				return mark
			}
		}
		t.Fatalf("conntrack mark not found: %s", out)
		return 0
	}
	spec := api.EgressRoutePolicySpec{Mode: "priority", HashFields: []string{"sourceAddress"}, SourceCIDRs: []string{"198.18.26.2/32"}, Candidates: []api.EgressRoutePolicyCandidate{{Name: "initial-b", Mark: 0x111}}}
	apply(spec, spec.Candidates)
	if got := exchangeMark("initial-b"); got != 0x111 {
		t.Fatalf("initial mark=%#x", got)
	}
	ready, unready := true, false
	primary := api.EgressRoutePolicyCandidate{Name: "primary", Targets: []api.EgressRoutePolicyTarget{
		{Name: "a", Mark: 0x110, RuntimeReady: &ready},
		{Name: "b", Mark: 0x111, RuntimeReady: &unready},
		{Name: "c", Mark: 0x112, RuntimeReady: &ready},
	}}
	fallback := api.EgressRoutePolicyCandidate{Name: "fallback", Mark: 0x113}
	// During deferred HA admission the controller supplies declared marks,
	// including paths still being prepared, to protect replicated bindings.
	declared := primary
	declared.Targets = append([]api.EgressRoutePolicyTarget(nil), primary.Targets...)
	for index := range declared.Targets {
		declared.Targets[index].RuntimeReady = nil
	}
	spec.Candidates = []api.EgressRoutePolicyCandidate{fallback}
	apply(spec, []api.EgressRoutePolicyCandidate{declared, fallback})
	if got := exchangeMark("promotion-before-admission"); got != 0x111 {
		t.Fatalf("replicated mark changed before gateway publication: %#x", got)
	}
	spec.Candidates = []api.EgressRoutePolicyCandidate{primary}
	apply(spec, []api.EgressRoutePolicyCandidate{primary, fallback})
	got := exchangeMark("b-failed")
	if got != 0x110 && got != 0x112 {
		t.Fatalf("existing flow retained failed mark %#x; want healthy a/c", got)
	}
	if after := exchangeMark("still-healthy"); after != got {
		t.Fatalf("healthy flow changed mark from %#x to %#x", got, after)
	}
	spec.Candidates = []api.EgressRoutePolicyCandidate{fallback}
	apply(spec, spec.Candidates)
	if got := exchangeMark("all-primary-failed"); got != 0x113 {
		t.Fatalf("fallback mark=%#x", got)
	}
	primary.Targets[1].RuntimeReady = &ready
	spec.Candidates = []api.EgressRoutePolicyCandidate{primary}
	apply(spec, []api.EgressRoutePolicyCandidate{primary, fallback})
	if got := exchangeMark("primary-returned"); got != 0x113 {
		t.Fatalf("healthy fallback flow preempted: %#x", got)
	}
}
