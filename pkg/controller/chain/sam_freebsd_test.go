// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package chain

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/sam"
)

func TestFreeBSDSAMPublishedARPUsesExactAddressAndRefusesForeign(t *testing.T) {
	reset := saveFreeBSDSAMSeams()
	defer reset()
	var commands []string
	freeBSDSAMRunCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		switch strings.Join(args, " ") {
		case "-n 192.0.2.55":
			return nil, nil
		case "-s 192.0.2.55 02:00:00:00:00:55 pub":
			return nil, nil
		default:
			return nil, errors.New("unexpected command")
		}
	}
	freeBSDSAMInterfaceByName = func(name string) (*net.Interface, error) {
		if name != "em0" {
			t.Fatalf("interface = %q", name)
		}
		return &net.Interface{Name: name, HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 0x55}}, nil
	}
	if err := (freeBSDSAMProxyNeighborApplier{}).EnsureProxyNeighbor(context.Background(), "192.0.2.55/32", "em0"); err != nil {
		t.Fatalf("EnsureProxyNeighbor: %v", err)
	}
	if got, want := strings.Join(commands, "\n"), "arp -n 192.0.2.55\narp -s 192.0.2.55 02:00:00:00:00:55 pub"; got != want {
		t.Fatalf("commands = %q, want %q", got, want)
	}

	commands = nil
	freeBSDSAMRunCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return []byte("? (192.0.2.55) at 02:00:00:00:00:99 on em1 permanent [ethernet]\n"), nil
	}
	if err := (freeBSDSAMProxyNeighborApplier{}).EnsureProxyNeighbor(context.Background(), "192.0.2.55/32", "em0"); err == nil || !strings.Contains(err.Error(), "foreign published ARP") {
		t.Fatalf("foreign EnsureProxyNeighbor error = %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("foreign state commands = %#v, want only read-only probe", commands)
	}
}

func TestFreeBSDSAMARPEntryOnlyNormalizesCanonicalFreeBSDAbsence(t *testing.T) {
	reset := saveFreeBSDSAMSeams()
	defer reset()
	freeBSDSAMRunCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "arp" || strings.Join(args, " ") != "-n 198.18.250.99" {
			t.Fatalf("command = %s %s", name, strings.Join(args, " "))
		}
		return []byte("198.18.250.99 (198.18.250.99) -- no entry\n"), errors.New("exit status 1")
	}
	if entry, found, err := freeBSDARPEntry(context.Background(), "198.18.250.99"); err != nil || found || entry != "" {
		t.Fatalf("canonical absent entry = %q, found=%t, err=%v", entry, found, err)
	}

	freeBSDSAMRunCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("arp: route lookup failed\n"), errors.New("exit status 1")
	}
	if _, _, err := freeBSDARPEntry(context.Background(), "198.18.250.99"); err == nil || !strings.Contains(err.Error(), "route lookup failed") {
		t.Fatalf("unrelated arp lookup error = %v", err)
	}
}

func TestFreeBSDSAMCollisionAndPFEmptyCleanupContracts(t *testing.T) {
	reset := saveFreeBSDSAMSeams()
	defer reset()
	freeBSDSAMRunCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch name + " " + strings.Join(args, " ") {
		case "ifconfig -l":
			return []byte("em0 lo0\n"), nil
		case "ifconfig em0":
			return []byte("em0: flags\n\tinet 192.0.2.55 netmask 0xffffffff\n"), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}
	if _, err := (freeBSDSAMProxyNeighborApplier{}).EnsureOSAddressAbsent(context.Background(), "192.0.2.55/32"); err == nil || !strings.Contains(err.Error(), "foreign OS address") {
		t.Fatalf("collision error = %v", err)
	}

	var commands []string
	freeBSDSAMRunCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		if strings.Join(args, " ") == "-a routerd_sam_forward -sr" {
			return []byte("pass in quick on em0 inet from any to 192.0.2.55\n"), nil
		}
		if strings.Join(args, " ") == "-a routerd_sam_forward -F rules" {
			return nil, nil
		}
		return nil, errors.New("unexpected command")
	}
	if err := (freeBSDSAMProxyNeighborApplier{}).ReconcileForwardPaths(context.Background(), nil); err != nil {
		t.Fatalf("ReconcileForwardPaths empty: %v", err)
	}
	if got, want := strings.Join(commands, "\n"), "pfctl -a routerd_sam_forward -sr\npfctl -a routerd_sam_forward -F rules"; got != want {
		t.Fatalf("PF cleanup commands = %q, want %q", got, want)
	}
}

