// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package chain

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

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
