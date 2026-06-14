// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/imksoo/routerd/pkg/sam"
	"github.com/vishvananda/netlink"
)

type netlinkSAMProxyNeighborApplier struct{}

var samForwardPathMu sync.Mutex

func defaultSAMProxyNeighborApplier() samProxyNeighborApplier {
	return netlinkSAMProxyNeighborApplier{}
}

func defaultSAMGratuitousARPAnnouncer() samGratuitousARPAnnouncer {
	return commandSAMGratuitousARPAnnouncer{}
}

type commandSAMGratuitousARPAnnouncer struct {
	Command func(context.Context, string, ...string) ([]byte, error)
}

func (netlinkSAMProxyNeighborApplier) SetProxyARP(ctx context.Context, ifname string, enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	key := "net.ipv4.conf." + ifname + ".proxy_arp"
	out, err := exec.CommandContext(ctx, "sysctl", "-w", key+"="+value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sysctl -w %s=%s: %w: %s", key, value, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (a commandSAMGratuitousARPAnnouncer) SendGratuitousARP(ctx context.Context, address, ifname string) error {
	ip, _, err := net.ParseCIDR(address)
	if err != nil {
		ip = net.ParseIP(address)
	}
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("invalid IPv4 address %q", address)
	}
	run := a.Command
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		}
	}
	if out, err := run(ctx, "arping", "-U", "-c", "3", "-I", ifname, ip.To4().String()); err != nil {
		return fmt.Errorf("arping gratuitous ARP: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (netlinkSAMProxyNeighborApplier) EnsureProxyNeighbor(_ context.Context, address, ifname string) error {
	link, neigh, err := samProxyNeighbor(address, ifname)
	if err != nil {
		return err
	}
	_ = link
	return netlink.NeighSet(neigh)
}

func (netlinkSAMProxyNeighborApplier) DeleteProxyNeighbor(_ context.Context, address, ifname string) error {
	_, neigh, err := samProxyNeighbor(address, ifname)
	if err != nil {
		return err
	}
	if err := netlink.NeighDel(neigh); err != nil && !isNetlinkNotFound(err) {
		return err
	}
	return nil
}

func (netlinkSAMProxyNeighborApplier) EnsureOSAddressAbsent(_ context.Context, address string) (samOSAddressDeassignResult, error) {
	ip, _, err := net.ParseCIDR(address)
	if err != nil {
		ip = net.ParseIP(address)
	}
	if ip == nil || ip.To4() == nil {
		return samOSAddressDeassignResult{}, fmt.Errorf("invalid IPv4 address %q", address)
	}
	result := samOSAddressDeassignResult{address: address}
	links, err := netlink.LinkList()
	if err != nil {
		return result, err
	}
	for _, link := range links {
		addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
		if err != nil {
			return result, err
		}
		for _, addr := range addrs {
			if addr.IP == nil || !addr.IP.Equal(ip.To4()) {
				continue
			}
			if err := netlink.AddrDel(link, &addr); err != nil && !isNetlinkNotFound(err) {
				return result, err
			}
			result.ifname = link.Attrs().Name
			result.removedThisReconcile = true
		}
	}
	return result, nil
}

func (netlinkSAMProxyNeighborApplier) ReconcileForwardPaths(ctx context.Context, paths []sam.CaptureAction) error {
	samForwardPathMu.Lock()
	defer samForwardPathMu.Unlock()

	const chain = "routerd_sam_forward"
	run := func(args ...string) error {
		out, err := exec.CommandContext(ctx, "iptables", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("iptables %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	_ = run("-N", chain)
	if err := run("-C", "FORWARD", "-j", chain); err != nil {
		if insertErr := run("-I", "FORWARD", "1", "-j", chain); insertErr != nil {
			return insertErr
		}
	}
	if err := run("-F", chain); err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
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
	for _, path := range paths {
		address := strings.TrimSpace(path.Address)
		captureIface := strings.TrimSpace(path.Interface)
		tunnelIface := strings.TrimSpace(path.PeerInterface)
		if address == "" || captureIface == "" || tunnelIface == "" {
			continue
		}
		if path.Kind == "forward-local-path" {
			if err := ensureIPTablesRule(ctx, chain, "-i", tunnelIface, "-o", captureIface, "-d", address, "-j", "ACCEPT"); err != nil {
				return err
			}
			if err := ensureIPTablesRule(ctx, chain, "-i", captureIface, "-o", tunnelIface, "-s", address, "-j", "ACCEPT"); err != nil {
				return err
			}
		} else {
			if err := ensureIPTablesRule(ctx, chain, "-i", captureIface, "-o", tunnelIface, "-d", address, "-j", "ACCEPT"); err != nil {
				return err
			}
			if err := ensureIPTablesRule(ctx, chain, "-i", tunnelIface, "-o", captureIface, "-s", address, "-j", "ACCEPT"); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureIPTablesRule(ctx context.Context, chain string, rule ...string) error {
	checkArgs := append([]string{"-C", chain}, rule...)
	if out, err := exec.CommandContext(ctx, "iptables", checkArgs...).CombinedOutput(); err == nil {
		return nil
	} else if strings.TrimSpace(string(out)) != "" {
		_ = out
	}
	addArgs := append([]string{"-A", chain}, rule...)
	out, err := exec.CommandContext(ctx, "iptables", addArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables %s: %w: %s", strings.Join(addArgs, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
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
		State:     netlink.NUD_PERMANENT,
		Flags:     netlink.NTF_PROXY,
		IP:        ip.To4(),
	}, nil
}

func isNetlinkNotFound(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such file") || strings.Contains(message, "not found")
}
