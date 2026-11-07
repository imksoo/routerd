// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/sam"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type netlinkSAMProxyNeighborApplier struct{}

var samForwardPathMu sync.Mutex

type samForwardPathOps struct {
	runIPTables   func(args ...string) ([]byte, error)
	setSysctl     func(key, value string) error
	sysctlPresent func(key string) (bool, error)
}

func defaultSAMProxyNeighborApplier() samProxyNeighborApplier {
	return netlinkSAMProxyNeighborApplier{}
}

func defaultSAMGratuitousARPAnnouncer() samGratuitousARPAnnouncer {
	return packetSAMGratuitousARPAnnouncer{}
}

type packetSAMGratuitousARPAnnouncer struct{}

func (netlinkSAMProxyNeighborApplier) SetProxyARP(ctx context.Context, ifname string, enabled, owned bool, beforeAcquire func() error) (samProxyARPApplyResult, error) {
	return samSetOwnedInterfaceSysctl(ctx, ifname, "proxy_arp", enabled, owned, beforeAcquire)
}

func (netlinkSAMProxyNeighborApplier) SetAcceptLocal(ctx context.Context, ifname string, enabled, owned bool, beforeAcquire func() error) (samProxyARPApplyResult, error) {
	return samSetOwnedInterfaceSysctl(ctx, ifname, "accept_local", enabled, owned, beforeAcquire)
}

func samSetOwnedInterfaceSysctl(ctx context.Context, ifname, setting string, enabled, owned bool, beforeAcquire func() error) (samProxyARPApplyResult, error) {
	if err := sam.ValidateCaptureInterface(ifname); err != nil {
		return samProxyARPApplyResult{}, err
	}
	key := "net.ipv4.conf." + ifname + "." + setting
	if !enabled {
		present, err := samSysctlPresent(key)
		if err != nil {
			return samProxyARPApplyResult{}, err
		}
		if !present {
			return samProxyARPApplyResult{}, nil
		}
	}
	return applySAMOwnedBinarySetting(key, enabled, owned, func() (string, error) { return samReadBinarySysctl(ctx, key) }, func(value string) error { return samSetSysctl(ctx, key, value) }, beforeAcquire)
}

func (netlinkSAMProxyNeighborApplier) SetIPForwarding(ctx context.Context, enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	return samSetSysctl(ctx, "net.ipv4.ip_forward", value)
}

func samSetSysctl(ctx context.Context, key, value string) error {
	path, err := samSysctlPath(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if value != "0" && value != "1" {
		return fmt.Errorf("SAM sysctl %s requires a binary value", key)
	}
	if err := os.WriteFile(path, []byte(value), 0); err != nil {
		return fmt.Errorf("write %s: %w", key, err)
	}
	return nil
}

func samReadBinarySysctl(ctx context.Context, key string) (string, error) {
	path, err := samSysctlPath(key)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	out, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", key, err)
	}
	value := strings.TrimSpace(string(out))
	if value != "0" && value != "1" {
		return "", fmt.Errorf("sysctl %s has unexpected value %q", key, value)
	}
	return value, nil
}

