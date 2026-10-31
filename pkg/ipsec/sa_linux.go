// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package ipsec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"syscall"

	"github.com/vishvananda/netlink"
)

type linuxSAs struct{}

func defaultNativeSAs() nativeSAStore { return linuxSAs{} }

func (linuxSAs) Scope(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	ns, err := os.Stat("/proc/self/ns/net")
	if err != nil {
		return "", err
	}
	stat, ok := ns.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("IPsec network namespace identity unavailable")
	}
	return fmt.Sprintf("%s/%d/%d", strings.TrimSpace(string(boot)), stat.Dev, stat.Ino), nil
}

func nativeSAKey(id saID) (*netlink.XfrmState, error) {
	if !id.valid() {
		return nil, errors.New("invalid recorded IPsec SA identity")
	}
	protocol := netlink.XFRM_PROTO_ESP
	if id.Protocol == "AH" {
		protocol = netlink.XFRM_PROTO_AH
	}
	state := &netlink.XfrmState{Src: net.ParseIP(id.Source), Dst: net.ParseIP(id.Destination), Proto: protocol, Spi: int(id.SPI), Ifid: int(id.IfID)}
	if id.Mark != 0 || id.Mask != 0 {
		state.Mark = &netlink.XfrmMark{Value: id.Mark, Mask: id.Mask}
	}
	return state, nil
}

func (n linuxSAs) Observe(ctx context.Context, id saID) (saReceipt, bool, error) {
	scope, err := n.Scope(ctx)
	if err != nil {
		return saReceipt{}, false, err
	}
	key, err := nativeSAKey(id)
	if err != nil {
		return saReceipt{}, false, err
	}
	state, err := netlink.XfrmStateGet(key)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ESRCH) {
			after, scopeErr := n.Scope(ctx)
			if scopeErr != nil {
				return saReceipt{}, false, scopeErr
			}
			if scope != after {
				return saReceipt{}, false, errors.New("IPsec kernel scope changed during absent observation")
			}
			return saReceipt{}, false, nil
		}
		return saReceipt{}, false, fmt.Errorf("observe IPsec kernel SA: %w", err)
	}
	after, err := n.Scope(ctx)
	if err != nil {
		return saReceipt{}, false, err
	}
	if scope != after {
		return saReceipt{}, false, errors.New("IPsec kernel scope changed during observation")
	}
	actual := saID{Source: state.Src.String(), Destination: state.Dst.String(), Protocol: strings.ToUpper(state.Proto.String()), SPI: uint32(state.Spi), IfID: uint32(state.Ifid)}
	if state.Mark != nil {
		actual.Mark, actual.Mask = state.Mark.Value, state.Mark.Mask
	}
	if actual != id || state.Statistics.AddTime == 0 {
		return saReceipt{}, false, errors.New("IPsec kernel SA observation did not match exact identity")
	}
	// Sequence/replay and traffic/use counters change during ordinary forwarding.
	// Hash only immutable configuration and installation time; never persist keys.
	stable := *state
	stable.Statistics = netlink.XfrmStateStats{AddTime: state.Statistics.AddTime}
	stable.Replay = nil
	data, err := json.Marshal(stable)
	if err != nil {
		return saReceipt{}, false, err
	}
	hash := sha256.Sum256(data)
	return saReceipt{ID: actual, Scope: scope, ReqID: uint32(state.Reqid), AddTime: state.Statistics.AddTime, Fingerprint: hex.EncodeToString(hash[:])}, true, nil
}

func (n linuxSAs) Delete(ctx context.Context, expected saReceipt) error {
	if !expected.valid() {
		return errors.New("invalid native IPsec ownership receipt")
	}
	scope, err := n.Scope(ctx)
	if err != nil {
		return err
	}
	if scope != expected.Scope {
		return errors.New("recorded IPsec SA belongs to a different kernel scope")
	}
	current, found, err := n.Observe(ctx, expected.ID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if current != expected {
		return errors.New("refuse to delete replaced IPsec SA")
	}
	key, err := nativeSAKey(expected.ID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := netlink.XfrmStateDel(key); err != nil && !errors.Is(err, syscall.ENOENT) && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("delete recorded IPsec kernel SA: %w", err)
	}
	return nil
}

func (n linuxSAs) ForEndpoints(ctx context.Context, local, remote string) ([]saID, error) {
	a, err := netip.ParseAddr(local)
	if err != nil {
		return nil, errors.New("IPsec local endpoint observation is unknown")
	}
	b, err := netip.ParseAddr(remote)
	if err != nil {
		return nil, errors.New("IPsec remote endpoint observation is unknown")
	}
	scope, err := n.Scope(ctx)
	if err != nil {
		return nil, err
	}
	states, err := netlink.XfrmStateList(netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("observe remaining IPsec kernel SAs: %w", err)
	}
	after, err := n.Scope(ctx)
	if err != nil {
		return nil, err
	}
	if scope != after {
		return nil, errors.New("IPsec kernel scope changed during SA inventory")
	}
	var result []saID
	for _, state := range states {
		src, ok := netip.AddrFromSlice(state.Src)
		if !ok {
			continue
		}
		dst, ok := netip.AddrFromSlice(state.Dst)
		if !ok {
			continue
		}
		src, dst = src.Unmap(), dst.Unmap()
		if !((src == a.Unmap() && dst == b.Unmap()) || (src == b.Unmap() && dst == a.Unmap())) {
			continue
		}
		if state.Proto != netlink.XFRM_PROTO_ESP && state.Proto != netlink.XFRM_PROTO_AH {
			continue
		}
		id := saID{Source: src.String(), Destination: dst.String(), Protocol: strings.ToUpper(state.Proto.String()), SPI: uint32(state.Spi), IfID: uint32(state.Ifid)}
		if state.Mark != nil {
			id.Mark, id.Mask = state.Mark.Value, state.Mark.Mask
		}
		result = append(result, id)
	}
	return result, nil
}
