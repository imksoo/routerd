// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"fmt"
	"io"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

// A callback captured before this consumer started is historical. The current
// lease file may already contain a newer renewal or a different owner of the IP.
// Only an action consistent with that authoritative state may reach live DNS or
// mobility consumers. Everything else is still journaled with its original time.
func dhcpReplayDisposition(router *api.Router, fallback string, req controlapi.DHCPLeaseEventRequest, now time.Time) (string, error) {
	hasDHCP := false
	if router != nil {
		for _, r := range router.Spec.Resources {
			if r.Kind == "DHCPv4Server" || r.Kind == "DHCPv6Server" {
				hasDHCP = true
				break
			}
		}
	}
	if !hasDHCP {
		return "history-only-no-dhcp-source", nil
	}
	path, err := controllerchain.DNSMasqLeaseFile(router, fallback)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read authoritative DHCP leases: %w", err)
	}
	defer f.Close()
	const maxLeaseFile = 8 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxLeaseFile+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxLeaseFile {
		return "", fmt.Errorf("authoritative DHCP lease file exceeds %d bytes", maxLeaseFile)
	}
	ip, err := netip.ParseAddr(req.IP)
	if err != nil {
		return "", err
	}
	if ip.Is6() {
		return "history-only-unverified-dhcpv6-identity", nil
	}
	found, matching := false, true
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.EqualFold(fields[0], "duid") {
			continue
		}
		if len(fields) < 4 {
			return "", fmt.Errorf("invalid authoritative DHCP lease record")
		}
		expiry, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return "", fmt.Errorf("invalid DHCP lease expiry: %w", err)
		}
		address, err := netip.ParseAddr(fields[2])
		if err != nil {
			return "", fmt.Errorf("invalid DHCP lease address: %w", err)
		}
		if address != ip || (expiry != 0 && expiry <= now.Unix()) {
			continue
		}
		found = true
		hostname := fields[3]
		if hostname == "*" {
			hostname = ""
		}
		if !strings.EqualFold(fields[1], req.MAC) || hostname != req.Hostname {
			matching = false
		}
	}
	if req.Action == daemonapi.DHCPLeaseActionRemoved {
		if !found {
			return "", nil
		}
	} else if found && matching {
		return "", nil
	}
	return "history-only-lease-state-changed", nil
}
