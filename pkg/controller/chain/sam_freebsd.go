// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package chain

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/imksoo/routerd/pkg/sam"
	"golang.org/x/sys/unix"
)

const freeBSDSAMForwardAnchor = "routerd_sam_forward"

// PF rule updates are a multi-command transaction (begin/add/commit inside
// pfctl). PF rejects a second transaction against the same anchor while the
// first one owns its inactive ticket. Keep the whole routerd-owned anchor
// reconciliation serial, matching the Linux iptables transaction boundary.
var freeBSDSAMForwardPathMu sync.Mutex

var (
	freeBSDSAMRunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
	freeBSDSAMRunCommandInput = func(ctx context.Context, name, input string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stdin = strings.NewReader(input)
		return cmd.CombinedOutput()
	}
	freeBSDSAMInterfaceByName      = net.InterfaceByName
	freeBSDSAMOpenBPFDevice        = openFreeBSDSAMBPF
	freeBSDSAMAttachBPFDevice      = attachFreeBSDSAMBPF
	freeBSDSAMSetBPFHeaderComplete = func(fd int) error {
		return unix.IoctlSetPointerInt(fd, unix.BIOCSHDRCMPLT, 1)
	}
	freeBSDSAMWriteBPF = unix.Write
	freeBSDSAMCloseBPF = unix.Close
)

// freeBSDSAMProxyNeighborApplier publishes exactly one IPv4 address through a
// permanent ARP entry on its declared interface. It never overwrites an entry
// that does not have the routerd-compatible published shape; that is the
// fail-closed boundary for administrator-owned ARP state.
type freeBSDSAMProxyNeighborApplier struct{}

func defaultSAMProxyNeighborApplier() samProxyNeighborApplier {
	return freeBSDSAMProxyNeighborApplier{}
}

func defaultSAMGratuitousARPAnnouncer() samGratuitousARPAnnouncer {
	return freeBSDSAMGratuitousARPAnnouncer{}
}

func (freeBSDSAMProxyNeighborApplier) SetProxyARP(context.Context, string, bool) error {
	// FreeBSD's per-address published ARP entries replace Linux proxy_arp.
	return nil
}