func TestFreeBSDSAMPFDeviceMissingIsOnlyAnEmptyDesiredNoop(t *testing.T) {
	reset := saveFreeBSDSAMSeams()
	defer reset()
	missing := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "pfctl" || strings.Join(args, " ") != "-a routerd_sam_forward -sr" {
			return nil, errors.New("unexpected command")
		}
		return []byte("pfctl: /dev/pf: No such file or directory\n"), errors.New("exit status 1")
	}
	freeBSDSAMRunCommand = missing
	if err := (freeBSDSAMProxyNeighborApplier{}).ReconcileForwardPaths(context.Background(), nil); err != nil {
		t.Fatalf("empty desired missing PF = %v", err)
	}
	paths := []sam.CaptureAction{{Kind: "forward-path", ClaimName: "claim", Address: "192.0.2.55", Interface: "em0", PeerInterface: "gif0"}}
	if err := (freeBSDSAMProxyNeighborApplier{}).ReconcileForwardPaths(context.Background(), paths); err == nil || !strings.Contains(err.Error(), "/dev/pf") {
		t.Fatalf("non-empty desired missing PF error = %v", err)
	}
}

func TestFreeBSDSAMPFForwardPathRequiresReachableAnchorAndUses32(t *testing.T) {
	reset := saveFreeBSDSAMSeams()
	defer reset()
	freeBSDSAMRunCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch name + " " + strings.Join(args, " ") {
		case "pfctl -a routerd_sam_forward -sr":
			return nil, nil
		case "pfctl -sr":
			return []byte("anchor \"routerd_sam_forward\" all\n"), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}
	var input string
	freeBSDSAMRunCommandInput = func(_ context.Context, name, gotInput string, args ...string) ([]byte, error) {
		if name != "pfctl" || strings.Join(args, " ") != "-a routerd_sam_forward -f -" {
			return nil, errors.New("unexpected PF load")
		}
		input = gotInput
		return nil, nil
	}
	paths := []sam.CaptureAction{{Kind: "forward-path", ClaimName: "claim", Address: "192.0.2.55/24", Interface: "em0", PeerInterface: "gif0"}}
	if err := (freeBSDSAMProxyNeighborApplier{}).ReconcileForwardPaths(context.Background(), paths); err != nil {
		t.Fatalf("ReconcileForwardPaths: %v", err)
	}
	if !strings.Contains(input, "192.0.2.55/32") || strings.Contains(input, "192.0.2.55/24") {
		t.Fatalf("PF rules do not retain /32 boundary:\n%s", input)
	}
	if !strings.Contains(input, "pass quick on em0 inet from any to 192.0.2.55/32") ||
		!strings.Contains(input, "pass quick on gif0 inet from 192.0.2.55/32 to any") {
		t.Fatalf("forward-path PF rules have wrong LAN-to-overlay direction:\n%s", input)
	}

	freeBSDSAMRunCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "pfctl" && strings.Join(args, " ") == "-a routerd_sam_forward -sr" {
			return nil, nil
		}
		if name == "pfctl" && strings.Join(args, " ") == "-sr" {
			return []byte("pass all\n"), nil
		}
		return nil, errors.New("unexpected command")
	}
	if err := (freeBSDSAMProxyNeighborApplier{}).ReconcileForwardPaths(context.Background(), paths); err == nil || !strings.Contains(err.Error(), "not reachable") {
		t.Fatalf("unreachable PF anchor error = %v", err)
	}
}

func TestFreeBSDSAMIPForwardingUsesFreeBSDSysctlAndFailsClosed(t *testing.T) {
	reset := saveFreeBSDSAMSeams()
	defer reset()
	var commands []string
	freeBSDSAMRunCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	if err := (freeBSDSAMProxyNeighborApplier{}).SetIPForwarding(context.Background(), true); err != nil {
		t.Fatalf("SetIPForwarding: %v", err)
	}
	if got, want := strings.Join(commands, "\n"), "sysctl -w net.inet.ip.forwarding=1"; got != want {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	freeBSDSAMRunCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("sysctl: permission denied\n"), errors.New("exit status 1")
	}
	if err := (freeBSDSAMProxyNeighborApplier{}).SetIPForwarding(context.Background(), true); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("SetIPForwarding failure = %v", err)
	}
}

