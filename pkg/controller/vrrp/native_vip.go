// SPDX-License-Identifier: BSD-3-Clause

package vrrp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

const nativeVIPStatusName = "vrrp-native"

type nativeVIP struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	IfName   string `json:"ifname"`
	Address  string `json:"address"`
	Backend  string `json:"backend"`
	VHID     int    `json:"vhid"`
	Announce bool   `json:"announce"`
}

type nativeVIPJournal struct {
	Applied []nativeVIP
	Pending []nativeVIP
}

type nativeVIPAnnouncementError struct{ cause error }

func (e *nativeVIPAnnouncementError) Error() string { return e.cause.Error() }
func (e *nativeVIPAnnouncementError) Unwrap() error { return e.cause }

func sameVIP(a, b nativeVIP) bool {
	a.Announce, b.Announce = false, false
	return a == b
}

func containsVIP(items []nativeVIP, vip nativeVIP) bool {
	for _, item := range items {
		if sameVIP(item, vip) {
			return true
		}
	}
	return false
}

func withoutVIP(items []nativeVIP, vip nativeVIP) []nativeVIP {
	out := make([]nativeVIP, 0, len(items))
	for _, item := range items {
		if !sameVIP(item, vip) {
			out = append(out, item)
		}
	}
	return out
}

func (c *Controller) readNativeVIPJournal() (nativeVIPJournal, error) {
	status, err := c.Store.ReadObjectStatus(api.RouterAPIVersion, "Router", nativeVIPStatusName)
	if err != nil {
		return nativeVIPJournal{}, err
	}
	if status == nil {
		return nativeVIPJournal{}, nil
	}
	applied, err := decodeNativeVIPs(status["applied"])
	if err != nil {
		return nativeVIPJournal{}, err
	}
	pending, err := decodeNativeVIPs(status["pending"])
	return nativeVIPJournal{Applied: applied, Pending: pending}, err
}

func (c *Controller) saveNativeVIPJournal(journal nativeVIPJournal) error {
	if journal.Applied == nil {
		journal.Applied = []nativeVIP{}
	}
	if journal.Pending == nil {
		journal.Pending = []nativeVIP{}
	}
	return c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", nativeVIPStatusName, map[string]any{"applied": journal.Applied, "pending": journal.Pending})
}

func decodeNativeVIPs(value any) ([]nativeVIP, error) {
	var out []nativeVIP
	switch value := value.(type) {
	case []nativeVIP:
		out = value
	case []any:
		for _, item := range value {
			fields, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid native VIP receipt")
			}
			vip := nativeVIP{}
			for key, target := range map[string]*string{"kind": &vip.Kind, "name": &vip.Name, "ifname": &vip.IfName, "address": &vip.Address, "backend": &vip.Backend} {
				text, ok := fields[key].(string)
				if !ok {
					return nil, fmt.Errorf("invalid native VIP receipt %s", key)
				}
				*target = text
			}
			vhid, ok := fields["vhid"].(float64)
			if !ok || vhid != math.Trunc(vhid) || vhid < 0 || vhid > 255 {
				return nil, fmt.Errorf("invalid native VIP receipt vhid")
			}
			vip.VHID = int(vhid)
			vip.Announce, ok = fields["announce"].(bool)
			if !ok {
				return nil, fmt.Errorf("invalid native VIP receipt announcement")
			}
			out = append(out, vip)
		}
	default:
		return nil, fmt.Errorf("invalid native VIP receipt list")
	}
	for _, vip := range out {
		_, err := netip.ParsePrefix(vip.Address)
		if err != nil || vip.Kind != "VirtualAddress" || vip.Name == "" || vip.IfName == "" ||
			(vip.Backend != "iproute2" && vip.Backend != "ifconfig" && vip.Backend != "carp") || (vip.Backend == "carp" && vip.VHID == 0) {
			return nil, fmt.Errorf("invalid native VIP receipt identity")
		}
	}
	return out, nil
}

func (c *Controller) desiredNativeVIPs(aliases map[string]string) ([]nativeVIP, error) {
	var out []nativeVIP
	for _, resource := range c.Router.Spec.Resources {
		spec, ok, err := vrrpResourceSpec(resource)
		if err != nil {
			return nil, err
		}
		if !ok || spec.Mode == "vrrp" && c.currentOS() != platform.OSFreeBSD {
			continue
		}
		address, err := renderVirtualAddress(c.Router, spec)
		if err != nil {
			continue // unresolved intent cannot retain a previously owned address
		}
		vip := nativeVIP{Kind: resource.Kind, Name: resource.Metadata.Name, IfName: aliases[spec.Interface], Address: address, Backend: c.virtualAddressBackend(spec)}
		if vip.Backend == "carp" {
			vip.VHID = spec.VRRP.VirtualRouterID
		}
		out = append(out, vip)
	}
	return out, nil
}

