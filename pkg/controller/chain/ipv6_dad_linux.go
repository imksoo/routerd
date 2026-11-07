// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type linuxIPv6DADWatcher struct {
	updates chan netlink.AddrUpdate
	done    chan struct{}
	errors  chan error
	ifIndex int
	address netip.Prefix
}

func watchIPv6DAD(ifIndex int, address string) (ipv6DADWatcher, error) {
	prefix, err := netip.ParsePrefix(address)
	if err != nil {
		return nil, err
	}
	w := &linuxIPv6DADWatcher{updates: make(chan netlink.AddrUpdate, 64), done: make(chan struct{}), errors: make(chan error, 1), ifIndex: ifIndex, address: prefix}
	err = netlink.AddrSubscribeWithOptions(w.updates, w.done, netlink.AddrSubscribeOptions{ReceiveBufferSize: 1 << 20, ErrorCallback: func(err error) {
		select {
		case w.errors <- err:
		default:
		}
	}})
	if err != nil {
		return nil, err
	}
	return w, nil
}
func (w *linuxIPv6DADWatcher) Close() {
	close(w.done)
	// The dependency's delivery send is blocking. Drain through channel closure
	// to join its reader, including cancellation during an unrelated event burst.
	for range w.updates {
	}
}
func (w *linuxIPv6DADWatcher) Wait(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("IPv6 DAD result remains unknown: %w", ctx.Err())
		case err := <-w.errors:
			return false, fmt.Errorf("observe IPv6 DAD: %w", err)
		case update, ok := <-w.updates:
			if !ok {
				return false, fmt.Errorf("IPv6 DAD observation closed before completion")
			}
			ip, ok := netip.AddrFromSlice(update.LinkAddress.IP)
			bits, total := update.LinkAddress.Mask.Size()
			if !ok || total != 128 || update.LinkIndex != w.ifIndex || netip.PrefixFrom(ip, bits) != w.address {
				continue
			}
			// In particular, RTM_DELADDR carries DADFAILED for a finite address.
			if update.Flags&unix.IFA_F_DADFAILED != 0 {
				return true, nil
			}
			if !update.NewAddr {
				return false, fmt.Errorf("IPv6 address disappeared during DAD without a duplicate result")
			}
			if update.Flags&unix.IFA_F_TENTATIVE == 0 {
				return false, nil
			}
		}
	}
}
