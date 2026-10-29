// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

var errPDAddressAuthorityChanged = errors.New("PD prefix authority changed or expired")

const infinitePDLifetime = uint32(0xffffffff)

// Round down finite protocol lifetimes. A kernel address must never outlive
// its upstream authority because reconciliation rounded a fractional second up.
func remainingPDLifetime(acquired time.Time, seconds uint32, now time.Time) uint32 {
	if seconds == infinitePDLifetime {
		return infinitePDLifetime
	}
	remaining := acquired.Add(time.Duration(seconds) * time.Second).Sub(now)
	if remaining <= 0 {
		return 0
	}
	return uint32(remaining / time.Second)
}

func currentPDAddressLifetimes(lease routerstate.PDLease, now time.Time, deprecated bool) (uint32, uint32, error) {
	if !lease.HasFreshTransactionEvidence(now) || lease.PreferredSeconds > lease.ValidSeconds {
		return 0, 0, fmt.Errorf("PD prefix has no current lifetime authority")
	}
	valid := remainingPDLifetime(lease.LastReplyAt, lease.ValidSeconds, now)
	if valid == 0 {
		return 0, 0, fmt.Errorf("PD prefix valid lifetime exhausted")
	}
	preferred := remainingPDLifetime(lease.LastReplyAt, lease.PreferredSeconds, now)
	if deprecated {
		preferred = 0
	}
	if preferred > valid {
		preferred = valid
	}
	return preferred, valid, nil
}

func pdLifetimeDeadline(acquired time.Time, seconds uint32) string {
	if seconds == infinitePDLifetime {
		return "infinite"
	}
	return acquired.Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339Nano)
}

func ipv6DelegatedAddressApplyCommand(osName platform.OS, ifname, address string, preferred, valid uint32) (string, []string) {
	prefText, validText := strconv.FormatUint(uint64(preferred), 10), strconv.FormatUint(uint64(valid), 10)
	if osName == platform.OSFreeBSD {
		prefix, _ := netip.ParsePrefix(address)
		return "ifconfig", []string{ifname, "inet6", prefix.Addr().String(), "prefixlen", strconv.Itoa(prefix.Bits()), "pltime", prefText, "vltime", validText, "alias"}
	}
	if preferred == infinitePDLifetime {
		prefText = "forever"
	}
	if valid == infinitePDLifetime {
		validText = "forever"
	}
	return "ip", []string{"-6", "addr", "replace", address, "dev", ifname, "preferred_lft", prefText, "valid_lft", validText}
}

func (c LANAddressController) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c LANAddressController) currentAddressAuthority(pdName, prefix string) (routerstate.PDLease, error) {
	now := c.now()
	lease, found, err := livePDLease(c.Store, pdName, now)
	if err != nil {
		return lease, err
	}
	if !found {
		return lease, fmt.Errorf("%w: observation missing", errPDAddressAuthorityChanged)
	}
	if lease.Phase != "Bound" || lease.CurrentPrefix != prefix || !lease.HasFreshTransactionEvidence(now) {
		return lease, errPDAddressAuthorityChanged
	}
	return lease, nil
}

func (c LANAddressController) saveAddressPending(name, reason, message string) error {
	reader, ok := c.Store.(interface {
		ReadObjectStatus(string, string, string) (map[string]any, error)
	})
	if !ok {
		return fmt.Errorf("delegated address ownership reader unavailable")
	}
	current, err := reader.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", name)
	if err != nil {
		return err
	}
	next := copyStatusMap(current)
	for _, key := range []string{"ownerKey", "ownerRef", "ownerRefs", "lifecycleClass"} {
		delete(next, key)
	}
	next["phase"] = "Pending"
	if reason != "" {
		next["reason"] = reason
	} else {
		delete(next, "reason")
	}
	if message != "" {
		next["message"] = message
	} else {
		delete(next, "message")
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", name, next)
}

func (c LANAddressController) currentPreviousAddressAuthority(pdName, currentPrefix, address string, spec api.IPv6DelegatedAddressSpec) (managedPreviousDelegatedAddress, error) {
	lease, err := c.currentAddressAuthority(pdName, currentPrefix)
	if err != nil {
		return managedPreviousDelegatedAddress{}, err
	}
	previous, err := derivePreviousPDLeaseAddresses(lease, currentPrefix, spec, c.now())
	if err != nil {
		return managedPreviousDelegatedAddress{}, err
	}
	for _, candidate := range previous {
		if candidate.Address == address {
			return candidate, nil
		}
	}
	return managedPreviousDelegatedAddress{}, fmt.Errorf("previous delegated address %s no longer has canonical authority", address)
}
