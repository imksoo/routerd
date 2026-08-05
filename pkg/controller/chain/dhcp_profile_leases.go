// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

type dhcpv4ProfileLeaseScope struct {
	server string
	match  api.DHCPv4ServerScopeMatchSpec
	start  netip.Addr
	end    netip.Addr
}

// pruneDHCPv4ProfileMismatchedLeases removes only dynamic leases whose MAC now
// matches a profile scope but whose address is outside every matching profile
// pool. Static reservations are deliberately retained. The caller restarts
// dnsmasq when this returns changed, so its in-memory lease table agrees with
// the persisted file before another DHCP request is served.
func pruneDHCPv4ProfileMismatchedLeases(router *api.Router, configPath, pidFile string) (bool, error) {
	leaseFile, err := dnsmasqLeaseFile(router, configPath, pidFile)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(leaseFile)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	scopes, reserved := dhcpv4ProfileLeaseScopes(router)
	if len(scopes) == 0 {
		return false, nil
	}
	var kept bytes.Buffer
	changed := false
	for _, record := range strings.SplitAfter(string(data), "\n") {
		if record == "" {
			continue
		}
		fields := strings.Fields(record)
		if len(fields) < 3 {
			kept.WriteString(record)
			continue
		}
		mac, macErr := net.ParseMAC(fields[1])
		ip, ipErr := netip.ParseAddr(fields[2])
		if macErr != nil || ipErr != nil || !ip.Is4() || reserved[strings.ToLower(mac.String())] || !dhcpv4LeaseOutsideMatchingProfilePool(scopes, mac, ip) {
			kept.WriteString(record)
			continue
		}
		changed = true
	}
	if !changed {
		return false, nil
	}
	info, err := os.Stat(leaseFile)
	if err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(leaseFile), ".routerd-dnsmasq-leases-*")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(kept.Bytes()); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmpName, leaseFile); err != nil {
		return false, err
	}
	return true, nil
}

func dhcpv4ProfileLeaseScopes(router *api.Router) ([]dhcpv4ProfileLeaseScope, map[string]bool) {
	var scopes []dhcpv4ProfileLeaseScope
	reserved := map[string]bool{}
	if router == nil {
		return scopes, reserved
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DHCPv4Reservation" {
			continue
		}
		spec, err := resource.DHCPv4ReservationSpec()
		if err == nil {
			if mac, err := net.ParseMAC(spec.MACAddress); err == nil {
				reserved[strings.ToLower(mac.String())] = true
			}
		}
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DHCPv4Server" {
			continue
		}
		spec, err := resource.DHCPv4ServerSpec()
		if err != nil {
			continue
		}
		profiles := make(map[string]api.DHCPv4ServerProfileSpec, len(spec.Profiles))
		for _, profile := range spec.Profiles {
			profiles[profile.Name] = profile
		}
		for _, scope := range spec.Scopes {
			pool := scope.AddressPool
			if scope.ProfileRef != "" {
				profile, ok := profiles[scope.ProfileRef]
				if !ok {
					continue
				}
				pool = profile.AddressPool
			}
			start, startErr := netip.ParseAddr(pool.Start)
			end, endErr := netip.ParseAddr(pool.End)
			if startErr != nil || endErr != nil || !start.Is4() || !end.Is4() {
				continue
			}
			scopes = append(scopes, dhcpv4ProfileLeaseScope{server: resource.Metadata.Name, match: scope.Match, start: start, end: end})
		}
	}
	return scopes, reserved
}

func dhcpv4LeaseOutsideMatchingProfilePool(scopes []dhcpv4ProfileLeaseScope, mac net.HardwareAddr, ip netip.Addr) bool {
	matched := false
	for _, scope := range scopes {
		if !dhcpv4ScopeMatchesMAC(scope.match, mac) {
			continue
		}
		matched = true
		if ip.Compare(scope.start) >= 0 && ip.Compare(scope.end) <= 0 {
			return false
		}
	}
	return matched
}

func dhcpv4ScopeMatchesMAC(match api.DHCPv4ServerScopeMatchSpec, mac net.HardwareAddr) bool {
	for _, value := range match.MACAddresses {
		candidate, err := net.ParseMAC(value)
		if err == nil && bytes.Equal(candidate, mac) {
			return true
		}
	}
	for _, oui := range match.OUIPrefixes {
		if len(mac) < 3 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(oui), mac.String()[:8]) {
			return true
		}
	}
	return false
}