func (freeBSDSAMProxyNeighborApplier) SetIPForwarding(ctx context.Context, enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	out, err := freeBSDSAMRunCommand(ctx, "sysctl", "-w", "net.inet.ip.forwarding="+value)
	if err != nil {
		return fmt.Errorf("sysctl -w net.inet.ip.forwarding=%s: %w: %s", value, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (freeBSDSAMProxyNeighborApplier) EnsureProxyNeighbor(ctx context.Context, address, ifname string) error {
	ip, err := samIPv4Address(address)
	if err != nil {
		return err
	}
	entry, found, err := freeBSDARPEntry(ctx, ip, ifname)
	if err != nil {
		return err
	}
	if found {
		if freeBSDPublishedARPMatches(entry, ifname) {
			return nil
		}
		return fmt.Errorf("foreign published ARP %s: %s", ip, strings.TrimSpace(entry))
	}
	iface, err := freeBSDSAMInterfaceByName(ifname)
	if err != nil {
		return fmt.Errorf("lookup published-ARP interface %s: %w", ifname, err)
	}
	if len(iface.HardwareAddr) != 6 {
		return fmt.Errorf("published-ARP interface %s has non-ethernet MAC %q", ifname, iface.HardwareAddr)
	}
	// FreeBSD arp(8) accepts -i for F_SET. Keep the published neighbor on
	// the declared Ethernet interface even when a more-specific FIB route for
	// the same address points at a non-L2 tunnel.
	out, err := freeBSDSAMRunCommand(ctx, "arp", "-i", ifname, "-s", ip, iface.HardwareAddr.String(), "pub")
	if err != nil {
		return fmt.Errorf("publish ARP %s on %s: %w: %s", ip, ifname, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (freeBSDSAMProxyNeighborApplier) DeleteProxyNeighbor(ctx context.Context, address, ifname string) error {
	ip, err := samIPv4Address(address)
	if err != nil {
		return err
	}
	entry, found, err := freeBSDARPEntry(ctx, ip, ifname)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if !freeBSDPublishedARPMatches(entry, ifname) {
		return fmt.Errorf("foreign published ARP %s: refusing deletion", ip)
	}
	out, err := freeBSDSAMRunCommand(ctx, "arp", "-d", ip)
	if err != nil {
		return fmt.Errorf("delete published ARP %s: %w: %s", ip, err, strings.TrimSpace(string(out)))
	}
	if _, remains, err := freeBSDARPEntry(ctx, ip, ifname); err != nil {
		return err
	} else if remains {
		return fmt.Errorf("delete published ARP %s: scoped entry remains on %s", ip, ifname)
	}
	return nil
}

func (freeBSDSAMProxyNeighborApplier) EnsureOSAddressAbsent(ctx context.Context, address string) (samOSAddressDeassignResult, error) {
	ip, err := samIPv4Address(address)
	if err != nil {
		return samOSAddressDeassignResult{}, err
	}
	result := samOSAddressDeassignResult{address: address}
	out, err := freeBSDSAMRunCommand(ctx, "ifconfig", "-l")
	if err != nil {
		return result, fmt.Errorf("list FreeBSD interfaces: %w: %s", err, strings.TrimSpace(string(out)))
	}
	for _, ifname := range strings.Fields(string(out)) {
		data, err := freeBSDSAMRunCommand(ctx, "ifconfig", ifname)
		if err != nil {
			return result, fmt.Errorf("observe %s for SAM address collision: %w: %s", ifname, err, strings.TrimSpace(string(data)))
		}
		if freeBSDInterfaceHasIPv4(data, ip) {
			return result, fmt.Errorf("foreign OS address %s remains configured on %s", ip, ifname)
		}
	}
	return result, nil
}

func (freeBSDSAMProxyNeighborApplier) ReconcileForwardPaths(ctx context.Context, paths []sam.CaptureAction) error {
	freeBSDSAMForwardPathMu.Lock()
	defer freeBSDSAMForwardPathMu.Unlock()

	out, err := freeBSDSAMRunCommand(ctx, "pfctl", "-a", freeBSDSAMForwardAnchor, "-sr")
	if err != nil {
		// With no desired path, a missing PF device cannot carry a stale SAM
		// rule.  This is the empty-desired cleanup no-op boundary.  A desired
		// path must still fail closed below: it cannot be made active without
		// a reachable PF anchor.
		if len(paths) == 0 && (errors.Is(err, exec.ErrNotFound) || freeBSDPFDeviceUnavailable(out)) {
			return nil
		}
		return fmt.Errorf("pfctl -a %s -sr: %w: %s", freeBSDSAMForwardAnchor, err, strings.TrimSpace(string(out)))
	}
	if len(paths) == 0 {
		if strings.TrimSpace(string(out)) == "" {
			return nil
		}
		flushed, flushErr := freeBSDSAMRunCommand(ctx, "pfctl", "-a", freeBSDSAMForwardAnchor, "-F", "rules")
		if flushErr != nil {
			return fmt.Errorf("flush SAM PF anchor: %w: %s", flushErr, strings.TrimSpace(string(flushed)))
		}
		return nil
	}
	if err := freeBSDSAMForwardAnchorReachable(ctx); err != nil {
		return err
	}
	rules, err := freeBSDSAMForwardRules(paths)
	if err != nil {
		return err
	}
	loaded, loadErr := freeBSDSAMRunCommandInput(ctx, "pfctl", strings.Join(rules, "\n")+"\n", "-a", freeBSDSAMForwardAnchor, "-f", "-")
	if loadErr != nil {
		return fmt.Errorf("load SAM PF anchor: %w: %s", loadErr, strings.TrimSpace(string(loaded)))
	}
	return nil
}

func freeBSDPFDeviceUnavailable(out []byte) bool {
	return strings.Contains(strings.ToLower(string(out)), "/dev/pf: no such file or directory")
}

func freeBSDSAMForwardAnchorReachable(ctx context.Context) error {
	out, err := freeBSDSAMRunCommand(ctx, "pfctl", "-sr")
	if err != nil {
		return fmt.Errorf("observe active PF rules for SAM anchor: %w: %s", err, strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, `anchor "`+freeBSDSAMForwardAnchor+`"`) {
			return nil
		}
	}
	return fmt.Errorf("SAM PF anchor %q is not reachable from the active PF ruleset", freeBSDSAMForwardAnchor)
}

type freeBSDSAMGratuitousARPAnnouncer struct{}

func (freeBSDSAMGratuitousARPAnnouncer) SendGratuitousARP(ctx context.Context, address, ifname string) error {
	ip, err := samIPv4Address(address)
	if err != nil {
		return err
	}
	iface, err := freeBSDSAMInterfaceByName(ifname)
	if err != nil {
		return fmt.Errorf("lookup GARP interface %s: %w", ifname, err)
	}
	if len(iface.HardwareAddr) != 6 {
		return fmt.Errorf("GARP interface %s has non-ethernet MAC %q", ifname, iface.HardwareAddr)
	}
	fd, err := freeBSDSAMOpenBPFDevice()
	if err != nil {
		return err
	}
	defer freeBSDSAMCloseBPF(fd)
	if err := freeBSDSAMAttachBPFDevice(fd, ifname); err != nil {
		return err
	}
	if err := freeBSDSAMSetBPFHeaderComplete(fd); err != nil {
		return fmt.Errorf("BIOCSHDRCMPLT: %w", err)
	}
	frame := freeBSDGratuitousARPFrame(iface.HardwareAddr, net.ParseIP(ip).To4())
	if err := ctx.Err(); err != nil {
		return err
	}
	for i := 0; i < 3; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := freeBSDSAMWriteBPF(fd, frame); err != nil {
			return fmt.Errorf("write FreeBSD BPF gratuitous ARP: %w", err)
		}
		if i == 2 {
			break
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func samIPv4Address(address string) (string, error) {
	ip, _, err := net.ParseCIDR(address)
	if err != nil {
		ip = net.ParseIP(address)
	}
	if ip == nil || ip.To4() == nil {
		return "", fmt.Errorf("invalid IPv4 address %q", address)
	}
	return ip.To4().String(), nil
}

func freeBSDARPEntry(ctx context.Context, address, ifname string) (string, bool, error) {
	// FreeBSD arp(8) accepts a hostname/address for the single-entry form;
	// -a selects all entries and cannot be combined with that operand.
	out, err := freeBSDSAMRunCommand(ctx, "arp", "-n", "-i", ifname, address)
	if err != nil {
		text := strings.TrimSpace(string(out))
		// FreeBSD arp(8) reports an absent single entry as a nonzero command
		// with either the unscoped "ADDRESS (ADDRESS) -- no entry" form or,
		// when -i is used, that exact form followed by " on IFACE". Those are
		// the only nonzero lookup outcomes safe to normalize; every other error
		// leaves ownership unknown and remains fail-closed.
		if freeBSDARPEntryAbsent(text, address, ifname) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("observe ARP %s: %w: %s", address, err, text)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "("+address+")") {
			return line, true, nil
		}
	}
	return "", false, nil
}

func freeBSDARPEntryAbsent(text, address, ifname string) bool {
	base := address + " (" + address + ") -- no entry"
	return text == base || text == base+" on "+ifname
}

func freeBSDPublishedARPMatches(entry, ifname string) bool {
	return strings.Contains(entry, " on "+ifname+" ") && strings.Contains(strings.ToLower(entry), "published")
}

func freeBSDInterfaceHasIPv4(out []byte, address string) bool {
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "inet" && fields[1] == address {
			return true
		}
	}
	return false
}

func freeBSDSAMForwardRules(paths []sam.CaptureAction) ([]string, error) {
	var rules []string
	for _, path := range paths {
		address, err := samIPv4Address(path.Address)
		if err != nil {
			return nil, err
		}
		capture := strings.TrimSpace(path.Interface)
		peer := strings.TrimSpace(path.PeerInterface)
		if capture == "" || peer == "" {
			return nil, fmt.Errorf("SAM PF path %s requires capture and overlay interfaces", path.ClaimName)
		}
		if path.Kind == "forward-local-path" {
			rules = append(rules,
				fmt.Sprintf("pass quick on %s inet from %s/32 to any", peer, address),
				fmt.Sprintf("pass quick on %s inet from any to %s/32", capture, address),
			)
		} else {
			rules = append(rules,
				fmt.Sprintf("pass quick on %s inet from any to %s/32", capture, address),
				fmt.Sprintf("pass quick on %s inet from %s/32 to any", peer, address),
			)
		}
	}
	return rules, nil
}

func openFreeBSDSAMBPF() (int, error) {
	if fd, err := unix.Open("/dev/bpf", unix.O_RDWR, 0); err == nil {
		return fd, nil
	}
	var last error
	for i := 0; i < 256; i++ {
		fd, err := unix.Open(fmt.Sprintf("/dev/bpf%d", i), unix.O_RDWR, 0)
		if err == nil {
			return fd, nil
		}
		last = err
	}
	return -1, fmt.Errorf("open FreeBSD BPF device: %w", last)
}

func attachFreeBSDSAMBPF(fd int, ifname string) error {
	var req [32]byte
	if len(ifname) >= len(req) {
		return fmt.Errorf("interface name too long: %s", ifname)
	}
	copy(req[:], ifname)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.BIOCSETIF), uintptr(unsafe.Pointer(&req[0])))
	if errno != 0 {
		return os.NewSyscallError("BIOCSETIF", errno)
	}
	return nil
}

func freeBSDGratuitousARPFrame(mac net.HardwareAddr, ip net.IP) []byte {
	frame := make([]byte, 42)
	copy(frame[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(frame[6:12], mac)
	binary.BigEndian.PutUint16(frame[12:14], 0x0806)
	binary.BigEndian.PutUint16(frame[14:16], 1)
	binary.BigEndian.PutUint16(frame[16:18], 0x0800)
	frame[18] = 6
	frame[19] = 4
	binary.BigEndian.PutUint16(frame[20:22], 1)
	copy(frame[22:28], mac)
	copy(frame[28:32], ip)
	copy(frame[38:42], ip)
	return frame
}