func (c *Controller) cleanupNativeVIPs(ctx context.Context, aliases map[string]string) (bool, error) {
	if c.DryRun {
		return false, nil
	}
	journal, err := c.readNativeVIPJournal()
	if err != nil {
		return false, err
	}
	desired, err := c.desiredNativeVIPs(aliases)
	if err != nil {
		return false, err
	}
	changed := false
	for _, vip := range append(append([]nativeVIP(nil), journal.Applied...), journal.Pending...) {
		if containsVIP(desired, vip) || !containsVIP(journal.Applied, vip) && !containsVIP(journal.Pending, vip) {
			continue
		}
		previous, err := c.Store.ReadObjectStatus(api.NetAPIVersion, vip.Kind, vip.Name)
		if err != nil {
			return changed, err
		}
		present, exact, err := c.observeNativeVIP(ctx, vip, journal)
		if err != nil {
			return changed, err
		}
		if present && !exact {
			return changed, fmt.Errorf("owned VIP %s on %s changed outside routerd", vip.Address, vip.IfName)
		}
		if present {
			if !containsVIP(journal.Pending, vip) {
				journal.Pending = append(journal.Pending, vip)
				if err := c.saveNativeVIPJournal(journal); err != nil {
					return changed, err
				}
			}
			if err := c.removeStaticAddress(ctx, vip.IfName, vip.Address); err != nil {
				return changed, err
			}
			present, _, err = c.observeNativeVIP(ctx, vip, journal)
			if err != nil {
				return changed, fmt.Errorf("verify withdrawn VIP %s on %s: %w", vip.Address, vip.IfName, err)
			}
			if present {
				return changed, fmt.Errorf("VIP %s remains on %s after withdrawal", vip.Address, vip.IfName)
			}
			changed = true
		}
		journal.Applied, journal.Pending = withoutVIP(journal.Applied, vip), withoutVIP(journal.Pending, vip)
		if err := c.saveNativeVIPJournal(journal); err != nil {
			return changed, err
		}
		status := map[string]any{"phase": "Removed", "backend": vip.Backend, "appliedAddress": "", "address": vip.Address, "ifname": vip.IfName}
		if previous["phase"] == "Pending" && previous["reason"] == "WhenFalse" {
			status["phase"], status["reason"], status["staticAddressRemoved"] = "Pending", "WhenFalse", true
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, vip.Kind, vip.Name, status); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func (c *Controller) applyNativeVIP(ctx context.Context, vip nativeVIP, announce, force bool, apply func() error) (bool, error) {
	journal, err := c.readNativeVIPJournal()
	if err != nil {
		return false, err
	}
	present, exact, err := c.observeNativeVIP(ctx, vip, journal)
	if err != nil {
		return false, err
	}
	owned := containsVIP(journal.Applied, vip) || containsVIP(journal.Pending, vip)
	if present && (!owned || !exact) {
		return false, fmt.Errorf("VIP %s on %s already exists without matching routerd ownership", vip.Address, vip.IfName)
	}
	needsAnnounce := false
	for _, pending := range journal.Pending {
		needsAnnounce = needsAnnounce || sameVIP(pending, vip) && pending.Announce
	}
	_, ipv4 := staticIPv4Host(vip.Address)
	needsAnnounce = needsAnnounce || !present && announce && ipv4 && c.currentOS() == platform.OSLinux
	if present && !force && !needsAnnounce && containsVIP(journal.Applied, vip) && !containsVIP(journal.Pending, vip) {
		return false, nil
	}
	vip.Announce = needsAnnounce
	journal.Pending = append(withoutVIP(journal.Pending, vip), vip)
	if err := c.saveNativeVIPJournal(journal); err != nil {
		return false, err
	}
	if !present || force {
		if err := apply(); err != nil {
			return false, err
		}
		present, exact, err = c.observeNativeVIP(ctx, vip, journal)
		if err != nil {
			return true, fmt.Errorf("verify applied VIP %s on %s: %w", vip.Address, vip.IfName, err)
		}
		if !present || !exact {
			return true, fmt.Errorf("VIP %s on %s does not match after apply", vip.Address, vip.IfName)
		}
	}
	if needsAnnounce {
		if err := c.announceStaticIPv4Address(ctx, vip.IfName, vip.Address); err != nil {
			return true, &nativeVIPAnnouncementError{cause: err}
		}
	}
	vip.Announce = false
	journal.Applied = append(withoutVIP(journal.Applied, vip), vip)
	journal.Pending = withoutVIP(journal.Pending, vip)
	return true, c.saveNativeVIPJournal(journal)
}

func (c *Controller) observeNativeVIP(ctx context.Context, vip nativeVIP, journal nativeVIPJournal) (present, exact bool, err error) {
	wanted, err := netip.ParsePrefix(vip.Address)
	if err != nil {
		return false, false, err
	}
	if c.currentOS() == platform.OSLinux {
		if vip.Backend != "iproute2" {
			return false, false, fmt.Errorf("cannot reconcile %s VIP receipt on Linux", vip.Backend)
		}
		out, err := c.run(ctx, stringutil.FirstNonEmpty(c.IP, "ip"), "-j", "addr", "show", "dev", vip.IfName)
		if err != nil {
			return false, false, err
		}
		var links []struct {
			IfName    string `json:"ifname"`
			Addresses []struct {
				Local string `json:"local"`
				Bits  int    `json:"prefixlen"`
			} `json:"addr_info"`
		}
		if err := json.Unmarshal(out, &links); err != nil {
			return false, false, fmt.Errorf("decode native VIP addresses: %w", err)
		}
		if len(links) != 1 || links[0].IfName != vip.IfName || links[0].Addresses == nil {
			return false, false, fmt.Errorf("incomplete native VIP interface observation for %s", vip.IfName)
		}
		for _, address := range links[0].Addresses {
			local, err := netip.ParseAddr(address.Local)
			if err != nil {
				return false, false, err
			}
			if local == wanted.Addr() {
				return true, address.Bits == wanted.Bits(), nil
			}
		}
		return false, false, nil
	}
	if c.currentOS() != platform.OSFreeBSD || vip.Backend != "ifconfig" && vip.Backend != "carp" {
		return false, false, fmt.Errorf("native VIP backend %s unsupported on %s", vip.Backend, c.currentOS())
	}
	out, err := c.run(ctx, stringutil.FirstNonEmpty(c.Ifconfig, "ifconfig"), vip.IfName)
	if err != nil {
		return false, false, err
	}
	if !strings.HasPrefix(strings.TrimSpace(string(out)), vip.IfName+":") {
		return false, false, fmt.Errorf("incomplete native VIP interface observation for %s", vip.IfName)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "inet" && fields[0] != "inet6" {
			continue
		}
		local, err := netip.ParseAddr(fields[1])
		if err != nil {
			return false, false, err
		}
		bits, vhid := -1, 0
		for i := 2; i+1 < len(fields); i++ {
			switch fields[i] {
			case "prefixlen":
				bits, err = strconv.Atoi(fields[i+1])
			case "netmask":
				var mask uint64
				mask, err = strconv.ParseUint(strings.TrimPrefix(fields[i+1], "0x"), 16, 32)
				if err == nil {
					bits = 0
					for bit := 31; bit >= 0 && mask&(1<<bit) != 0; bit-- {
						bits++
					}
					if uint32(mask) != ^uint32(0)<<(32-bits) {
						err = fmt.Errorf("noncontiguous native IPv4 netmask")
					}
				}
			case "vhid":
				vhid, err = strconv.Atoi(fields[i+1])
			}
			if err != nil {
				return false, false, err
			}
		}
		local = local.WithZone("")
		if vip.Backend == "carp" && vhid == vip.VHID && local != wanted.Addr() && !ownedCARPGroupAlias(journal, vip, local, bits) {
			return false, false, fmt.Errorf("CARP VHID %d on %s has an unowned alias %s", vhid, vip.IfName, local)
		}
		if local == wanted.Addr() {
			present, exact = true, bits == wanted.Bits() && vhid == vip.VHID
		}
	}
	return present, exact, nil
}

func ownedCARPGroupAlias(journal nativeVIPJournal, vip nativeVIP, address netip.Addr, bits int) bool {
	for _, group := range [][]nativeVIP{journal.Applied, journal.Pending} {
		for _, owned := range group {
			if owned.Backend != "carp" || owned.IfName != vip.IfName || owned.VHID != vip.VHID {
				continue
			}
			prefix, err := netip.ParsePrefix(owned.Address)
			if err == nil && prefix.Addr() == address && prefix.Bits() == bits {
				return true
			}
		}
	}
	return false
}
