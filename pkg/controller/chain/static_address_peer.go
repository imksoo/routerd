// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/platform"
)

type staticAddressPeer struct {
	Address netip.Prefix
	Peer    netip.Addr
}

func (s staticAddressSnapshot) peerAddress() string {
	if s.Peer.IsValid() {
		return s.Peer.String()
	}
	return ""
}

func (s staticAddressSnapshot) owns(receipt staticAddressReceipt) bool {
	if !s.has(receipt.Address) || s.peerAddress() != receipt.PeerAddress {
		return false
	}
	if receipt.PeerAddress == "" {
		return true
	}
	for _, address := range s.Peers {
		if address.Address.String() == receipt.Address {
			return address.Peer.String() == receipt.PeerAddress
		}
	}
	return false
}

func staticAddressAddCommand(osName platform.OS, receipt staticAddressReceipt) (string, []string) {
	if osName == platform.OSFreeBSD && receipt.PeerAddress != "" {
		return "ifconfig", []string{receipt.IfName, "inet", receipt.Address, receipt.PeerAddress, "alias"}
	}
	return ipv4StaticAddressApplyCommand(osName, receipt.IfName, receipt.Address)
}

func parseFreeBSDStaticPeers(name string, data []byte) (staticAddressSnapshot, error) {
	var snapshot staticAddressSnapshot
	if !strings.HasPrefix(strings.TrimSpace(string(data)), name+":") {
		return snapshot, errors.New("native P2P interface observation has a different name")
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "inet" {
			continue
		}
		if len(fields) < 4 || fields[2] != "-->" {
			return snapshot, errors.New("P2P address observation has no peer")
		}
		local, err := netip.ParseAddr(fields[1])
		if err != nil || !local.Is4() {
			return snapshot, errors.New("invalid native P2P local address")
		}
		var peer netip.Addr
		bits := -1
		if prefix, err := netip.ParsePrefix(fields[3]); err == nil {
			peer, bits = prefix.Addr(), prefix.Bits()
		} else {
			peer, err = netip.ParseAddr(fields[3])
			if err != nil {
				return snapshot, errors.New("invalid native P2P peer")
			}
		}
		if !peer.Is4() {
			return snapshot, errors.New("native P2P peer is not IPv4")
		}
		for i := 4; i+1 < len(fields); i++ {
			if fields[i] != "netmask" {
				continue
			}
			var mask net.IPMask
			if strings.HasPrefix(fields[i+1], "0x") {
				value, err := strconv.ParseUint(fields[i+1][2:], 16, 32)
				if err != nil {
					return snapshot, errors.New("invalid native P2P netmask")
				}
				mask = make(net.IPMask, 4)
				binary.BigEndian.PutUint32(mask, uint32(value))
			} else {
				value, err := netip.ParseAddr(fields[i+1])
				if err != nil || !value.Is4() {
					return snapshot, errors.New("invalid native P2P netmask")
				}
				bytes := value.As4()
				mask = net.IPMask(bytes[:])
			}
			var size int
			bits, size = mask.Size()
			if size != 32 {
				return snapshot, errors.New("noncontiguous native P2P netmask")
			}
			break
		}
		if bits < 0 {
			return snapshot, errors.New("native P2P address has no netmask")
		}
		prefix := netip.PrefixFrom(local, bits)
		snapshot.Addresses = append(snapshot.Addresses, prefix)
		snapshot.Peers = append(snapshot.Peers, staticAddressPeer{Address: prefix, Peer: peer})
		if peer.IsUnspecified() {
			continue
		}
		if snapshot.Peer.IsValid() && snapshot.Peer != peer {
			return snapshot, errors.New("native P2P peers are ambiguous")
		}
		snapshot.Peer = peer
	}
	if !snapshot.Peer.IsValid() {
		return snapshot, errors.New("native P2P interface has no usable existing IPv4 peer")
	}
	return snapshot, nil
}

func observeFreeBSDStaticPeers(ctx context.Context, iface net.Interface) (staticAddressSnapshot, error) {
	scope, err := platform.KernelNetworkScope(ctx)
	if err != nil {
		return staticAddressSnapshot{}, err
	}
	out, err := exec.CommandContext(ctx, "ifconfig", iface.Name).CombinedOutput()
	if err != nil {
		return staticAddressSnapshot{}, fmt.Errorf("observe P2P interface %s: %w: %s", iface.Name, err, strings.TrimSpace(string(out)))
	}
	snapshot, err := parseFreeBSDStaticPeers(iface.Name, out)
	if err != nil {
		return snapshot, err
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return snapshot, err
	}
	present := false
	for _, current := range interfaces {
		if current.Name == iface.Name {
			present = current.Index == iface.Index && current.Flags&net.FlagPointToPoint != 0
			break
		}
	}
	freshScope, err := platform.KernelNetworkScope(ctx)
	if err != nil || !present || scope != freshScope {
		return snapshot, errors.Join(err, errors.New("P2P interface identity changed during peer observation"))
	}
	snapshot.Index, snapshot.Scope = iface.Index, scope
	return snapshot, ctx.Err()
}