func (packetSAMGratuitousARPAnnouncer) SendGratuitousARP(ctx context.Context, address, ifname string) error {
	ip, _, err := net.ParseCIDR(address)
	if err != nil {
		ip = net.ParseIP(address)
	}
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("invalid IPv4 address %q", address)
	}
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return fmt.Errorf("lookup interface %s: %w", ifname, err)
	}
	if len(iface.HardwareAddr) != 6 {
		return fmt.Errorf("interface %s has non-ethernet hardware address %q", ifname, iface.HardwareAddr)
	}
	frame := buildGratuitousARPFrame(iface.HardwareAddr, ip.To4())
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC, int(htons(unix.ETH_P_ARP)))
	if err != nil {
		return fmt.Errorf("open AF_PACKET ARP socket: %w", err)
	}
	defer unix.Close(fd)
	addr := &unix.SockaddrLinklayer{
		Protocol: htons(unix.ETH_P_ARP),
		Ifindex:  iface.Index,
		Halen:    6,
		Addr:     [8]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	}
	for i := 0; i < 3; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := unix.Sendto(fd, frame, 0, addr); err != nil {
			return fmt.Errorf("send gratuitous ARP frame: %w", err)
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

func buildGratuitousARPFrame(mac net.HardwareAddr, ip net.IP) []byte {
	frame := make([]byte, 42)
	copy(frame[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(frame[6:12], mac)
	binary.BigEndian.PutUint16(frame[12:14], unix.ETH_P_ARP)
	binary.BigEndian.PutUint16(frame[14:16], 1)             // Ethernet
	binary.BigEndian.PutUint16(frame[16:18], unix.ETH_P_IP) // IPv4
	frame[18] = 6
	frame[19] = 4
	binary.BigEndian.PutUint16(frame[20:22], 1) // ARP request
	copy(frame[22:28], mac)
	copy(frame[28:32], ip.To4())
	copy(frame[38:42], ip.To4())
	return frame
}

func htons(value uint16) uint16 {
	return (value<<8)&0xff00 | value>>8
}

func (netlinkSAMProxyNeighborApplier) EnsureProxyNeighbor(_ context.Context, address, ifname string, owned bool, beforeAcquire func() error) error {
	_, expected, err := samProxyNeighbor(address, ifname)
	if err != nil {
		return err
	}
	present, err := samExactProxyNeighbor(expected)
	if err != nil {
		return err
	}
	if present {
		if !owned {
			return fmt.Errorf("foreign proxy neighbor %s dev %s: refusing adoption", address, ifname)
		}
		return nil
	}
	if err := recordSAMAcquisition(beforeAcquire); err != nil {
		return err
	}
	return netlink.NeighAdd(expected)
}

func (netlinkSAMProxyNeighborApplier) DeleteProxyNeighbor(_ context.Context, address, ifname string) error {
	_, expected, err := samProxyNeighbor(address, ifname)
	if err != nil {
		var missing netlink.LinkNotFoundError
		if errors.As(err, &missing) {
			return nil
		}
		return err
	}
	present, err := samExactProxyNeighbor(expected)
	if err != nil || !present {
		return err
	}
	if err := netlink.NeighDel(expected); err != nil && !isNetlinkNotFound(err) {
		return err
	}
	return nil
}

func samExactProxyNeighbor(expected *netlink.Neigh) (bool, error) {
	neighbors, err := netlink.NeighProxyList(expected.LinkIndex, netlink.FAMILY_V4)
	if err != nil {
		return false, err
	}
	return samMatchProxyNeighbors(expected, neighbors)
}

func samMatchProxyNeighbors(expected *netlink.Neigh, neighbors []netlink.Neigh) (bool, error) {
	present := false
	for _, neighbor := range neighbors {
		if !neighbor.IP.Equal(expected.IP) {
			continue
		}
		if present || neighbor.LinkIndex != expected.LinkIndex || neighbor.Flags != expected.Flags || neighbor.State != expected.State || len(neighbor.HardwareAddr) != 0 {
			return false, fmt.Errorf("foreign proxy-neighbor attributes for %s on index %d", expected.IP, expected.LinkIndex)
		}
		present = true
	}
	return present, nil
}

func (netlinkSAMProxyNeighborApplier) EnsureOSAddressAbsent(_ context.Context, address string) error {
	ip, _, err := net.ParseCIDR(address)
	if err != nil {
		ip = net.ParseIP(address)
	}
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("invalid IPv4 address %q", address)
	}
	links, err := netlink.LinkList()
	if err != nil {
		return err
	}
	for _, link := range links {
		addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
		if err != nil {
			return err
		}
		for _, addr := range addrs {
			if addr.IP == nil || !addr.IP.Equal(ip.To4()) {
				continue
			}
			return fmt.Errorf("foreign OS address %s remains configured on %s", ip, link.Attrs().Name)
		}
	}
	return nil
}

func (netlinkSAMProxyNeighborApplier) ReconcileForwardPaths(ctx context.Context, paths []sam.CaptureAction, setAcceptLocal func(context.Context, string, bool) error) error {
	samForwardPathMu.Lock()
	defer samForwardPathMu.Unlock()

	ops := samForwardPathOps{
		runIPTables: func(args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, "iptables", args...).CombinedOutput()
		},
		setSysctl: func(key, value string) error {
			if !strings.HasPrefix(key, "net.ipv4.conf.") || !strings.HasSuffix(key, ".accept_local") || setAcceptLocal == nil {
				return fmt.Errorf("SAM forwarding requires owned accept_local effects")
			}
			iface := strings.TrimSuffix(strings.TrimPrefix(key, "net.ipv4.conf."), ".accept_local")
			return setAcceptLocal(ctx, iface, value == "1")
		},
		sysctlPresent: samSysctlPresent,
	}
	return reconcileSAMForwardPaths(paths, ops)
}

func reconcileSAMForwardPaths(paths []sam.CaptureAction, ops samForwardPathOps) error {
	const chain = "routerd_sam_forward"
	run := func(args ...string) error {
		out, err := ops.runIPTables(args...)
		if err != nil {
			return fmt.Errorf("iptables %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// Empty desired state is a teardown request. Do not create or attach a
	// forwarding chain merely to clean it: this controller runs for routers
	// that have no SAM forwarding intent. When the routerd-owned chain exists,
	// continue through the normal stale-rule and accept_local cleanup below.
	if len(paths) == 0 {
		out, err := ops.runIPTables("-S", chain)
		if err != nil {
			if samForwardChainAbsent(chain, err, out, ops.runIPTables) {
				return nil
			}
			return fmt.Errorf("iptables -S %s: %w: %s", chain, err, strings.TrimSpace(string(out)))
		}
	}
	_ = run("-N", chain)
	if err := run("-C", "FORWARD", "-j", chain); err != nil {
		if insertErr := run("-I", "FORWARD", "1", "-j", chain); insertErr != nil {
			return insertErr
		}
	}
	currentIfaces, err := currentForwardPathInterfaces(chain, ops.runIPTables)
	if err != nil {
		return err
	}
	sort.SliceStable(paths, func(i, j int) bool {
		if paths[i].Address != paths[j].Address {
			return paths[i].Address < paths[j].Address
		}
		if paths[i].Interface != paths[j].Interface {
			return paths[i].Interface < paths[j].Interface
		}
		return paths[i].PeerInterface < paths[j].PeerInterface
	})
	desired := map[string][]string{}
	acceptLocal := map[string]bool{}
	for _, path := range paths {
		address := strings.TrimSpace(path.Address)
		captureIface := strings.TrimSpace(path.Interface)
		tunnelIface := strings.TrimSpace(path.PeerInterface)
		if address == "" || captureIface == "" || tunnelIface == "" {
			continue
		}
		acceptLocal[captureIface] = true
		acceptLocal[tunnelIface] = true
		if path.Kind == "forward-local-path" {
			addDesiredIPTablesRule(desired, "-i", tunnelIface, "-o", captureIface, "-d", address, "-j", "ACCEPT")
			addDesiredIPTablesRule(desired, "-i", captureIface, "-o", tunnelIface, "-s", address, "-j", "ACCEPT")
		} else {
			addDesiredIPTablesRule(desired, "-i", captureIface, "-o", tunnelIface, "-d", address, "-j", "ACCEPT")
			addDesiredIPTablesRule(desired, "-i", tunnelIface, "-o", captureIface, "-s", address, "-j", "ACCEPT")
		}
	}
	for ifname := range acceptLocal {
		if err := ops.setSysctl("net.ipv4.conf."+ifname+".accept_local", "1"); err != nil {
			return err
		}
	}
	for ifname := range currentIfaces {
		if acceptLocal[ifname] {
			continue
		}
		if err := samSetSysctlIfPresent("net.ipv4.conf."+ifname+".accept_local", "0", ops); err != nil {
			return err
		}
	}
	for _, rule := range sortedIPTablesRules(desired) {
		if err := ensureIPTablesRule(chain, ops.runIPTables, rule...); err != nil {
			return err
		}
	}
	if err := deleteStaleIPTablesRules(chain, desired, ops.runIPTables); err != nil {
		return err
	}
	return nil
}

func samForwardChainAbsent(chain string, err error, output []byte, runIPTables func(...string) ([]byte, error)) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	message := strings.ToLower(err.Error() + " " + string(output))
	if strings.Contains(message, "no chain") || strings.Contains(message, "chain/target/match by that name") {
		return true
	}
	// iptables-nft 1.8.7 reports an absent named chain as incompatible too.
	// That message alone is not proof of absence: a native nft rule in an
	// existing chain can produce the same error. Only a successful whole-table
	// listing that does not contain this exact chain permits a no-op.
	if !strings.Contains(string(output), "chain `"+chain+"' in table `filter' is incompatible") {
		return false
	}
	rules, listErr := runIPTables("-S")
	if listErr != nil {
		return false
	}
	for _, line := range strings.Split(string(rules), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == chain {
			switch fields[0] {
			case "-N", "-P", "-A":
				return false
			}
		}
	}
	return true
}

func samSysctlPresent(key string) (bool, error) {
	path, err := samSysctlPath(key)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func samSysctlPath(key string) (string, error) {
	if key == "net.ipv4.ip_forward" {
		return "/proc/sys/net/ipv4/ip_forward", nil
	}
	if rest, ok := strings.CutPrefix(key, "net.ipv4.conf."); ok {
		for _, setting := range []string{"proxy_arp", "accept_local"} {
			if ifname, ok := strings.CutSuffix(rest, "."+setting); ok {
				if err := sam.ValidateCaptureInterface(ifname); err != nil {
					return "", err
				}
				return "/proc/sys/net/ipv4/conf/" + ifname + "/" + setting, nil
			}
		}
	}
	return "", fmt.Errorf("unsupported SAM sysctl key %q", key)
}

func samSetSysctlIfPresent(key, value string, ops samForwardPathOps) error {
	if !strings.HasPrefix(key, "net.ipv4.conf.") {
		return ops.setSysctl(key, value)
	}
	present, err := ops.sysctlPresent(key)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	return ops.setSysctl(key, value)
}

func addDesiredIPTablesRule(desired map[string][]string, rule ...string) {
	normalized := append([]string(nil), rule...)
	desired[iptablesRuleKey(normalized)] = normalized
}

func sortedIPTablesRules(rules map[string][]string) [][]string {
	keys := make([]string, 0, len(rules))
	for key := range rules {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([][]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, rules[key])
	}
	return out
}

func ensureIPTablesRule(chain string, runIPTables func(args ...string) ([]byte, error), rule ...string) error {
	checkArgs := append([]string{"-C", chain}, rule...)
	if _, err := runIPTables(checkArgs...); err == nil {
		return nil
	}
	addArgs := append([]string{"-A", chain}, rule...)
	out, err := runIPTables(addArgs...)
	if err != nil {
		return fmt.Errorf("iptables %s: %w: %s", strings.Join(addArgs, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func currentForwardPathInterfaces(chain string, runIPTables func(args ...string) ([]byte, error)) (map[string]bool, error) {
	out, err := runIPTables("-S", chain)
	if err != nil {
		return nil, fmt.Errorf("iptables -S %s: %w: %s", chain, err, strings.TrimSpace(string(out)))
	}
	ifaces := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		prefix := "-A " + chain + " "
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		for _, ifname := range forwardPathInterfacesFromRule(strings.Fields(strings.TrimPrefix(line, prefix))) {
			ifaces[ifname] = true
		}
	}
	return ifaces, nil
}

func forwardPathInterfacesFromRule(rule []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(ifname string) {
		ifname = strings.TrimSpace(ifname)
		if ifname == "" || seen[ifname] {
			return
		}
		seen[ifname] = true
		out = append(out, ifname)
	}
	for i := 0; i < len(rule); i++ {
		token := rule[i]
		if token != "-i" && token != "--in-interface" && token != "-o" && token != "--out-interface" {
			continue
		}
		if i+1 >= len(rule) {
			continue
		}
		add(rule[i+1])
		i++
	}
	return out
}

func deleteStaleIPTablesRules(chain string, desired map[string][]string, runIPTables func(args ...string) ([]byte, error)) error {
	out, err := runIPTables("-S", chain)
	if err != nil {
		return fmt.Errorf("iptables -S %s: %w: %s", chain, err, strings.TrimSpace(string(out)))
	}
	seenDesired := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		prefix := "-A " + chain + " "
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rule := strings.Fields(strings.TrimPrefix(line, prefix))
		if len(rule) == 0 {
			continue
		}
		key := iptablesRuleKey(rule)
		if _, ok := desired[key]; ok && !seenDesired[key] {
			seenDesired[key] = true
			continue
		}
		deleteArgs := append([]string{"-D", chain}, rule...)
		if out, err := runIPTables(deleteArgs...); err != nil {
			return fmt.Errorf("iptables %s: %w: %s", strings.Join(deleteArgs, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func iptablesRuleKey(rule []string) string {
	var src, dst, in, out, jump string
	rest := make([]string, 0, len(rule))
	for i := 0; i < len(rule); i++ {
		token := rule[i]
		value := ""
		if i+1 < len(rule) {
			value = rule[i+1]
		}
		switch token {
		case "-s", "--source":
			if value != "" {
				src = value
				i++
				continue
			}
		case "-d", "--destination":
			if value != "" {
				dst = value
				i++
				continue
			}
		case "-i", "--in-interface":
			if value != "" {
				in = value
				i++
				continue
			}
		case "-o", "--out-interface":
			if value != "" {
				out = value
				i++
				continue
			}
		case "-j", "--jump":
			if value != "" {
				jump = value
				i++
				continue
			}
		}
		rest = append(rest, token)
	}
	key := make([]string, 0, len(rule))
	if src != "" {
		key = append(key, "-s", src)
	}
	if dst != "" {
		key = append(key, "-d", dst)
	}
	if in != "" {
		key = append(key, "-i", in)
	}
	if out != "" {
		key = append(key, "-o", out)
	}
	key = append(key, rest...)
	if jump != "" {
		key = append(key, "-j", jump)
	}
	return strings.Join(key, "\x00")
}

func samProxyNeighbor(address, ifname string) (netlink.Link, *netlink.Neigh, error) {
	ip, _, err := net.ParseCIDR(address)
	if err != nil {
		ip = net.ParseIP(address)
	}
	if ip == nil || ip.To4() == nil {
		return nil, nil, fmt.Errorf("invalid IPv4 address %q", address)
	}
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return nil, nil, err
	}
	return link, &netlink.Neigh{
		LinkIndex: link.Attrs().Index,
		Family:    netlink.FAMILY_V4,
		// Linux pneigh_fill_info reports NUD_NONE for proxy entries; they do
		// not carry the normal neighbor-cache reachability state.
		State: netlink.NUD_NONE,
		Flags: netlink.NTF_PROXY,
		IP:    ip.To4(),
	}, nil
}

func isNetlinkNotFound(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such file") || strings.Contains(message, "not found")
}