func TestFreeBSDSAMPFForwardPathSerializesAnchorTransactions(t *testing.T) {
	reset := saveFreeBSDSAMSeams()
	defer reset()
	freeBSDSAMRunCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch name + " " + strings.Join(args, " ") {
		case "pfctl -a routerd_sam_forward -sr":
			return nil, nil
		case "pfctl -sr":
			return []byte("anchor \"routerd_sam_forward\" all\n"), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	freeBSDSAMRunCommandInput = func(_ context.Context, name, _ string, args ...string) ([]byte, error) {
		if name != "pfctl" || strings.Join(args, " ") != "-a routerd_sam_forward -f -" {
			return nil, errors.New("unexpected PF load")
		}
		entered <- struct{}{}
		<-release
		return nil, nil
	}
	paths := []sam.CaptureAction{{Kind: "forward-path", ClaimName: "claim", Address: "192.0.2.55", Interface: "em0", PeerInterface: "gif0"}}
	errCh := make(chan error, 2)
	go func() { errCh <- (freeBSDSAMProxyNeighborApplier{}).ReconcileForwardPaths(context.Background(), paths) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first PF transaction did not start")
	}
	go func() { errCh <- (freeBSDSAMProxyNeighborApplier{}).ReconcileForwardPaths(context.Background(), paths) }()
	select {
	case <-entered:
		t.Fatal("second PF transaction entered before the first completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatalf("ReconcileForwardPaths: %v", err)
		}
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("second PF transaction did not run after the first completed")
	}
}

func TestFreeBSDSAMBPFGratuitousARPUsesPublishedAddress(t *testing.T) {
	reset := saveFreeBSDSAMSeams()
	defer reset()
	freeBSDSAMInterfaceByName = func(string) (*net.Interface, error) {
		return &net.Interface{Name: "em0", HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 0x55}}, nil
	}
	freeBSDSAMOpenBPFDevice = func() (int, error) { return 7, nil }
	freeBSDSAMAttachBPFDevice = func(fd int, ifname string) error {
		if fd != 7 || ifname != "em0" {
			t.Fatalf("BPF attach fd=%d if=%s", fd, ifname)
		}
		return nil
	}
	freeBSDSAMSetBPFHeaderComplete = func(int) error { return nil }
	freeBSDSAMCloseBPF = func(int) error { return nil }
	var frames [][]byte
	freeBSDSAMWriteBPF = func(fd int, data []byte) (int, error) {
		if fd != 7 {
			t.Fatalf("BPF write fd=%d", fd)
		}
		frames = append(frames, append([]byte(nil), data...))
		return len(data), nil
	}
	if err := (freeBSDSAMGratuitousARPAnnouncer{}).SendGratuitousARP(context.Background(), "192.0.2.55/32", "em0"); err != nil {
		t.Fatalf("SendGratuitousARP: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("BPF GARP frame count = %d, want Linux-equivalent 3", len(frames))
	}
	for _, frame := range frames {
		if len(frame) != 42 || frame[28] != 192 || frame[29] != 0 || frame[30] != 2 || frame[31] != 55 {
			t.Fatalf("BPF GARP frame = %v", frame)
		}
	}
}

func saveFreeBSDSAMSeams() func() {
	run := freeBSDSAMRunCommand
	runInput := freeBSDSAMRunCommandInput
	iface := freeBSDSAMInterfaceByName
	open := freeBSDSAMOpenBPFDevice
	attach := freeBSDSAMAttachBPFDevice
	header := freeBSDSAMSetBPFHeaderComplete
	write := freeBSDSAMWriteBPF
	close := freeBSDSAMCloseBPF
	return func() {
		freeBSDSAMRunCommand = run
		freeBSDSAMRunCommandInput = runInput
		freeBSDSAMInterfaceByName = iface
		freeBSDSAMOpenBPFDevice = open
		freeBSDSAMAttachBPFDevice = attach
		freeBSDSAMSetBPFHeaderComplete = header
		freeBSDSAMWriteBPF = write
		freeBSDSAMCloseBPF = close
	}
}
