// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/logstore"
)

func (h Handler) dhcpFingerprintList(filter logstore.DHCPFingerprintFilter) ([]logstore.DHCPFingerprint, error) {
	if strings.TrimSpace(h.opts.DHCPFingerprintLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenDHCPFingerprintLog(h.opts.DHCPFingerprintLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.List(context.Background(), filter)
}

func (h Handler) dhcpStickyLeaseList(filter logstore.DHCPStickyFilter) ([]logstore.DHCPStickyLease, error) {
	if strings.TrimSpace(h.opts.DHCPStickyLogPath) == "" {
		return nil, nil
	}
	if _, err := os.Stat(h.opts.DHCPStickyLogPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	store, err := logstore.OpenDHCPStickyLogReadOnly(h.opts.DHCPStickyLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.List(context.Background(), filter)
}

func annotateDHCPLeasesWithSticky(leases []DHCPLease, sticky []logstore.DHCPStickyLease, now time.Time) []DHCPLease {
	if len(sticky) == 0 {
		return leases
	}
	byIP := map[string]logstore.DHCPStickyLease{}
	byMAC := map[string]logstore.DHCPStickyLease{}
	for _, row := range sticky {
		if row.StickyUntil.IsZero() || !row.StickyUntil.After(now) {
			continue
		}
		if row.IP != "" {
			byIP[row.IP] = row
		}
		if row.MAC != "" {
			byMAC[strings.ToLower(row.MAC)] = row
		}
	}
	seen := map[string]bool{}
	for i := range leases {
		seen[leases[i].IP+"|"+strings.ToLower(leases[i].MAC)] = true
		row, ok := byIP[leases[i].IP]
		if !ok {
			row, ok = byMAC[strings.ToLower(leases[i].MAC)]
		}
		if !ok {
			continue
		}
		stickyUntil := row.StickyUntil
		leases[i].StickyUntil = &stickyUntil
		leases[i].StickyState = "held"
	}
	for _, row := range byIP {
		key := row.IP + "|" + strings.ToLower(row.MAC)
		if seen[key] {
			continue
		}
		stickyUntil := row.StickyUntil
		leases = append(leases, DHCPLease{
			MAC:         row.MAC,
			IP:          row.IP,
			Hostname:    row.Hostname,
			Family:      row.Family,
			Source:      "sticky-history",
			StickyUntil: &stickyUntil,
			StickyState: "held",
		})
	}
	return leases
}

func (h Handler) dhcpLeaseList() ([]DHCPLease, error) {
	seen := map[string]DHCPLease{}
	now := time.Now().UTC()
	pathPriority := map[string]int{}
	for priority, path := range h.opts.DHCPLeasePaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := pathPriority[path]; !ok {
			pathPriority[path] = priority
		}
		leases, err := readDnsmasqLeases(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, lease := range leases {
			if leaseExpired(lease, now) {
				continue
			}
			key := lease.IP
			if key == "" {
				key = lease.MAC
			}
			if key == "" {
				continue
			}
			if existing, ok := seen[key]; !ok || preferDHCPLease(lease, existing, pathPriority[path], pathPriorityValue(pathPriority, existing.Source)) {
				seen[key] = lease
			}
		}
	}
	out := make([]DHCPLease, 0, len(seen))
	for _, lease := range seen {
		out = append(out, lease)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].IP < out[j].IP
	})
	return out, nil
}

func leaseExpired(lease DHCPLease, now time.Time) bool {
	return !lease.ExpiresAt.IsZero() && !lease.ExpiresAt.After(now)
}

func preferDHCPLease(candidate, existing DHCPLease, candidatePriority, existingPriority int) bool {
	if !candidate.ExpiresAt.IsZero() && !existing.ExpiresAt.IsZero() && !candidate.ExpiresAt.Equal(existing.ExpiresAt) {
		return candidate.ExpiresAt.After(existing.ExpiresAt)
	}
	return candidatePriority < existingPriority
}

func pathPriorityValue(priorities map[string]int, path string) int {
	if priority, ok := priorities[path]; ok {
		return priority
	}
	return len(priorities)
}

func readDnsmasqLeases(path string) ([]DHCPLease, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []DHCPLease
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		expiresUnix, _ := strconv.ParseInt(fields[0], 10, 64)
		hostname := fields[3]
		if hostname == "*" {
			hostname = ""
		}
		lease := DHCPLease{
			MAC:      strings.ToLower(fields[1]),
			IP:       fields[2],
			Hostname: hostname,
			Family:   leaseAddressFamily(fields[2]),
			Source:   path,
		}
		if expiresUnix > 0 {
			lease.ExpiresAt = time.Unix(expiresUnix, 0).UTC()
		}
		if len(fields) >= 5 && fields[4] != "*" {
			lease.ClientID = fields[4]
		}
		lease.Vendor = macVendor(lease.MAC)
		out = append(out, lease)
	}
	return out, nil
}

func leaseAddressFamily(address string) string {
	parsed, err := netip.ParseAddr(address)
	if err != nil {
		return ""
	}
	if parsed.Is4() {
		return "ipv4"
	}
	if parsed.Is6() {
		return "ipv6"
	}
	return ""
}
