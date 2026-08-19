// SPDX-License-Identifier: BSD-3-Clause

// Package sam lowers the typed local capture plan to OS-neutral actions.
package sam

import (
	"fmt"
	"math/bits"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/platform"
)

var captureInterfaceToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,62}$`)

type CaptureAction struct {
	Kind          string
	IntentID      string
	PoolRef       string
	PoolPrefix    string
	Address       string
	Interface     string
	PeerInterface string
	Key           string
	Value         string
	GratuitousARP bool
}

type CaptureGateStatus struct {
	Active             bool
	Type               string
	VirtualAddressRef  string
	VirtualAddressRole string
	Reason             string
	Message            string
}

// CaptureGateObservation is the typed fact needed to evaluate a capture
// gate. The controller shell reads and decodes persisted resource status;
// this pure lowering package never depends on a status store.
type CaptureGateObservation struct {
	VirtualAddressStatusAvailable bool
	VirtualAddressRole            string
}

// PlanLocalCaptureIntents is the sole Cloud SAM lowering path. Ownership,
// capture eligibility, and the kernel capture interface have already been
// decided by mobility.PoolPlan.
func PlanLocalCaptureIntents(intents []dynamicconfig.LocalCaptureIntent, targetOS platform.OS) ([]CaptureAction, error) {
	if targetOS != platform.OSLinux && targetOS != platform.OSFreeBSD {
		return nil, nil
	}
	var actions []CaptureAction
	forwarding := false
	addForwarding := func() {
		if forwarding {
			return
		}
		key := "net.ipv4.ip_forward"
		if targetOS == platform.OSFreeBSD {
			key = "net.inet.ip.forwarding"
		}
		actions = append(actions, CaptureAction{Kind: "sysctl", Key: key, Value: "1"})
		forwarding = true
	}
	proxyEnabled := map[string]bool{}
	for _, intent := range intents {
		intentID := strings.TrimSpace(intent.ID)
		if intentID == "" {
			return nil, fmt.Errorf("local capture intent requires id")
		}
		if strings.TrimSpace(intent.PoolRef) == "" {
			return nil, fmt.Errorf("local capture intent %q requires poolRef", intentID)
		}
		address, err := normalizeIPv4Host(intent.Address)
		if err != nil {
			return nil, fmt.Errorf("local capture intent %q: %w", intentID, err)
		}
		poolPrefix, err := canonicalCapturePoolPrefix(intent.PoolPrefix)
		if err != nil || !poolPrefix.Contains(mustParseIPv4HostAddress(address)) {
			return nil, fmt.Errorf("local capture intent %q poolPrefix %q does not contain address %q", intentID, intent.PoolPrefix, address)
		}
		captureType := strings.TrimSpace(intent.CaptureType)
		if captureType != "proxy-arp" && captureType != "provider-secondary-ip" {
			return nil, fmt.Errorf("local capture intent %q has unsupported captureType %q", intentID, captureType)
		}
		switch intent.Disposition {
		case dynamicconfig.CaptureProhibited, dynamicconfig.CaptureDesired, dynamicconfig.CaptureProtectExisting, dynamicconfig.CaptureRelease, dynamicconfig.CaptureHold:
		default:
			return nil, fmt.Errorf("local capture intent %q has unsupported disposition %q", intentID, intent.Disposition)
		}
		if captureType == "proxy-arp" || intent.Disposition != dynamicconfig.CaptureRelease {
			if err := ValidateCaptureInterface(intent.CaptureInterface); err != nil {
				return nil, fmt.Errorf("local capture intent %q captureInterface: %w", intentID, err)
			}
		}
		if captureType == "provider-secondary-ip" && intent.Disposition != dynamicconfig.CaptureRelease {
			if err := validateCaptureTunnelInterfaces(intent.TunnelInterfaces); err != nil {
				return nil, fmt.Errorf("local capture intent %q tunnelInterfaces: %w", intentID, err)
			}
		}
		if intent.Disposition == dynamicconfig.CaptureRelease {
			// A release is an explicit desired dataplane operation, not a
			// fallback status cleanup. Provider-secondary-IP capture must never
			// leave a formerly assigned address configured in the local OS.
			if captureType == "provider-secondary-ip" {
				actions = append(actions, CaptureAction{Kind: "deassign-os-address", IntentID: intentID, PoolRef: intent.PoolRef, PoolPrefix: poolPrefix.String(), Address: address})
			}
			continue
		}
		if intent.Disposition != dynamicconfig.CaptureDesired && intent.Disposition != dynamicconfig.CaptureProtectExisting {
			continue
		}
		iface := strings.TrimSpace(intent.CaptureInterface)
		addForwarding()
		switch captureType {
		case "proxy-arp":
			if targetOS == platform.OSLinux && !proxyEnabled[iface] {
				proxyEnabled[iface] = true
				actions = append(actions, CaptureAction{Kind: "sysctl", Key: "net.ipv4.conf." + iface + ".proxy_arp", Value: "1", Interface: iface})
			}
			if targetOS == platform.OSFreeBSD {
				actions = append(actions, CaptureAction{Kind: "deassign-os-address", IntentID: intentID, PoolRef: intent.PoolRef, PoolPrefix: poolPrefix.String(), Address: address})
			}
			actions = append(actions, CaptureAction{Kind: "proxy-neighbor", IntentID: intentID, PoolRef: intent.PoolRef, PoolPrefix: poolPrefix.String(), Address: address, Interface: iface, GratuitousARP: intent.GratuitousARP})
		case "provider-secondary-ip":
			actions = append(actions, CaptureAction{Kind: "deassign-os-address", IntentID: intentID, PoolRef: intent.PoolRef, PoolPrefix: poolPrefix.String(), Address: address})
			actions = append(actions, CaptureAction{Kind: "proxy-neighbor", IntentID: intentID, PoolRef: intent.PoolRef, PoolPrefix: poolPrefix.String(), Address: address, Interface: iface, GratuitousARP: intent.GratuitousARP})
			for _, tunnel := range intent.TunnelInterfaces {
				if tunnel = strings.TrimSpace(tunnel); tunnel != "" {
					actions = append(actions, CaptureAction{Kind: "forward-path", IntentID: intentID, PoolRef: intent.PoolRef, PoolPrefix: poolPrefix.String(), Address: address, Interface: iface, PeerInterface: tunnel})
				}
			}
		}
	}
	return actions, nil
}

func canonicalCapturePoolPrefix(value string) (netip.Prefix, error) {
	return dynamicconfig.ParseCanonicalIPv4Prefix(value)
}

func mustParseIPv4HostAddress(value string) netip.Addr {
	prefix, _ := netip.ParsePrefix(value)
	return prefix.Addr()
}

// ValidateCaptureInterface rejects pseudo sysctl namespaces and unsafe link
// tokens before a typed local capture can reach sysctl, netlink, PF, or
// iptables. It is intentionally OS-neutral because all supported dataplanes
// use the same names as command arguments.
func ValidateCaptureInterface(value string) error {
	if !captureInterfaceToken.MatchString(value) || value == "all" || value == "default" {
		return fmt.Errorf("%q is not a safe capture interface", value)
	}
	return nil
}

func validateCaptureTunnelInterfaces(values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if err := ValidateCaptureInterface(value); err != nil {
			return err
		}
		if seen[value] {
			return fmt.Errorf("duplicate tunnel interface %q", value)
		}
		seen[value] = true
	}
	return nil
}

func CaptureExcludesAddress(capture api.MobilityMemberCapture, address string) bool {
	addr, ok := normalizeIPv4Address(address)
	if !ok {
		return false
	}
	for _, raw := range capture.ExcludeAddresses {
		if prefix, ok := normalizeIPv4Prefix(raw); ok && prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func IPv4PrefixesExcluding(pool netip.Prefix, excludes []string) []netip.Prefix {
	pool = pool.Masked()
	if !pool.Addr().Is4() {
		return nil
	}
	start, end := ipv4PrefixRange(pool)
	ranges := []ipv4Range{{start: start, end: end}}
	for _, raw := range excludes {
		exclude, ok := normalizeIPv4Prefix(raw)
		if !ok || !pool.Overlaps(exclude) {
			continue
		}
		excludeStart, excludeEnd := ipv4PrefixRange(exclude)
		var next []ipv4Range
		for _, current := range ranges {
			if excludeEnd < current.start || excludeStart > current.end {
				next = append(next, current)
				continue
			}
			if excludeStart > current.start {
				next = append(next, ipv4Range{current.start, excludeStart - 1})
			}
			if excludeEnd < current.end {
				next = append(next, ipv4Range{excludeEnd + 1, current.end})
			}
		}
		ranges = next
	}
	var out []netip.Prefix
	for _, r := range ranges {
		out = append(out, ipv4RangePrefixes(r.start, r.end)...)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Addr().Compare(out[j].Addr()) < 0 || out[i].Addr() == out[j].Addr() && out[i].Bits() < out[j].Bits()
	})
	return out
}

func EvaluateCaptureGate(capture api.MobilityMemberCapture, observation CaptureGateObservation) CaptureGateStatus {
	typ, ref := strings.TrimSpace(capture.ActiveWhen.Type), strings.TrimSpace(capture.ActiveWhen.VirtualAddressRef)
	ref = strings.TrimPrefix(ref, "VirtualAddress/")
	if typ == "" && ref == "" {
		return CaptureGateStatus{Active: true, Reason: "AlwaysActive"}
	}
	if typ == "single-router" && ref == "" {
		return CaptureGateStatus{Active: true, Type: typ, Reason: "SingleRouter"}
	}
	result := CaptureGateStatus{Type: typ, VirtualAddressRef: ref, Reason: "CaptureGateInactive"}
	if typ != "vrrp-master" || ref == "" || !observation.VirtualAddressStatusAvailable {
		result.Message = "capture activeWhen requires an available vrrp-master VirtualAddress"
		return result
	}
	role := strings.TrimSpace(observation.VirtualAddressRole)
	result.VirtualAddressRole = role
	if strings.EqualFold(role, "master") {
		result.Active, result.Reason = true, "VRRPMaster"
		return result
	}
	result.Message = "VirtualAddress is not VRRP master"
	return result
}

type ipv4Range struct{ start, end uint32 }

func normalizeIPv4Host(value string) (string, error) {
	p, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil || !p.Addr().Is4() || p.Bits() != 32 {
		return "", fmt.Errorf("must be an IPv4 /32 CIDR")
	}
	return p.Masked().String(), nil
}
func normalizeIPv4Address(value string) (netip.Addr, bool) {
	if p, err := netip.ParsePrefix(strings.TrimSpace(value)); err == nil && p.Addr().Is4() {
		return p.Masked().Addr(), true
	}
	a, err := netip.ParseAddr(strings.TrimSpace(value))
	return a, err == nil && a.Is4()
}
func normalizeIPv4Prefix(value string) (netip.Prefix, bool) {
	if p, err := netip.ParsePrefix(strings.TrimSpace(value)); err == nil && p.Addr().Is4() {
		return p.Masked(), true
	}
	a, err := netip.ParseAddr(strings.TrimSpace(value))
	return netip.PrefixFrom(a, 32), err == nil && a.Is4()
}
func ipv4PrefixRange(prefix netip.Prefix) (uint32, uint32) {
	addr := ipv4ToUint32(prefix.Masked().Addr())
	size := uint64(1) << uint(32-prefix.Bits())
	return addr, addr + uint32(size-1)
}
func ipv4RangePrefixes(start, end uint32) []netip.Prefix {
	var out []netip.Prefix
	for uint64(start) <= uint64(end) {
		zero := bits.TrailingZeros32(start)
		if start == 0 {
			zero = 32
		}
		size := uint64(1) << uint(zero)
		for size > uint64(end)-uint64(start)+1 {
			zero--
			size >>= 1
		}
		out = append(out, netip.PrefixFrom(uint32ToIPv4(start), 32-zero))
		if size > uint64(^uint32(0))-uint64(start) {
			break
		}
		start += uint32(size)
	}
	return out
}
func ipv4ToUint32(addr netip.Addr) uint32 {
	raw := addr.As4()
	return uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])
}
func uint32ToIPv4(value uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}
